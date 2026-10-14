// Command middleware demonstrates song's middleware mode: song is embedded
// in an unrelated Go server and serves the routes it owns while every other
// path continues to be handled by the host application.
//
// Create a silo as a directory with an index document first, e.g.:
//
//	mkdir -p data/web && printf '<h1>from song</h1>' > data/web/index.html
//
//	# then run this host server (song lives at :8090)
//	go run ./examples/middleware --root ./data
//	curl localhost:8090/            -> handled by the host app
//	curl localhost:8090/web/        -> handled by song (silo serving)
//	curl localhost:8090/_song/components.js -> handled by song
//
// song also owns the Web Components surfaces: /_song/components.js (the
// baseline custom-element runtime) and /_song/components (tag definitions).
// Any served HTML file that uses a registered tag such as <host-hello> gets the
// definitions and runtime injected automatically.
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"azzurrotech/song/pkg/component"
	"azzurrotech/song/pkg/song"
)

func main() {
	port := flag.String("port", "8090", "host port")
	root := flag.String("root", "./data", "song store root (same as the song server uses)")
	flag.Parse()

	srv, err := song.New(song.Config{
		Root: *root,
		// Provide extra template functions available to every SSR template.
		Funcs: templateFuncs(),
		// Register a host-defined Web Component. It becomes usable both as
		// <host-hello> in any served HTML file and as
		// {{template "host-hello" .}} in server-rendered templates.
		Components: []component.Tag{
			{Name: "host-hello", Markup: `<b>hello</b> <slot></slot>`},
		},
	})
	if err != nil {
		log.Fatalf("song: %v", err)
	}

	// The host application handles everything song does not own.
	host := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprintf(w, "host app handled %s — song is only the middleware here.\n", r.URL.Path)
	})

	addr := ":" + *port
	fmt.Printf("host server on %s — song middleware embedded\n", addr)
	log.Fatal(http.ListenAndServe(addr, srv.Middleware(host)))
}
