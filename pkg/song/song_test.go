package song

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"azzurrotech/song/pkg/component"
)

func newTestSong(t *testing.T) *Song {
	t.Helper()
	s, err := New(Config{Root: filepath.Join(t.TempDir(), "data")})
	if err != nil {
		t.Fatal(err)
	}
	// Ensure default components (song-badge, song-card) are registered
	for _, tag := range component.DefaultTags() {
		if err := s.Components().Register(tag); err != nil {
			t.Fatalf("register default tag %q: %v", tag.Name, err)
		}
	}
	return s
}

func do(t *testing.T, h http.Handler, method, target string, body io.Reader, hdrs map[string]string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, target, body)
	for k, v := range hdrs {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decodeResp(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

// TestComponentRuntimeRoutes proves the baseline runtime and the tag
// definitions are served by song itself.
func TestComponentRuntimeRoutes(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()
	rec := do(t, h, http.MethodGet, component.RuntimePath, nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "customElements.define") {
		t.Fatalf("runtime: %d %q", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, component.DefinitionsPath, nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("definitions: %d", rec.Code)
	}
	var defs struct {
		Components []string `json:"components"`
	}
	decodeResp(t, rec, &defs)
	// Verify we have component definitions (at least the default ones)
	if len(defs.Components) == 0 {
		t.Fatalf("expected at least default component definitions, got: %v", defs)
	}
	// The runtime path is owned by song in middleware mode.
	host := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "host") })
	if rec := do(t, s.Middleware(host), http.MethodGet, component.RuntimePath, nil, nil); rec.Body.String() == "host" {
		t.Fatal("runtime must be owned by song, not delegated")
	}
}
