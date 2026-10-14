package tmpl

import (
	"bytes"
	"html/template"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeTemplate creates a silo template set: <root>/<silo>/.templates/<name>.
func writeTemplate(t *testing.T, root, silo, name, content string) {
	t.Helper()
	dir := filepath.Join(root, silo, ".templates")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func render(t *testing.T, e *Engine, silo, name string, data any) string {
	t.Helper()
	var buf bytes.Buffer
	if err := e.Render(&buf, silo, name, data); err != nil {
		t.Fatalf("render %q: %v", name, err)
	}
	return buf.String()
}

func TestMultiFileTemplateSet(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "layout.tmpl", `{{define "layout"}}<html><head><title>{{block "title" .}}default{{end}}</title></head><body>{{template "content" .}}</body></html>{{end}}`)
	writeTemplate(t, root, "demo", "page.tmpl", `{{define "page"}}{{template "layout" .}}{{end}}{{define "title"}}{{.Title}} - song{{end}}{{define "content"}}<h1>{{.Title}}</h1>{{range .Items}}<p>{{.}}</p>{{end}}{{end}}`)

	data := map[string]any{"Title": "Hello <World>", "Items": []string{"one", "two"}}
	out := render(t, e, "demo", "page", data)
	if !strings.Contains(out, "Hello &lt;World&gt; - song") {
		t.Fatalf("expected escaped, composed title: %s", out)
	}
	if !strings.Contains(out, "<p>one</p><p>two</p>") {
		t.Fatalf("expected ranged items: %s", out)
	}
	// block fallback when page does not define title would be exercised by "about" absence.

	names, err := e.TemplateNames("demo")
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(names, ",")
	for _, want := range []string{"layout", "page", "title", "content"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("template names missing %q: %s", want, joined)
		}
	}
}

func TestTypedStringsNotEscaped(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "page.tmpl", `{{define "page"}}<p class="{{.Class}}">{{.Body}}</p><script>const d = {{.Data}};</script>{{end}}`)
	data := map[string]any{
		"Body":  template.HTML("<b>trusted</b>"),
		"Class": template.HTMLAttr("x-y"),
		"Data":  template.JS(`{ok: true, "n": 1}`),
	}
	out := render(t, e, "demo", "page", data)
	if !strings.Contains(out, "<b>trusted</b>") {
		t.Fatalf("HTML typed string must not be escaped: %s", out)
	}
	if !strings.Contains(out, "{ok: true") {
		t.Fatalf("JS typed string must not be escaped: %s", out)
	}

	// Plain strings ARE escaped in all contexts.
	data2 := map[string]any{
		"Body":  "<b>evil</b>",
		"Class": `" onmouseover="alert(1)`,
		"Data":  `";alert(1);//`,
	}
	out = render(t, e, "demo", "page", data2)
	if strings.Contains(out, "<b>evil</b>") {
		t.Fatal("plain string must be HTML-escaped")
	}
	if strings.Contains(out, `alert(1)//`) && !strings.Contains(out, `\u003c`) {
		t.Fatal("plain string in JS context must be escaped")
	}
}

func TestJSONFuncSafeInScript(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "page.tmpl", `{{define "page"}}<script>window.__DATA__ = {{json .}};</script>{{end}}`)
	out := render(t, e, "demo", "page", map[string]any{"name": "</script><script>alert(1)"})
	if strings.Contains(out, `"name":"</script>`) {
		t.Fatalf("json func must escape HTML-breaking sequences: %s", out)
	}
	if !strings.Contains(out, `\u003c/script\u003e`) {
		t.Fatalf("expected escaped closing script tag: %s", out)
	}
}

func TestDefaultFuncs(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "f.tmpl", `{{define "f"}}{{upper .a}}|{{lower .b}}|{{default "fallback" .missing}}|{{coalesce "" "" .a "x"}}|{{join .items "-"}}|{{add 1 2}}|{{div 10 4}}|{{ternary .ok "yes" "no"}}|{{slug "Hello, World!"}}|{{sha256 "x"}}|{{len (seq 5)}}{{end}}`)
	data := map[string]any{"a": "AbC", "b": "DeF", "items": []string{"p", "q"}, "ok": true}
	out := render(t, e, "demo", "f", data)
	for _, want := range []string{"ABC|def|fallback|AbC|p-q|3|2|yes|hello-world"} {
		if !strings.Contains(out, want) {
			t.Fatalf("missing %q in: %s", want, out)
		}
	}
	if !strings.Contains(out, "5") {
		t.Fatalf("seq/len should produce 5: %s", out)
	}
}

