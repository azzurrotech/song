package song

import (
	"net/http"
	"strings"
	"time"
)

// StatusPath is the prefix of song's introspection endpoints. A platform
// orchestrator (atp) uses them to answer "is song reachable, and what is it
// hosting?" over the network instead of through a Go import.
const StatusPath = "/_song/"

// HealthPath and SilosPath are the two introspection endpoints song owns
// beneath StatusPath. The rest of /_song/ belongs to the component runtime.
const (
	HealthPath     = StatusPath + "health"
	SilosPath      = StatusPath + "silos"
	ComponentsPath = StatusPath + "components"
)

// Status is the JSON shape of GET /_song/health.
type Status struct {
	Status  string    `json:"status"`
	Service string    `json:"service"`
	Version string    `json:"version"`
	Root    string    `json:"root"`
	Silos   int       `json:"silos"`
	Time    time.Time `json:"time"`
}

// Health answers GET /_song/health: a liveness and identity document.
func (s *Song) Health() Status {
	silos, _ := s.store.ListSilos()
	return Status{
		Status:  "healthy",
		Service: "song",
		Version: Version,
		Root:    s.store.Root(),
		Silos:   len(silos),
		Time:    time.Now().UTC(),
	}
}

// Silos answers GET /_song/silos: the store root and every hosted silo.
func (s *Song) Silos() (root string, silos []string, err error) {
	root = s.store.Root()
	silos, err = s.store.ListSilos()
	if silos == nil {
		silos = []string{}
	}
	return root, silos, err
}

// handleStatus serves song's introspection endpoints. It is mounted before the
// silo router so a silo can never shadow them.
func (s *Song) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	switch strings.TrimSuffix(r.URL.Path, "/") {
	case "/_song/health":
		writeJSON(w, http.StatusOK, s.Health())
	case "/_song/silos":
		root, silos, err := s.Silos()
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"root": root, "silos": silos})
	case "/_song/components":
		writeJSON(w, http.StatusOK, map[string]any{"components": s.components.Names()})
	default:
		http.NotFound(w, r)
	}
}
