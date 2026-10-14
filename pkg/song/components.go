package song

import (
	"net/http"

	"azzurrotech/song/pkg/component"
)

// handleComponents serves song's Web Components surfaces:
//
//	GET /_song/components.js  the baseline custom-element runtime (ES module)
//	GET /_song/components     the registered tag definitions as JSON
//
// The runtime is injected into (and only into) served HTML documents that use
// a registered tag, so the system-defined tags are accessible from any
// provided HTML file. See pkg/component.
func (s *Song) handleComponents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	switch r.URL.Path {
	case component.RuntimePath:
		w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
		w.Header().Set("Cache-Control", "public, max-age=3600")
		_, _ = w.Write([]byte(component.Runtime()))
	case component.DefinitionsPath:
		tags := s.components.Tags()
		writeJSON(w, http.StatusOK, map[string]any{
			"runtime": component.RuntimePath,
			"count":   len(tags),
			"tags":    tags,
		})
	default:
		http.NotFound(w, r)
	}
}
