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
