// Package main is the song server-mode entry point. song runs as either a
// standalone Go webserver (here) or as middleware embedded in another Go
// server (see pkg/song and examples/middleware).
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"

	"azzurrotech/song/pkg/song"
)

// versionString is the value printed by --version.
const versionString = "2.0.0"

// envOr returns the environment variable value, or def when empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func main() {
	port := flag.String("port", envOr("SONG_PORT", "8080"), "HTTP port to listen on")
	root := flag.String("root", envOr("SONG_ROOT", "./data"), "root directory that holds the silos")
	version := flag.Bool("version", false, "print version and exit")
	help := flag.Bool("help", false, "show usage and exit")
	flag.Parse()

	if *help {
		fmt.Println(`song — static hosting with server-side template processing and Web Components

Usage: song [options]

Options:
  --port   <n>    HTTP port to listen on             (default 8080)
  --root   <dir>  root directory that holds silos    (default ./data)
  --version       print the version and exit
  --help          show this help

Routes (a silo is a directory directly under the store root):
  GET  /{silo}/<path>   serve a static resource; directories serve index.html
  POST /{silo}/<path>   upsert a static resource (body is the content)
  /_song/components.js  baseline Web Components runtime
  /_song/components     registered Web Component tag definitions

Environment:
  SONG_PORT   default for --port
  SONG_ROOT   default for --root`)
		return
	}
	if *version {
		fmt.Printf("song v%s\n", versionString)
		return
	}

	srv, err := song.New(song.Config{Root: *root})
	if err != nil {
		log.Fatalf("song: %v", err)
	}

	addr := ":" + *port
	fmt.Printf("song v%s listening on %s\n", versionString, addr)
	fmt.Printf("  store root : %s\n", srv.Store().Root())
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}
