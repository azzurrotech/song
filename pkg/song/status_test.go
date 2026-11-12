package song

import (
	"net/http"
	"net/http/httptest"
	"strings"
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

// TestCreateSiloEndpoint proves a fresh store can be populated over HTTP
// without song hijacking a host application's unknown POST paths.
func TestCreateSiloEndpoint(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()

	rec := do(t, h, http.MethodPost, SilosPath, strings.NewReader(`{"name":"demo"}`), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create silo: %d %q", rec.Code, rec.Body.String())
	}
	if !s.Store().HasSilo("demo") {
		t.Fatal("silo was not created")
	}

	if rec := do(t, h, http.MethodPost, SilosPath, strings.NewReader(`{"name":"demo"}`), nil); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate silo: expected 409, got %d", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, SilosPath, strings.NewReader(`{"name":""}`), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("empty name: expected 400, got %d", rec.Code)
	}
	if rec := do(t, h, http.MethodPost, SilosPath, strings.NewReader(`{"name":"api"}`), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("reserved name: expected 400, got %d", rec.Code)
	}

	// An unknown two-segment POST still belongs to the host, not to song.
	next := &recordingHandler{}
	s.Middleware(next).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/login/submit", nil))
	if !next.called {
		t.Fatal("unknown POST path must fall through to next")
	}
}

type recordingHandler struct{ called bool }

func (h *recordingHandler) ServeHTTP(http.ResponseWriter, *http.Request) { h.called = true }