func TestDictKeysAndList(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "d.tmpl", `{{define "d"}}{{range keys (dict "b" 2 "a" 1)}}{{.}}{{end}}|{{len (list 1 2 3)}}{{end}}`)
	out := render(t, e, "demo", "d", nil)
	if !strings.Contains(out, "ab") {
		t.Fatalf("keys must be sorted: %s", out)
	}
	if !strings.Contains(out, "3") {
		t.Fatalf("list length: %s", out)
	}
}

func TestCustomFuncsOverride(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "c.tmpl", `{{define "c"}}{{shout "hi"}}{{end}}`)
	// Before custom func: undefined error.
	var buf bytes.Buffer
	if err := e.Render(&buf, "demo", "c", nil); err == nil {
		t.Fatal("expected error for undefined func")
	}
	e.AddFuncs(template.FuncMap{"shout": func(s string) string { return strings.ToUpper(s) + "!!" }})
	out := render(t, e, "demo", "c", nil)
	if out != "HI!!" {
		t.Fatalf("custom func not applied: %q", out)
	}
}

func TestReloadOnChange(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "p.tmpl", `{{define "p"}}v1{{end}}`)
	if out := render(t, e, "demo", "p", nil); out != "v1" {
		t.Fatalf("expected v1, got %q", out)
	}
	// Change content and ensure the engine notices the new mod time.
	writeTemplate(t, root, "demo", "p.tmpl", `{{define "p"}}v2-longer-content{{end}}`)
	if out := render(t, e, "demo", "p", nil); out != "v2-longer-content" {
		t.Fatalf("engine must reload on change, got %q", out)
	}
}

func TestMissingTemplateAndSilo(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "a.tmpl", `{{define "a"}}ok{{end}}`)
	var buf bytes.Buffer
	if err := e.Render(&buf, "demo", "nope", nil); err == nil {
		t.Fatal("expected error for missing template name")
	} else if !strings.Contains(err.Error(), "not defined") {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := e.Render(&buf, "other", "a", nil); err != ErrNoTemplates {
		t.Fatalf("expected ErrNoTemplates, got %v", err)
	}
}

func TestDelims(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	e.SetDelims("[[", "]]")
	writeTemplate(t, root, "demo", "p.tmpl", `[[define "p"]]hello [[.Name]][[end]]`)
	out := render(t, e, "demo", "p", map[string]any{"Name": "world"})
	if out != "hello world" {
		t.Fatalf("custom delims failed: %q", out)
	}
}

func TestWhitespaceTrimmingAndComment(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "p.tmpl", `{{define "p"}}A{{- if .On }}B{{- else }}C{{- end }}{{/* comment */}}D{{end}}`)
	out := render(t, e, "demo", "p", map[string]any{"On": true})
	if out != "ABD" {
		t.Fatalf("whitespace trimming failed: %q", out)
	}
}

func TestContentTypesHelpers(t *testing.T) {
	// contentTypeFor is exercised indirectly through the song layer; here we
	// merely assert the func helpers shape used by templates.
	if _, ok := defaultFuncs()["json"]; !ok {
		t.Fatal("json func missing")
	}
	fm := defaultFuncs()
	for _, name := range []string{"html", "js", "css", "url", "dict", "keys", "join", "default", "coalesce", "empty", "ternary", "seq", "add", "mod", "now", "date", "b64enc", "urlencode", "regexMatch", "regexReplace", "sha256", "fileSize", "slug", "abbrev", "title"} {
		if _, ok := fm[name]; !ok {
			t.Errorf("expected func %q in default set", name)
		}
	}
}

func containsName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// TestFormatOfByExtension proves the format selection rule: non-HTML text
// extensions use text/template, everything else uses html/template.
func TestFormatOfByExtension(t *testing.T) {
	for _, name := range []string{"note.txt", "readme.text", "doc.md", "doc.markdown", "UPPER.TXT"} {
		if got := FormatOf(name); got != FormatText {
			t.Errorf("FormatOf(%q) = %v, want text", name, got)
		}
	}
	for _, name := range []string{"page.html", "page.htm", "page.tmpl", "layout", "data.json", "app.js"} {
		if got := FormatOf(name); got != FormatHTML {
			t.Errorf("FormatOf(%q) = %v, want html", name, got)
		}
	}
}

