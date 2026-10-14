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
	return s
}

// do issues a request against h and returns the recorded response.
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

// newDemoSilo registers the "demo" silo in the store and returns the song
// Handler plus the silo name. Silos are directories under the store root, so
// tests set them up through the store rather than through an HTTP API.
func newDemoSilo(t *testing.T, s *Song) (h http.Handler, name string) {
	t.Helper()
	if err := s.store.CreateSilo("demo"); err != nil {
		t.Fatalf("create silo: %v", err)
	}
	return s.Handler(), "demo"
}

// putFile writes a resource into a silo directly through the store.
func putFile(t *testing.T, s *Song, silo, rel, content string) {
	t.Helper()
	if _, err := s.store.Create(silo, rel, content); err != nil {
		t.Fatalf("create %s/%s: %v", silo, rel, err)
	}
}

// templateRoute registers a server-side template route in the silo metadata.
func templateRoute(t *testing.T, s *Song, silo, route, tpl string) {
	t.Helper()
	if err := s.store.SetTemplateRoute(silo, route, tpl); err != nil {
		t.Fatalf("set template route %q: %v", route, err)
	}
}

// TestMiddlewareDelegation covers the README's middleware design: song serves
// only the paths it owns (registered silos, the Web Component runtime) and
// delegates everything else to the next handler untouched.
func TestMiddlewareDelegation(t *testing.T) {
	s := newTestSong(t)
	_, silo := newDemoSilo(t, s)
	putFile(t, s, silo, "index.html", "<h1>demo</h1>")

	host := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Host", "yes")
		fmt.Fprint(w, "host:"+r.URL.Path)
	})
	h := s.Middleware(host)

	// Owned routes are served by song, never reaching the host.
	if rec := do(t, h, http.MethodGet, "/"+silo+"/", nil, nil); rec.Code != http.StatusOK ||
		strings.Contains(rec.Body.String(), "host:") {
		t.Fatalf("silo must be owned by song: %d %q", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodGet, component.RuntimePath, nil, nil); strings.Contains(rec.Body.String(), "host:") {
		t.Fatal("component runtime must be owned by song")
	}
	// Everything else falls through to the host.
	rec := do(t, h, http.MethodGet, "/", nil, nil)
	if rec.Body.String() != "host:/" || rec.Header().Get("X-Host") != "yes" {
		t.Fatalf("root must be delegated: %q", rec.Body.String())
	}
	if rec := do(t, h, http.MethodGet, "/some/other/path", nil, nil); rec.Body.String() != "host:/some/other/path" {
		t.Fatalf("non-silo path must be delegated: %q", rec.Body.String())
	}
	// An unknown first segment is not a silo, so it is delegated too.
	if rec := do(t, h, http.MethodGet, "/nope/x", nil, nil); rec.Body.String() != "host:/nope/x" {
		t.Fatalf("unknown silo must be delegated: %q", rec.Body.String())
	}
	// Reserved path segments never become silos.
	if rec := do(t, h, http.MethodGet, "/api/whatever", nil, nil); rec.Body.String() != "host:/api/whatever" {
		t.Fatalf("reserved segment must not be treated as a silo: %q", rec.Body.String())
	}
}

