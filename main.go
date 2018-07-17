package main

import (
	"flag"
	"log"
	"os"

	"./fileserver"
)

func main() {
	l := log.New(os.Stderr, "", 0)
	flag.Parse()
	args := flag.Args()
	if len(args) < 1 {
		l.Panicln("Need root directory")
	}
	rootDir := args[0]
	stat, err := os.Stat(rootDir)
	if err != nil {
		l.Panicln(err)
	}
	if !stat.IsDir() {
		l.Panicln("Is not a directory")
	}

	server := fileserver.New(rootDir)
	err = server.ListenToPort("60000")
	if err != nil {
		l.Panicln(err)
	}
}
