package song

import (
	"embed"
	"io/fs"
	"net/http"
)

// uiFS embeds the management interface, which is itself a static web
// application (HTML + CSS + vanilla JavaScript) hosted by song.
//
//go:embed ui
var uiFS embed.FS

// handleUI serves the /admin management interface.
func (s *Song) handleUI(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/admin" {
		http.Redirect(w, r, "/admin/", http.StatusMovedPermanently)
		return
	}
	sub, err := fs.Sub(uiFS, "ui")
	if err != nil {
		http.Error(w, "UI not available", http.StatusInternalServerError)
		return
	}
	fs := http.FileServer(http.FS(sub))
	http.StripPrefix("/admin/", fs).ServeHTTP(w, r)
}