// TestHTMLAndTextTemplateProcessing proves both standard templating packages
// are wired in: HTML files are escaped by html/template while text files are
// rendered verbatim by text/template.
func TestHTMLAndTextTemplateProcessing(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "page.html", `{{define "page"}}<h1>{{.Title}}</h1>{{end}}`)
	writeTemplate(t, root, "demo", "note.txt", `{{define "note"}}Hello {{.Title}}{{end}}`)

	data := map[string]any{"Title": "<World>"}

	htmlOut := render(t, e, "demo", "page", data)
	if !strings.Contains(htmlOut, "&lt;World&gt;") {
		t.Fatalf("html/template must escape: %q", htmlOut)
	}
	textOut := render(t, e, "demo", "note", data)
	if !strings.Contains(textOut, "Hello <World>") {
		t.Fatalf("text/template must not escape: %q", textOut)
	}
	if e.IsText("demo", "page") {
		t.Fatal("page.html must be an HTML template")
	}
	if !e.IsText("demo", "note") {
		t.Fatal("note.txt must be a text template")
	}

	names, err := e.TemplateNames("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !containsName(names, "page") || !containsName(names, "note") {
		t.Fatalf("TemplateNames must include both sets: %v", names)
	}

	// Explicit text rendering routes through text/template.
	var buf bytes.Buffer
	if err := e.RenderText(&buf, "demo", "note", data); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "Hello <World>" {
		t.Fatalf("RenderText output: %q", buf.String())
	}
}

// TestComponentTagsAvailableFromAnyTemplate proves system-defined HTML
// template tags are parsed into every silo's HTML set, so any HTML file can
// instantiate them.
func TestComponentTagsAvailableFromAnyTemplate(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	if err := e.AddComponent("song-badge", `<span class="badge">{{.}}</span>`); err != nil {
		t.Fatal(err)
	}
	writeTemplate(t, root, "demo", "page.html", `{{define "page"}}<div>{{template "song-badge" .Name}}</div>{{end}}`)

	out := render(t, e, "demo", "page", map[string]any{"Name": "Hi"})
	if !strings.Contains(out, `<span class="badge">Hi</span>`) {
		t.Fatalf("component tag not available: %q", out)
	}
	if got := e.Components(); len(got) != 1 || got[0] != "song-badge" {
		t.Fatalf("Components() = %v", got)
	}
	names, err := e.TemplateNames("demo")
	if err != nil {
		t.Fatal(err)
	}
	if !containsName(names, "song-badge") {
		t.Fatalf("component name missing from TemplateNames: %v", names)
	}
}

// TestAddComponentValidation rejects invalid registrations at registration
// time rather than at request time.
func TestAddComponentValidation(t *testing.T) {
	e := New(t.TempDir())
	if err := e.AddComponent("", "x"); err == nil {
		t.Fatal("empty name must be rejected")
	}
	if err := e.AddComponent("bad", ""); err == nil {
		t.Fatal("empty source must be rejected")
	}
	if err := e.AddComponent("broken", "{{if}}"); err == nil {
		t.Fatal("invalid template must be rejected")
	}
}

// TestRenderInline proves plain .html files can be processed on the fly with
// the component tags available, and that malformed sources report an error so
// the caller can fall back to verbatim serving.
func TestRenderInline(t *testing.T) {
	e := New(t.TempDir())
	if err := e.AddComponent("song-card", `<article>{{.}}</article>`); err != nil {
		t.Fatal(err)
	}
	out, err := e.RenderInline("demo", `<html><body>{{template "song-card" .}}</body></html>`, "X")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out, "<article>X</article>") {
		t.Fatalf("inline component render: %q", out)
	}
	// Literal markup passes through unchanged.
	out, err = e.RenderInline("demo", "<p>plain</p>", nil)
	if err != nil || out != "<p>plain</p>" {
		t.Fatalf("inline passthrough = %q, err=%v", out, err)
	}
	// Malformed source is an error, never a panic or partial write.
	if _, err := e.RenderInline("demo", "{{if}}", nil); err == nil {
		t.Fatal("malformed inline template must error")
	}
}

// TestTextTemplateReload proves text templates reload on change like HTML ones.
func TestTextTemplateReload(t *testing.T) {
	root := t.TempDir()
	e := New(root)
	writeTemplate(t, root, "demo", "n.txt", `{{define "n"}}v1{{end}}`)
	if out := render(t, e, "demo", "n", nil); out != "v1" {
		t.Fatalf("expected v1, got %q", out)
	}
	writeTemplate(t, root, "demo", "n.txt", `{{define "n"}}v2-changed{{end}}`)
	if out := render(t, e, "demo", "n", nil); out != "v2-changed" {
		t.Fatalf("text engine must reload on change, got %q", out)
	}
}
