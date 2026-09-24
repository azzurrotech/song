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
	"strconv"

	"azzurrotech/song/pkg/song"
)

const (
	defaultSecret = "change-me-secret-key-32-chars-long-!!"
	versionString = "2.0.0"
)

// envOr returns the environment variable value, or def when empty.
func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

// envBool parses the environment variable as a boolean, or returns def.
func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}

func main() {
	port := flag.String("port", envOr("SONG_PORT", "8080"), "HTTP port to listen on")
	root := flag.String("root", envOr("SONG_ROOT", "./data"), "root directory that holds the silos")
	secret := flag.String("secret", envOr("SONG_SECRET", defaultSecret), "encryption secret (must be at least 32 characters)")
	seed := flag.Bool("seed", envBool("SONG_SEED", false), "create a demo silo that exercises every feature")
	version := flag.Bool("version", false, "print version and exit")
	help := flag.Bool("help", false, "show usage and exit")
	flag.Parse()

	if *help {
		fmt.Println(`song — static hosting, server-side rendering and encrypted files

Usage: song [options]

Options:
  --port   <n>    HTTP port to listen on             (default 8080)
  --root   <dir>  root directory that holds silos    (default ./data)
  --secret <key>  encryption secret, >= 32 chars     (set a real one in prod)
  --seed          create a demo silo on startup
  --version       print the version and exit
  --help          show this help

Routes:
  /{silo}/...           serve a silo's web app (index.html by client header)
  /api/song/silos...    full file CRUD API (JSON, forms, multipart, raw)
  /api/song/silos/{s}/render   server-side rendering (go html/template)
  /api/auth/*           magic-link auth
  /admin                management UI
  /health               health check`)
		return
	}
	if *version {
		fmt.Printf("song v%s\n", versionString)
		return
	}

	if len(*secret) < 32 {
		log.Fatal("secret must be at least 32 characters long")
	}

	srv, err := song.New(song.Config{Root: *root, Secret: *secret})
	if err != nil {
		log.Fatalf("song: %v", err)
	}

	if *seed {
		if err := song.SeedDemo(srv); err != nil {
			log.Fatalf("seed: %v", err)
		}
	}

	addr := ":" + *port
	fmt.Printf("song v%s listening on %s\n", versionString, addr)
	fmt.Printf("  store root : %s\n", srv.Store().Root())
	fmt.Printf("  management : http://localhost%s/admin\n", addr)
	fmt.Printf("  health     : http://localhost%s/health\n", addr)
	if *seed {
		fmt.Printf("  demo silo  : http://localhost%s/demo/ (try header X-Index-Variant: mobile)\n", addr)
	}
	log.Fatal(http.ListenAndServe(addr, srv.Handler()))
}
