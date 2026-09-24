package song

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSecret = "test-secret-key-0123456789abcdefghij"

func newTestSong(t *testing.T) *Song {
	t.Helper()
	s, err := New(Config{Root: filepath.Join(t.TempDir(), "data"), Secret: testSecret})
	if err != nil {
		t.Fatal(err)
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

func jsonBody(t *testing.T, v any) io.Reader {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(b)
}

func decodeResp(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("decode response %q: %v", rec.Body.String(), err)
	}
}

func createDemoSilo(t *testing.T, s *Song) (h http.Handler, name string) {
	t.Helper()
	h = s.Handler()
	if rec := do(t, h, http.MethodPost, "/api/song/silos", strings.NewReader("name=demo"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); rec.Code != http.StatusCreated {
		t.Fatalf("create silo: status %d body %s", rec.Code, rec.Body.String())
	}
	return h, "demo"
}

func TestHealth(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()
	rec := do(t, h, http.MethodGet, "/health", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status %d", rec.Code)
	}
	var body map[string]any
	decodeResp(t, rec, &body)
	if body["service"] != "song" || body["status"] != "healthy" {
		t.Fatalf("unexpected health: %v", body)
	}
}

func TestAPISilosLifecycle(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()

	// Create via JSON.
	if rec := do(t, h, http.MethodPost, "/api/song/silos", jsonBody(t, map[string]string{"name": "web"}),
		map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d %s", rec.Code, rec.Body.String())
	}
	// Duplicate -> 409.
	if rec := do(t, h, http.MethodPost, "/api/song/silos", strings.NewReader("name=web"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate should be 409, got %d", rec.Code)
	}
	// Reserved name rejected.
	if rec := do(t, h, http.MethodPost, "/api/song/silos", strings.NewReader("name=api"), nil); rec.Code != http.StatusBadRequest {
		t.Fatalf("reserved name should be 400, got %d", rec.Code)
	}
	// List.
	var listed struct {
		Silos []struct {
			Name string `json:"name"`
		} `json:"silos"`
	}
	if rec := do(t, h, http.MethodGet, "/api/song/silos", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	} else {
		decodeResp(t, rec, &listed)
	}
	if len(listed.Silos) != 1 || listed.Silos[0].Name != "web" {
		t.Fatalf("unexpected silos: %+v", listed.Silos)
	}
	// Delete.
	if rec := do(t, h, http.MethodDelete, "/api/song/silos/web", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodDelete, "/api/song/silos/web", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("delete missing should be 404, got %d", rec.Code)
	}
}

func TestFileCRUDViaJSON(t *testing.T) {
	s := newTestSong(t)
	h, silo := createDemoSilo(t, s)

	// Create.
	rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/file", jsonBody(t, map[string]any{
		"path": "index.html", "content": "<h1>Hi</h1>",
	}), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create file: %d %s", rec.Code, rec.Body.String())
	}
	// Duplicate -> 409.
	if rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/file", jsonBody(t, map[string]any{
		"path": "index.html", "content": "x",
	}), map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusConflict {
		t.Fatalf("duplicate file should be 409, got %d", rec.Code)
	}
	// Serve it.
	if rec := do(t, h, http.MethodGet, "/"+silo+"/index.html", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("serve: %d %s", rec.Code, rec.Body.String())
	} else if rec.Body.String() != "<h1>Hi</h1>" {
		t.Fatalf("content mismatch: %q", rec.Body.String())
	} else if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type: %q", ct)
	}
	// List.
	rec = do(t, h, http.MethodGet, "/api/song/silos/"+silo+"/files", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("list files: %d", rec.Code)
	}
	var listing struct {
		Files []struct {
			Name string `json:"name"`
			Path string `json:"path"`
		} `json:"files"`
	}
	decodeResp(t, rec, &listing)
	if len(listing.Files) != 1 || listing.Files[0].Name != "index.html" {
		t.Fatalf("unexpected listing: %+v", listing.Files)
	}
	// Read with content via API.
	rec = do(t, h, http.MethodGet, "/api/song/silos/"+silo+"/file?path=index.html&content=1", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("read: %d", rec.Code)
	}
	var readResp struct {
		Content string `json:"content"`
	}
	decodeResp(t, rec, &readResp)
	if readResp.Content != "<h1>Hi</h1>" {
		t.Fatalf("read content: %q", readResp.Content)
	}
	// Update.
	rec = do(t, h, http.MethodPut, "/api/song/silos/"+silo+"/file", jsonBody(t, map[string]any{
		"path": "index.html", "content": "<h1>Bye</h1>",
	}), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("update: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodGet, "/"+silo+"/index.html", nil, nil); rec.Body.String() != "<h1>Bye</h1>" {
		t.Fatalf("update not reflected: %q", rec.Body.String())
	}
	// Delete.
	if rec := do(t, h, http.MethodDelete, "/api/song/silos/"+silo+"/file?path=index.html", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("delete: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodDelete, "/api/song/silos/"+silo+"/file?path=index.html", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("double delete should be 404, got %d", rec.Code)
	}
}

func TestFileCRUDViaFormAndRaw(t *testing.T) {
	s := newTestSong(t)
	h, silo := createDemoSilo(t, s)

	// HTML form (urlencoded).
	form := "path=app.js&content=console.log(1)&encrypt=true"
	rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/file", strings.NewReader(form),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("form create: %d %s", rec.Code, rec.Body.String())
	}
	var created struct {
		File struct {
			Encrypted bool `json:"encrypted"`
		} `json:"file"`
	}
	decodeResp(t, rec, &created)
	if !created.File.Encrypted {
		t.Fatal("form-created file should be encrypted")
	}

	// Encrypted file serves decrypted content over HTTP.
	rec = do(t, h, http.MethodGet, "/"+silo+"/app.js", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("serve encrypted: %d %s", rec.Code, rec.Body.String())
	}
	if rec.Body.String() != "console.log(1)" {
		t.Fatalf("decrypted content mismatch: %q", rec.Body.String())
	}
	if rec.Header().Get("X-Song-Encrypted") != "1" {
		t.Fatal("expected X-Song-Encrypted header")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.Contains(ct, "javascript") {
		t.Fatalf("js content type: %q", ct)
	}

	// On-disk must be ciphertext.
	raw, err := os.ReadFile(filepath.Join(s.store.Root(), silo, "app.js"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "console.log") {
		t.Fatal("plaintext leaked to disk")
	}

	// Raw body create with path from query.
	rawBody := strings.NewReader("Hello from raw body")
	rec = do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/file?path=raw.txt", rawBody,
		map[string]string{"Content-Type": "text/plain"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("raw create: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodGet, "/"+silo+"/raw.txt", nil, nil); rec.Body.String() != "Hello from raw body" {
		t.Fatalf("raw content mismatch: %q", rec.Body.String())
	}
}

func TestMultipartUpload(t *testing.T) {
	s := newTestSong(t)
	h, silo := createDemoSilo(t, s)

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	mw.WriteField("dir", "assets")
	fw, _ := mw.CreateFormFile("file", "logo.svg")
	fw.Write([]byte("<svg/>"))
	mw.Close()

	rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/upload", &buf,
		map[string]string{"Content-Type": mw.FormDataContentType()})
	if rec.Code != http.StatusOK {
		t.Fatalf("upload: %d %s", rec.Code, rec.Body.String())
	}
	var uploads struct {
		Uploads []struct {
			Path  string `json:"path"`
			Error string `json:"error"`
		} `json:"uploads"`
	}
	decodeResp(t, rec, &uploads)
	if len(uploads.Uploads) != 1 || uploads.Uploads[0].Error != "" {
		t.Fatalf("unexpected uploads: %+v", uploads.Uploads)
	}
	if rec := do(t, h, http.MethodGet, "/"+silo+"/assets/logo.svg", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("serve uploaded: %d %s", rec.Code, rec.Body.String())
	}
}

func TestIndexRedirectByHeader(t *testing.T) {
	s := newTestSong(t)
	h, silo := createDemoSilo(t, s)
	mustPut(t, h, "/api/song/silos/"+silo+"/file", map[string]any{
		"path": "index.html", "content": "default-index",
	})
	mustPut(t, h, "/api/song/silos/"+silo+"/file", map[string]any{
		"path": "index.mobile.html", "content": "mobile-index",
	})
	mustPut(t, h, "/api/song/silos/"+silo+"/file", map[string]any{
		"path": "index.desktop.html", "content": "desktop-index",
	})

	// IndexRules drive header-based index document selection.
	if rec := do(t, h, http.MethodPut, "/api/song/silos/"+silo+"/meta", jsonBody(t, map[string]any{
		"index_rules": []map[string]any{
			{"header": "X-Index-Variant", "value": "mobile", "file": "index.mobile.html"},
			{"header": "X-Index-Variant", "value": "desktop", "file": "index.desktop.html"},
			{"header": "User-Agent", "pattern": "(?i)mobile|android|iphone", "file": "index.mobile.html"},
		},
	}), map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusOK {
		t.Fatalf("set index meta: %d %s", rec.Code, rec.Body.String())
	}

	// No header -> index.html.
	if rec := do(t, h, http.MethodGet, "/"+silo+"/", nil, nil); rec.Body.String() != "default-index" {
		t.Fatalf("default index: %q", rec.Body.String())
	}
	// Variant header -> index.mobile.html.
	if rec := do(t, h, http.MethodGet, "/"+silo+"/", nil, map[string]string{
		"X-Index-Variant": "mobile",
	}); rec.Body.String() != "mobile-index" {
		t.Fatalf("mobile index: %q status %d", rec.Body.String(), rec.Code)
	}
	// User-Agent pattern.
	if rec := do(t, h, http.MethodGet, "/"+silo+"/", nil, map[string]string{
		"User-Agent": "Mozilla/5.0 (iPhone; CPU iPhone OS 17)",
	}); rec.Body.String() != "mobile-index" {
		t.Fatalf("ua index: %q", rec.Body.String())
	}
	// No trailing slash on a file is served directly.
	if rec := do(t, h, http.MethodGet, "/"+silo+"/index.mobile.html", nil, nil); rec.Body.String() != "mobile-index" {
		t.Fatalf("direct file: %q", rec.Body.String())
	}
}

func mustPut(t *testing.T, h http.Handler, target string, payload map[string]any) {
	t.Helper()
	rec := do(t, h, http.MethodPost, target, jsonBody(t, payload),
		map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusCreated {
		t.Fatalf("mustPut %s: %d %s", target, rec.Code, rec.Body.String())
	}
}

func TestServerSideRender(t *testing.T) {
	s := newTestSong(t)
	h, silo := createDemoSilo(t, s)

	// Templates live in the silo.
	if rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/file", strings.NewReader(`path=.templates/layout.tmpl&content=`+
		`{{define "layout"}}<html><head><title>{{block "title" .}}t{{end}}</title></head><body>{{template "content" .}}|{{upper (default "" .title)}}|{{json .}}</body></html>{{end}}`),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); rec.Code != http.StatusCreated {
		t.Fatalf("create layout: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/file", strings.NewReader(`path=.templates/page.tmpl&content=`+
		`{{define "page"}}{{template "layout" .}}{{end}}{{define "content"}}Hello {{.name}}{{end}}`),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"}); rec.Code != http.StatusCreated {
		t.Fatalf("create page: %d %s", rec.Code, rec.Body.String())
	}

	// Render via JSON body.
	rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/render", jsonBody(t, map[string]any{
		"template": "page",
		"data":     map[string]any{"name": "Song", "title": "Song"},
	}), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("render: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hello Song") || !strings.Contains(body, "SONG") {
		t.Fatalf("render output: %q", body)
	}
	if !strings.Contains(body, "&#34;name&#34;") || !strings.Contains(body, "&#34;Song&#34;") {
		t.Fatalf("json func should serialize data (escaped in html context): %q", body)
	}
	// Typed json inside <body> gets escaped by the html context; fine.

	// Render via HTML form.
	rec = do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/render", strings.NewReader("template=page&name=Form"),
		map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
	if !strings.Contains(rec.Body.String(), "Hello Form") {
		t.Fatalf("form render: %q", rec.Body.String())
	}

	// Render via GET with query params.
	rec = do(t, h, http.MethodGet, "/api/song/silos/"+silo+"/render?template=page&name=Query", nil, nil)
	if !strings.Contains(rec.Body.String(), "Hello Query") {
		t.Fatalf("get render: %q", rec.Body.String())
	}

	// Template route registered in meta + served at /silo/route.
	if rec := do(t, h, http.MethodPut, "/api/song/silos/"+silo+"/meta", jsonBody(t, map[string]any{
		"template_routes": map[string]string{"greet": "page"},
	}), map[string]string{"Content-Type": "application/json"}); rec.Code != http.StatusOK {
		t.Fatalf("set meta: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/"+silo+"/greet?name=Route", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "Hello Route") {
		t.Fatalf("route render: %d %q", rec.Code, rec.Body.String())
	}
	if rec.Header().Get("X-Song-Template") != "page" {
		t.Fatal("missing X-Song-Template header")
	}
}

func TestMiddlewareDelegation(t *testing.T) {
	s := newTestSong(t)
	_, _ = createDemoSilo(t, s)

	host := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Host", "yes")
		fmt.Fprint(w, "host:"+r.URL.Path)
	})
	h := s.Middleware(host)

	// Owned routes.
	rec := do(t, h, http.MethodGet, "/health", nil, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "host:") {
		t.Fatalf("health should be owned: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/api/song/silos", nil, nil)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), "host:") {
		t.Fatalf("api should be owned: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/demo/", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("silo without index should 404 via song: %d", rec.Code)
	}
	// Delegated routes.
	rec = do(t, h, http.MethodGet, "/", nil, nil)
	if rec.Body.String() != "host:/" || rec.Header().Get("X-Host") != "yes" {
		t.Fatalf("root must be delegated: %q", rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/some/other/path", nil, nil)
	if rec.Body.String() != "host:/some/other/path" {
		t.Fatalf("non-silo path must be delegated: %q", rec.Body.String())
	}
	// Reserved path segments never become silos.
	rec = do(t, h, http.MethodGet, "/api/whatever", nil, nil)
	if rec.Body.String() != "host:/api/whatever" {
		t.Fatalf("/api must not be treated as a silo: %q", rec.Body.String())
	}
}

func TestAdminUI(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()
	rec := do(t, h, http.MethodGet, "/admin/", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("admin ui: %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "song") {
		t.Fatal("admin ui body missing")
	}
	if rec := do(t, h, http.MethodGet, "/admin/app.js", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("admin js: %d", rec.Code)
	}

	// Disabled UI.
	s2, err := New(Config{Root: filepath.Join(t.TempDir(), "d"), Secret: testSecret, DisableUI: true})
	if err != nil {
		t.Fatal(err)
	}
	h2 := s2.Handler()
	if rec := do(t, h2, http.MethodGet, "/admin/", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("admin should be disabled: %d", rec.Code)
	}
}

func TestMagicLinkAPI(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()
	rec := do(t, h, http.MethodPost, "/api/auth/generate", jsonBody(t, map[string]string{
		"user_id": "a@example.com",
	}), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("generate: %d %s", rec.Code, rec.Body.String())
	}
	var gen struct {
		MagicLink string `json:"magic_link"`
	}
	decodeResp(t, rec, &gen)
	if gen.MagicLink == "" {
		t.Fatal("missing magic link")
	}
	rec = do(t, h, http.MethodPost, "/api/auth/validate", jsonBody(t, map[string]string{
		"link": gen.MagicLink, "user_id": "a@example.com",
	}), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusOK {
		t.Fatalf("validate: %d %s", rec.Code, rec.Body.String())
	}
	var val struct {
		Valid bool `json:"valid"`
	}
	decodeResp(t, rec, &val)
	if !val.Valid {
		t.Fatal("link should be valid")
	}
}

func TestPathTraversalViaAPI(t *testing.T) {
	s := newTestSong(t)
	h, silo := createDemoSilo(t, s)
	for _, path := range []string{"../evil", ".song/meta.json", "a/../../x"} {
		rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/file", strings.NewReader("path="+strings.ReplaceAll(path, "/", "%2F")+"&content=x"),
			map[string]string{"Content-Type": "application/x-www-form-urlencoded"})
		if rec.Code == http.StatusCreated {
			t.Fatalf("traversal %q must be rejected", path)
		}
	}
}

func TestSeedDemo(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()
	if err := SeedDemo(s); err != nil {
		t.Fatal(err)
	}
	// Mobile index via header.
	rec := do(t, h, http.MethodGet, "/demo/", nil, map[string]string{"X-Index-Variant": "mobile"})
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "mobile") {
		t.Fatalf("seeded mobile index: %d %q", rec.Code, rec.Body.String())
	}
	// Template route.
	rec = do(t, h, http.MethodGet, "/demo/about", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "About song") {
		t.Fatalf("seeded about route: %d %q", rec.Code, rec.Body.String())
	}
	// Encrypted file served decrypted.
	rec = do(t, h, http.MethodGet, "/demo/secrets/notes.txt", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("seeded secret: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "AES-256-GCM") {
		t.Fatalf("secret not decrypted: %q", rec.Body.String())
	}
	// Re-seeding is idempotent.
	if err := SeedDemo(s); err != nil {
		t.Fatal(err)
	}
}

func TestRenderErrorsAreSurfaced(t *testing.T) {
	s := newTestSong(t)
	h, silo := createDemoSilo(t, s)
	// Missing template -> 404 (no .templates dir yet).
	rec := do(t, h, http.MethodPost, "/api/song/silos/"+silo+"/render", jsonBody(t, map[string]any{
		"template": "nope", "data": map[string]any{},
	}), map[string]string{"Content-Type": "application/json"})
	if rec.Code != http.StatusNotFound {
		t.Fatalf("missing templates should 404, got %d", rec.Code)
	}
}

func TestUnknownEndpoints(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()
	if rec := do(t, h, http.MethodGet, "/api/song/silos/nope/files", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing silo: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/api/song/silos", nil, nil); rec.Code != http.StatusOK {
		t.Fatalf("list: %d", rec.Code)
	}
}
