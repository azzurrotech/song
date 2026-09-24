// Command middleware demonstrates song's middleware mode: song is embedded
// in an unrelated Go server and serves the routes it owns while every other
// path continues to be handled by the host application.
//
//	# create a silo and a file first
//	song --port 8080 --root ./data &        # or use the admin UI at :8080/admin
//	curl -X POST localhost:8080/api/song/silos -d 'name=web'
//	curl -X POST localhost:8080/api/song/silos/web/file \
//	     -d 'path=index.html&content=<h1>from song</h1>'
//
//	# then run this host server (song lives at :8090)
//	go run ./examples/middleware --root ./data
//	curl localhost:8090/            -> handled by the host app
//	curl localhost:8090/web/        -> handled by song (silo serving)
//	curl localhost:8090/api/song/silos -> handled by song (full API)
//	curl localhost:8090/health      -> handled by song
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"

	"azzurrotech/song/pkg/song"
)

func main() {
	port := flag.String("port", "8090", "host port")
	root := flag.String("root", "./data", "song store root (same as the song server uses)")
	secret := flag.String("secret", "change-me-secret-key-32-chars-long-!!", "shared encryption secret")
	flag.Parse()

	srv, err := song.New(song.Config{
		Root:   *root,
		Secret: *secret,
		// Provide extra template functions available to every SSR template.
		Funcs: templateFuncs(),
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
