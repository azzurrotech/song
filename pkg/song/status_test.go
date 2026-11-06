package song

import (
	"net/http"
	"testing"
)

// TestStatusEndpoints proves the platform-introspection endpoints exist so a
// remote orchestrator (atp) never needs a Go import of song.
func TestStatusEndpoints(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()

	rec := do(t, h, http.MethodGet, HealthPath, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health: %d %q", rec.Code, rec.Body.String())
	}
	var st Status
	decodeResp(t, rec, &st)
	if st.Status != "healthy" || st.Service != "song" || st.Version != Version {
		t.Fatalf("unexpected health: %+v", st)
	}

	rec = do(t, h, http.MethodGet, SilosPath, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("silos: %d %q", rec.Code, rec.Body.String())
	}

	// The component registry is introspectable too, so a remote orchestrator can
	// report what song is actually hosting.
	rec = do(t, h, http.MethodGet, ComponentsPath, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("components: %d %q", rec.Code, rec.Body.String())
	}
	var comps struct {
		Components []string `json:"components"`
	}
	decodeResp(t, rec, &comps)
	if len(comps.Components) == 0 {
		t.Fatal("components endpoint reports no components")
	}

	// The component runtime keeps its own paths under the same prefix.
	if rec := do(t, h, http.MethodGet, "/_song/components.js", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("component runtime must still be served: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, HealthPath, nil, nil); rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("health must reject POST: %d", rec.Code)
	}
}