// TestStaticResourceUpsert covers the README's GET/POST surface: a POST to
// /{silo}/{path} creates the resource from the raw request body and replaces it
// on the next POST, while GET serves it back.
func TestStaticResourceUpsert(t *testing.T) {
	s := newTestSong(t)
	h, silo := newDemoSilo(t, s)

	rec := do(t, h, http.MethodPost, "/"+silo+"/greeting.txt", strings.NewReader("hello v1"), nil)
	if rec.Code != http.StatusCreated {
		t.Fatalf("create resource: %d %s", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/"+silo+"/greeting.txt", nil, nil)
	if rec.Code != http.StatusOK || rec.Body.String() != "hello v1" {
		t.Fatalf("GET after create: %d %q", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("content type: %q", ct)
	}

	rec = do(t, h, http.MethodPost, "/"+silo+"/greeting.txt", strings.NewReader("hello v2"), nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("update resource: %d %s", rec.Code, rec.Body.String())
	}
	if rec := do(t, h, http.MethodGet, "/"+silo+"/greeting.txt", nil, nil); rec.Body.String() != "hello v2" {
		t.Fatalf("GET after update: %q", rec.Body.String())
	}

	// Traversal through the upsert surface is rejected.
	if rec := do(t, h, http.MethodPost, "/"+silo+"/../evil", strings.NewReader("x"), nil); rec.Code == http.StatusCreated {
		t.Fatal("path traversal must be rejected")
	}
	if rec := do(t, h, http.MethodPost, "/"+silo+"/.song%2Fmeta.json", strings.NewReader("x"), nil); rec.Code == http.StatusCreated {
		t.Fatal("writing into the metadata tree must be rejected")
	}
	// A missing silo falls through (server mode -> 404).
	if rec := do(t, h, http.MethodPost, "/nope/file.txt", strings.NewReader("x"), nil); rec.Code != http.StatusNotFound {
		t.Fatalf("missing silo: %d", rec.Code)
	}
}

// TestDirectoryServesIndex proves a directory request serves its index.html.
func TestDirectoryServesIndex(t *testing.T) {
	s := newTestSong(t)
	h, silo := newDemoSilo(t, s)
	putFile(t, s, silo, "index.html", "<h1>home</h1>")
	putFile(t, s, silo, "sub/index.html", "<h1>sub</h1>")

	rec := do(t, h, http.MethodGet, "/"+silo+"/", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "home") {
		t.Fatalf("root index: %d %q", rec.Code, rec.Body.String())
	}
	rec = do(t, h, http.MethodGet, "/"+silo+"/sub/", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "sub") {
		t.Fatalf("sub index: %d %q", rec.Code, rec.Body.String())
	}
	// A directory without an index document is a 404, not an endless redirect.
	if rec := do(t, h, http.MethodGet, "/"+silo+"/empty/", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("directory without index: %d", rec.Code)
	}
}

// TestHTMLComponentServing proves any served HTML file that uses a system tag
// gets the component definitions and runtime injected, while plain HTML is
// served byte-for-byte.
func TestHTMLComponentServing(t *testing.T) {
	s := newTestSong(t)
	h, silo := newDemoSilo(t, s)

	componentHTML := `<!DOCTYPE html><html><body><h1>Widget</h1><song-badge>New</song-badge></body></html>`
	if rec := do(t, h, http.MethodPost, "/"+silo+"/widget.html", strings.NewReader(componentHTML), nil); rec.Code != http.StatusCreated {
		t.Fatalf("create widget: %d %s", rec.Code, rec.Body.String())
	}
	rec := do(t, h, http.MethodGet, "/"+silo+"/widget.html", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET widget: %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `<song-badge>New</song-badge>`) {
		t.Fatalf("original markup must be preserved: %q", body)
	}
	if !strings.Contains(body, `data-song-component="song-badge"`) {
		t.Fatalf("component definitions must be injected: %q", body)
	}
	if !strings.Contains(body, component.RuntimePath) {
		t.Fatalf("runtime script must be injected: %q", body)
	}
	if rec.Header().Get("X-Song-Template-Processed") != "1" {
		t.Fatal("expected X-Song-Template-Processed")
	}

	if rec := do(t, h, http.MethodPost, "/"+silo+"/plain.html", strings.NewReader("<p>plain</p>"), nil); rec.Code != http.StatusCreated {
		t.Fatalf("create plain: %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/"+silo+"/plain.html", nil, nil)
	if rec.Body.String() != "<p>plain</p>" {
		t.Fatalf("plain html must be untouched: %q", rec.Body.String())
	}
	if rec.Header().Get("X-Song-Template-Processed") != "" {
		t.Fatal("plain html must not be template-processed")
	}

	// A component page that is not valid Go template syntax is still served
	// with the component definitions injected.
	broken := `<song-badge>x</song-badge>{{if}}`
	if rec := do(t, h, http.MethodPost, "/"+silo+"/broken.html", strings.NewReader(broken), nil); rec.Code != http.StatusCreated {
		t.Fatalf("create broken: %d", rec.Code)
	}
	rec = do(t, h, http.MethodGet, "/"+silo+"/broken.html", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `data-song-component="song-badge"`) {
		t.Fatalf("broken component page must still inject: %d %q", rec.Code, rec.Body.String())
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
		Count int `json:"count"`
		Tags  []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	decodeResp(t, rec, &defs)
	if defs.Count == 0 || len(defs.Tags) != defs.Count {
		t.Fatalf("unexpected definitions: %+v", defs)
	}
	// The runtime path is owned by song in middleware mode.
	host := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, "host") })
	if rec := do(t, s.Middleware(host), http.MethodGet, component.RuntimePath, nil, nil); rec.Body.String() == "host" {
		t.Fatal("runtime must be owned by song, not delegated")
	}
}

// TestServerSideRender covers the README's template processing: a named Go
// template stored in the silo is rendered server-side and served over GET.
func TestServerSideRender(t *testing.T) {
	s := newTestSong(t)
	h, silo := newDemoSilo(t, s)
	putFile(t, s, silo, ".templates/layout.tmpl",
		`{{define "layout"}}<html><head><title>{{block "title" .}}t{{end}}</title></head><body>{{template "content" .}}|{{upper (default "" .title)}}|{{json .}}</body></html>{{end}}`)
	putFile(t, s, silo, ".templates/page.tmpl",
		`{{define "page"}}{{template "layout" .}}{{end}}{{define "content"}}Hello {{.name}}{{end}}`)
	templateRoute(t, s, silo, "greet", "page")

	rec := do(t, h, http.MethodGet, "/"+silo+"/greet?name=Route&title=Song", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("render: %d %s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Hello Route") || !strings.Contains(body, "SONG") {
		t.Fatalf("render output: %q", body)
	}
	// The json func output is escaped by the html/template context.
	if !strings.Contains(body, "&#34;name&#34;") || !strings.Contains(body, "&#34;Route&#34;") {
		t.Fatalf("json func should serialize data (escaped in html context): %q", body)
	}
	if rec.Header().Get("X-Song-Template") != "page" {
		t.Fatal("missing X-Song-Template header")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("html content type: %q", ct)
	}
}

// TestTextTemplateRendering proves .txt templates are processed with
// text/template (no HTML escaping) and reported as text/plain.
func TestTextTemplateRendering(t *testing.T) {
	s := newTestSong(t)
	h, silo := newDemoSilo(t, s)
	putFile(t, s, silo, ".templates/note.txt", `{{define "note"}}plain {{.text}}{{end}}`)
	templateRoute(t, s, silo, "note", "note")

	rec := do(t, h, http.MethodGet, "/"+silo+"/note?text=%3Cb%3E", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("render text: %d %s", rec.Code, rec.Body.String())
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/plain") {
		t.Fatalf("text content type: %q", ct)
	}
	if !strings.Contains(rec.Body.String(), "plain <b>") {
		t.Fatalf("text template must not HTML-escape: %q", rec.Body.String())
	}
	if rec.Header().Get("X-Song-Template") != "note" {
		t.Fatal("missing X-Song-Template header")
	}
}

// TestServerSideComponentTag proves system component tags resolve inside a
// silo's server-rendered HTML templates.
func TestServerSideComponentTag(t *testing.T) {
	s := newTestSong(t)
	h, silo := newDemoSilo(t, s)
	putFile(t, s, silo, ".templates/page.html",
		`{{define "page"}}<main>{{template "song-card" .}}</main>{{end}}`)
	templateRoute(t, s, silo, "page", "page")
	rec := do(t, h, http.MethodGet, "/"+silo+"/page", nil, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("render: %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), `<slot name="title">`) {
		t.Fatalf("system component markup missing: %q", rec.Body.String())
	}
}

// TestRenderErrorsAreSurfaced proves a broken template route reports the
// failure instead of silently serving the wrong thing.
func TestRenderErrorsAreSurfaced(t *testing.T) {
	s := newTestSong(t)
	h, silo := newDemoSilo(t, s)
	templateRoute(t, s, silo, "missing", "nope")
	rec := do(t, h, http.MethodGet, "/"+silo+"/missing", nil, nil)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("missing template should 500, got %d %s", rec.Code, rec.Body.String())
	}
	if !strings.Contains(rec.Body.String(), "render error") {
		t.Fatalf("error should be surfaced: %q", rec.Body.String())
	}
}

// TestUnknownPathsAreNotFound proves paths song does not own produce a 404 in
// server mode rather than an accidental listing or redirect.
func TestUnknownPathsAreNotFound(t *testing.T) {
	s := newTestSong(t)
	h := s.Handler()
	if rec := do(t, h, http.MethodGet, "/", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("root: %d", rec.Code)
	}
	if rec := do(t, h, http.MethodGet, "/demo/anything", nil, nil); rec.Code != http.StatusNotFound {
		t.Fatalf("unknown silo: %d", rec.Code)
	}
}

// TestConfigComponents proves a host can register its own web component and
// immediately use it both server-side and in served HTML.
func TestConfigComponents(t *testing.T) {
	s, err := New(Config{
		Root: filepath.Join(t.TempDir(), "data"),
		Components: []component.Tag{
			{Name: "host-chip", Markup: "<strong>{{.}}</strong>"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Components().Get("host-chip"); !ok {
		t.Fatal("host component not registered")
	}
	h, silo := newDemoSilo(t, s)
	// Client side: a served HTML file using the custom element is injected.
	if rec := do(t, h, http.MethodPost, "/"+silo+"/chip.html", strings.NewReader("<host-chip>x</host-chip>"), nil); rec.Code != http.StatusCreated {
		t.Fatalf("create: %d", rec.Code)
	}
	rec := do(t, h, http.MethodGet, "/"+silo+"/chip.html", nil, nil)
	if !strings.Contains(rec.Body.String(), `data-song-component="host-chip"`) {
		t.Fatalf("host component not injected: %q", rec.Body.String())
	}
	// Server side: {{template "host-chip" .}} resolves.
	putFile(t, s, silo, ".templates/chip.html", `{{define "chip"}}{{template "host-chip" .name}}{{end}}`)
	templateRoute(t, s, silo, "chip", "chip")
	rec = do(t, h, http.MethodGet, "/"+silo+"/chip?name=Zed", nil, nil)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "<strong>Zed</strong>") {
		t.Fatalf("host component SSR: %d %q", rec.Code, rec.Body.String())
	}
}
