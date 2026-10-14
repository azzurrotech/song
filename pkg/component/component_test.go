package component

import (
	"strings"
	"testing"
)

func TestDefaultRegistry(t *testing.T) {
	r := DefaultRegistry()
	if r.Len() == 0 {
		t.Fatal("expected built-in tags")
	}
	for _, name := range []string{"song-badge", "song-card"} {
		tag, ok := r.Get(name)
		if !ok {
			t.Fatalf("missing built-in tag %q", name)
		}
		if tag.Markup == "" {
			t.Fatalf("tag %q has no markup", name)
		}
	}
}

func TestRegisterValidation(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Tag{Name: "BadName", Markup: "<span></span>"}); err == nil {
		t.Fatal("uppercase name must be rejected")
	}
	if err := r.Register(Tag{Name: "nodash", Markup: "<span></span>"}); err == nil {
		t.Fatal("name without a hyphen must be rejected")
	}
	if err := r.Register(Tag{Name: "song-x", Markup: "  "}); err == nil {
		t.Fatal("empty markup must be rejected")
	}
	if err := r.Register(Tag{Name: "song-x", Markup: "<span></span>"}); err != nil {
		t.Fatalf("valid tag rejected: %v", err)
	}
	// Re-registering replaces but keeps order.
	if err := r.Register(Tag{Name: "song-x", Markup: "<b></b>"}); err != nil {
		t.Fatal(err)
	}
	if r.Len() != 1 {
		t.Fatalf("expected 1 tag, got %d", r.Len())
	}
	if tag, _ := r.Get("song-x"); tag.Markup != "<b></b>" {
		t.Fatalf("re-register did not replace: %q", tag.Markup)
	}
}

func TestDefinitionsAndServerTemplate(t *testing.T) {
	tag := Tag{
		Name:   "song-badge",
		Styles: ":host{color:red}",
		Markup: "<slot></slot>",
	}
	defs := tag.ServerTemplate()
	if !strings.Contains(defs, `{{define "song-badge"}}`) || !strings.Contains(defs, "{{end}}") {
		t.Fatalf("ServerTemplate not a define block: %q", defs)
	}
	if !strings.Contains(defs, ":host{color:red}") || !strings.Contains(defs, "<slot></slot>") {
		t.Fatalf("ServerTemplate missing styles/markup: %q", defs)
	}
}

func TestDefinitions(t *testing.T) {
	r := DefaultRegistry()
	defs := r.Definitions()
	for _, name := range r.Names() {
		if !strings.Contains(defs, `data-song-component="`+name+`"`) {
			t.Fatalf("definitions missing %q: %s", name, defs)
		}
	}
	if !strings.Contains(defs, "<template") {
		t.Fatalf("definitions must use <template>: %s", defs)
	}
}

func TestUsedInAndInject(t *testing.T) {
	r := DefaultRegistry()

	plain := `<!DOCTYPE html><html><body><h1>plain</h1></body></html>`
	if r.UsedIn(plain) {
		t.Fatal("plain document must not use components")
	}
	if got := r.Inject(plain); got != plain {
		t.Fatalf("Inject must not modify unused documents: %q", got)
	}

	using := `<!DOCTYPE html><html><body><song-badge>Hi</song-badge></body></html>`
	if !r.UsedIn(using) {
		t.Fatal("document using a tag must be detected")
	}
	out := r.Inject(using)
	if !strings.Contains(out, `data-song-component="song-badge"`) {
		t.Fatalf("inject missing definitions: %q", out)
	}
	if !strings.Contains(out, RuntimePath) {
		t.Fatalf("inject missing runtime script: %q", out)
	}
	// Injected before </body>.
	if strings.Index(out, `data-song-component`) > strings.Index(out, "</body>") {
		t.Fatalf("definitions must be inserted before </body>: %q", out)
	}
	// Idempotent.
	if again := r.Inject(out); again != out {
		t.Fatalf("Inject must be idempotent")
	}
}

func TestInjectAppendsWithoutBody(t *testing.T) {
	r := DefaultRegistry()
	frag := `<song-badge>x</song-badge>`
	out := r.Inject(frag)
	if !strings.HasPrefix(out, frag) {
		t.Fatalf("fragment must be preserved: %q", out)
	}
	if !strings.Contains(out, `data-song-component="song-badge"`) {
		t.Fatalf("append injection failed: %q", out)
	}
}

func TestRuntimeDefinesCustomElements(t *testing.T) {
	js := Runtime()
	if !strings.Contains(js, "customElements.define") {
		t.Fatal("runtime must define custom elements")
	}
	if !strings.Contains(js, "attachShadow") {
		t.Fatal("runtime must use shadow DOM")
	}
	if !strings.Contains(js, "data-song-component") {
		t.Fatal("runtime must consume declarative template tags")
	}
}
