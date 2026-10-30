// Package viewkit serves the small, dependency-free JavaScript library that
// song-hosted applications share for the parts of a view that are decisions
// rather than markup: routing, escaping, relative time, dashboard aggregation,
// map projection and clustering, severity triage and reference counts.
//
// The library is embedded into the binary and served at AssetPath, so an
// application can load it from any song mount (including the one atp owns)
// without bundling or duplicating it:
//
//	<script src="/_song/viewkit.js" defer></script>
//
// It exports window.SongView in the browser and the same object from
// require() in Node, which keeps the library testable without a DOM.
package viewkit

import (
	_ "embed"
	"net/http"
)

// AssetPath is the URL at which song serves the viewkit library.
const AssetPath = "/_song/viewkit.js"

// ContentType is the media type used when serving AssetPath.
const ContentType = "text/javascript; charset=utf-8"

//go:embed viewkit.js
var js string

// JS returns the library source.
func JS() string { return js }

// Serve writes the library with the correct content type. It never varies by
// request, so the response can be cached by the browser.
func Serve(w http.ResponseWriter) {
	w.Header().Set("Content-Type", ContentType)
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write([]byte(js))
}

// Handler returns an http.HandlerFunc for AssetPath.
func Handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			w.Header().Set("Allow", "GET, HEAD")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		Serve(w)
	}
}
