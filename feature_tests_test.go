package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"azzurrotech/song/pkg/song"
)

// newTestHandler builds a Song over an isolated store root.
func newTestHandler(t *testing.T) (http.Handler, *song.Song) {
	t.Helper()
	srv, err := song.New(song.Config{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("song.New: %v", err)
	}
	return srv.Middleware(http.NotFoundHandler()), srv
}

// createSilo creates a silo through the HTTP API.
func createSilo(t *testing.T, handler http.Handler, name string) {
	t.Helper()
	body := strings.NewReader(`{"name":"` + name + `"}`)
	req := httptest.NewRequest(http.MethodPost, "/_song/silos", body)
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create silo %q: expected 201, got %d: %s", name, rec.Code, rec.Body.String())
	}
}

// SONG-MW-001: Test middleware
func TestSONG_MW_001_Middleware(t *testing.T) {
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("next"))
	})
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next.ServeHTTP(w, r)
	})
	req := httptest.NewRequest("GET", "/", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("SONG-MW-001: expected 200, got %d", rec.Code)
	}
}

// SONG-RES-001: Test GET resource
func TestSONG_RES_001_GetResource(t *testing.T) {
	handler, _ := newTestHandler(t)
	createSilo(t, handler, "test-silo")

	// Test that static resources are served
	createResp := httptest.NewRecorder()
	createReq := httptest.NewRequest("POST", "/test-silo/hello.txt", strings.NewReader("Hello World"))
	handler.ServeHTTP(createResp, createReq)
	if createResp.Code != http.StatusCreated {
		t.Fatalf("SONG-RES-001: expected 201, got %d", createResp.Code)
	}

	// Test that we can retrieve the resource
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test-silo/hello.txt", nil)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("SONG-RES-001: expected 200, got %d", resp.Code)
	}
	if resp.Body.String() != "Hello World" {
		t.Fatalf("SONG-RES-001: expected 'Hello World', got '%s'", resp.Body.String())
	}
}

// SONG-RES-002: Test POST upsert
func TestSONG_RES_002_PostUpsert(t *testing.T) {
	handler, _ := newTestHandler(t)
	createSilo(t, handler, "test-silo")

	// Test create new resource
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("POST", "/test-silo/new-file.txt", strings.NewReader("New content"))
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusCreated {
		t.Fatalf("SONG-RES-002: expected 201, got %d", resp.Code)
	}

	// Test update existing resource
	resp = httptest.NewRecorder()
	req = httptest.NewRequest("POST", "/test-silo/new-file.txt", strings.NewReader("Updated content"))
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("SONG-RES-002: expected 200, got %d", resp.Code)
	}

	var result map[string]interface{}
	if err := json.Unmarshal(resp.Body.Bytes(), &result); err != nil {
		t.Fatalf("SONG-RES-002: failed to parse JSON response: %v", err)
	}
	if file, ok := result["file"]; !ok {
		t.Fatalf("SONG-RES-002: expected file in response, got none")
	} else if fileMap, ok := file.(map[string]interface{}); !ok || fileMap["name"] != "new-file.txt" {
		t.Fatalf("SONG-RES-002: expected file name 'new-file.txt', got %v", file)
	}
}

// SONG-TMPL-001: Test template processing
func TestSONG_TMPL_001_TemplateProcessing(t *testing.T) {
	handler, srv := newTestHandler(t)

	if err := srv.Store().CreateSilo("test-silo"); err != nil {
		t.Fatalf("SONG-TMPL-001: create silo: %v", err)
	}
	dir := filepath.Join(srv.Store().Root(), "test-silo", ".templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("SONG-TMPL-001: create template dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "test.tmpl"), []byte("Hello {{.name}}"), 0o644); err != nil {
		t.Fatalf("SONG-TMPL-001: failed to write template: %v", err)
	}
	if err := srv.Store().SetTemplateRoute("test-silo", "test", "test.tmpl"); err != nil {
		t.Fatalf("SONG-TMPL-001: register route: %v", err)
	}

	// Test template rendering via metadata
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/test-silo/test?name=World", nil)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("SONG-TMPL-001: expected 200, got %d", resp.Code)
	}
	if !strings.Contains(resp.Body.String(), "Hello World") {
		t.Fatalf("SONG-TMPL-001: expected to contain 'Hello World', got '%s'", resp.Body.String())
	}
}

// SONG-WC-001: Test web components
func TestSONG_WC_001_WebComponents(t *testing.T) {
	handler, _ := newTestHandler(t)

	// Test that web component definitions are available
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/_song/components", nil)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("SONG-WC-001: expected 200 for components, got %d", resp.Code)
	}

	// Test that web component runtime is available
	resp = httptest.NewRecorder()
	req = httptest.NewRequest("GET", "/_song/components.js", nil)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("SONG-WC-001: expected 200 for runtime, got %d", resp.Code)
	}
	if !strings.Contains(resp.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("SONG-WC-001: expected JavaScript content type")
	}
}

// SONG-VK-001: Test viewkit
func TestSONG_VK_001_ViewkitJS(t *testing.T) {
	handler, _ := newTestHandler(t)

	// Test that viewkit library is available at its dedicated path
	resp := httptest.NewRecorder()
	req := httptest.NewRequest("GET", "/_song/viewkit.js", nil)
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusOK {
		t.Fatalf("SONG-VK-001: expected 200, got %d", resp.Code)
	}
	if !strings.Contains(resp.Header().Get("Content-Type"), "text/javascript") {
		t.Fatalf("SONG-VK-001: expected JavaScript content type")
	}
}
