package fileserver

import (
	"encoding/json"
	"fmt"
  "errors"
  "path"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
  "strconv"
)

type FileServer struct {
	fileRoot string
	Server   *http.Server
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type FileResponse struct {
	Directory bool           `json:"directory"`
	Filename  string         `json:"filename"`
	Size      int64          `json:"size"`
	ModTime   time.Time      `json:"mtime"`
	Files     []FileResponse `json:"files,omitempty"`
}

type InspectResponse struct {
	File string `json:"file"`
}

type ContentsResponse struct {
	Files []FileResponse `json:"files"`
	Lines []string       `json:"lines"`
}

func New(root string) *FileServer {
	server := &http.Server{}
	return &FileServer{
		fileRoot: root,
		Server:   server,
	}
}

func (f FileServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	err := f.handleRequest(w, r)
	if err != nil {
		json.NewEncoder(w).Encode(ErrorResponse{
			Error: err.Error(),
		})
	}
}

func (f FileServer) handleRequest(w http.ResponseWriter, r *http.Request) error {
	fullPath := f.fileRoot + "/" + r.URL.Path
	action := r.URL.Query().Get("action")
	stat, err := os.Stat(fullPath)
	if err != nil {
		return err
	}
	if stat.IsDir() {
		if action != "list" && action != "stat" {
			return fmt.Errorf("Cannot open directory")
		}
		response := FileResponse{
			Directory: stat.IsDir(),
			Filename:  stat.Name(),
			Size:      stat.Size(),
			ModTime:   stat.ModTime(),
		}
		var files []FileResponse
		filesRead, err := ioutil.ReadDir(fullPath)
		if err != nil {
			return err
		}
		for _, file := range filesRead {
			fileData := FileResponse{
				Directory: file.IsDir(),
				Filename:  file.Name(),
				Size:      file.Size(),
				ModTime:   stat.ModTime(),
			}
			files = append(files, fileData)
		}
		response.Files = files
		return json.NewEncoder(w).Encode(response)
	} else if action == "stat" {
		response := FileResponse{
			Directory: stat.IsDir(),
			Filename:  stat.Name(),
			Size:      stat.Size(),
		}
		return json.NewEncoder(w).Encode(response)
	} else if action == "inspect" {
		cmd := exec.Command("file", fullPath)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		err = cmd.Start()
		if err != nil {
			return err
		}
		output, err := ioutil.ReadAll(stdout)
		if err != nil {
			return err
		}
		cmd.Wait()
		response := InspectResponse{
			File: string(output[:]),
		}
		return json.NewEncoder(w).Encode(response)
	} else if action == "contents" {
		cmd := exec.Command("7z", "l", fullPath)
		stdout, err := cmd.StdoutPipe()
		if err != nil {
			return err
		}
		err = cmd.Start()
		if err != nil {
			return err
		}
		output, err := ioutil.ReadAll(stdout)
		if err != nil {
			return err
		}
		cmd.Wait()
		outputStr := string(output[:])
		response := ContentsResponse{
			Files: parse7zipOutput(outputStr),
		}
		return json.NewEncoder(w).Encode(response)
  } else if action == "extract" {
    toExtract := r.URL.Query().Get("extract")
    if toExtract == "" {
      return errors.New("Extract required")
    }
    dir, err := ioutil.TempDir("", "go-fileserver-extract")
    if err != nil {
      return err
    }
		cmd := exec.Command("7z", "x", fullPath, "-o" + dir, toExtract)
    err = cmd.Start()
    if err != nil {
      return err
    }
		cmd.Wait()
		file, err := os.Open(path.Join(dir, toExtract))
		if err != nil {
			return err
		}
    asDownload := r.URL.Query().Get("download")
    disposition := "inline"
    if asDownload == "true" {
      disposition = "attachment"
    }
    w.Header().Set("Content-Disposition", disposition + "; filename=\""+ path.Base(toExtract) +"\"")
		io.Copy(w, file)
    return nil
	} else {
    asDownload := r.URL.Query().Get("download")
    disposition := "inline"
    if asDownload == "true" {
      disposition = "attachment"
    }
    w.Header().Set("Content-Disposition", disposition + "; filename=\""+ path.Base(fullPath) +"\"")
		file, err := os.Open(fullPath)
		if err != nil {
			return err
		}
		io.Copy(w, file)
		return nil
	}
}

func (f *FileServer) Listen(port, host string) error {
	f.Server.Addr = host + ":" + port
	f.Server.Handler = f
	return f.Server.ListenAndServe()
}

func (f *FileServer) ListenToPort(port string) error {
	return f.Listen(port, "")
}

const (
	parseStart  = iota
	parseFields = iota
	parseFiles  = iota
	parseEnd    = iota
)

func parse7zipOutput(output string) []FileResponse {
	files := []FileResponse{}

	matchDate, _ := regexp.Compile("Date\\s+Time\\s+Attr\\s+Size\\s+Compressed\\s+Name")
	matchLines, _ := regexp.Compile("^(-+)\\s+(-+)\\s+(-+)\\s+(-+)\\s+(-+)$")
  var matchRow *regexp.Regexp

	lines := strings.Split(output, "\n")
	parseState := parseStart

  var fieldLengths [6]int

	for _, line := range lines {
		switch parseState {
		case parseStart:
			if matchDate.MatchString(line) {
				parseState = parseFields
			}
		case parseFields:
      if matchLines.MatchString(line) {
        matches := matchLines.FindStringSubmatch(line)
        for i, column := range matches {
          fieldLengths[i] = len(column)
        }
        regex := fmt.Sprintf("^(.{%d})\\s+(.{%d})\\s+(.{%d})\\s+(.{%d})\\s+(.+)$", fieldLengths[1], fieldLengths[2], fieldLengths[3], fieldLengths[4])
        matchRow, _ = regexp.Compile(regex)
        parseState = parseFiles
      }
		case parseFiles:
			fileInfo := FileResponse{
			}
			if matchLines.MatchString(line) {
				parseState = parseEnd
			} else {
        row := matchRow.FindStringSubmatch(line)
        dateForm := "2006-01-02 15:04:05"
        fileInfo.ModTime, _ = time.Parse(dateForm, strings.TrimSpace(row[1]))
        fileInfo.Size, _ = strconv.ParseInt(strings.TrimSpace(row[3]), 10, 64)
        fileInfo.Filename = row[5]
			}

			files = append(files, fileInfo)
		case parseEnd:
			// do nothing
		}
	}

	return files
}
