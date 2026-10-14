// Package component implements song's Web Components support using only the Go
// standard library and baseline browser HTML, CSS and JavaScript.
//
// MDN defines Web Components around three primitives: custom elements, shadow
// DOM and HTML <template>/<slot> elements. song supports all three:
//
//   - A system-defined "tag" is a custom element (its name must contain a
//     hyphen, e.g. song-badge) whose markup and styles live inside a
//     <template> element rendered into the page.
//   - The baseline runtime (see Runtime) is a tiny dependency-free ES module
//     that defines every <template data-song-component="…"> it finds as a
//     custom element and clones the template into the element's shadow root,
//     so <slot> projection works exactly as described by MDN.
//   - Because Inject adds the definitions and the runtime to any served HTML
//     document that uses a registered tag, the tags are "accessible from any
//     provided HTML file".
//
// The same tags are also compiled into every server-side HTML template set by
// pkg/tmpl (via Tag.ServerTemplate), so {{template "song-badge" .}} works in
// server-rendered pages too.
package component

import (
	_ "embed"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// RuntimePath is the URL at which song serves the baseline Web Components
// runtime. It is injected into HTML documents that use a registered tag.
const RuntimePath = "/_song/components.js"

// RuntimePathPrefix is the URL path prefix owned by the component surfaces
// (/_song/components.js and /_song/components). The HTTP layer routes this
// prefix to the component handler before any silo lookup.
const RuntimePathPrefix = "/_song/"

// DefinitionsPath is the URL at which song exposes the registered tag
// definitions as JSON (useful for tooling and for hosts embedding the runtime).
const DefinitionsPath = "/_song/components"

// runtimeJS is the baseline custom-element runtime, embedded at build time so
// the server stays a single self-contained binary with no external assets.
//
//go:embed runtime.js
var runtimeJS string

// Runtime returns the JavaScript source of the baseline Web Components runtime.
func Runtime() string { return runtimeJS }

// tagNameRe matches valid custom element names: lower-case ASCII letters and
// digits in hyphen-separated words, as required by the HTML specification for
// custom element names (the name must contain at least one hyphen).
var tagNameRe = regexp.MustCompile(`^[a-z][a-z0-9]*(?:-[a-z0-9]+)+$`)

// Tag is a system-defined HTML template tag (a Web Component).
//
//	Name   custom element name; must contain a hyphen, e.g. "song-badge"
//	Markup the component's markup, placed inside a <template> for the
//	       client runtime and inside {{define "Name"}} for server rendering.
//	       May contain <slot> elements for light-DOM projection.
//	Styles CSS placed inside the component's shadow root (client) and in an
//	       inline <style> when rendered server-side.
type Tag struct {
	Name   string `json:"name"`
	Markup string `json:"markup,omitempty"`
	Styles string `json:"styles,omitempty"`
}

// ServerTemplate returns the Go html/template definition for the tag. It is
// passed to tmpl.Engine.AddComponent so {{template "Name" .}} resolves in every
// HTML template set.
func (t Tag) ServerTemplate() string {
	var b strings.Builder
	b.WriteString(`{{define "` + t.Name + `"}}`)
	if t.Styles != "" {
		b.WriteString("<style>" + t.Styles + "</style>")
	}
	b.WriteString(t.Markup)
	b.WriteString("{{end}}")
	return b.String()
}

// Registry holds the system-defined tags in registration order. It is safe for
// concurrent use.
type Registry struct {
	mu    sync.RWMutex
	tags  map[string]Tag
	order []string
}

// NewRegistry returns an empty Registry.
func NewRegistry() *Registry {
	return &Registry{tags: map[string]Tag{}}
}

// Register validates and stores a tag. Registering an existing name replaces
// the previous definition (keeping its original position). Invalid names are
// rejected so a bad definition cannot break every served HTML document.
func (r *Registry) Register(t Tag) error {
	if !tagNameRe.MatchString(t.Name) {
		return fmt.Errorf("component: %q is not a valid custom element name (lower-case, hyphen-separated)", t.Name)
	}
	if strings.TrimSpace(t.Markup) == "" {
		return fmt.Errorf("component: tag %q has no markup", t.Name)
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.tags[t.Name]; !ok {
		r.order = append(r.order, t.Name)
	}
	r.tags[t.Name] = t
	return nil
}

// Tags returns every registered tag in registration order.
func (r *Registry) Tags() []Tag {
	r.mu.RLock()
	defer r.mu.RUnlock()
	out := make([]Tag, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tags[name])
	}
	return out
}

// Names returns the registered tag names in registration order.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return append([]string(nil), r.order...)
}

// Get returns the tag with the given name.
func (r *Registry) Get(name string) (Tag, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	t, ok := r.tags[name]
	return t, ok
}

// Len returns the number of registered tags.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.order)
}

// Definitions renders every registered tag as a declarative HTML <template>
// element. Placed in a document, the baseline runtime turns each one into a
// custom element backed by a shadow root.
func (r *Registry) Definitions() string {
	tags := r.Tags()
	var b strings.Builder
	for _, t := range tags {
		b.WriteString(`<template data-song-component="`)
		b.WriteString(t.Name)
		b.WriteString(`">`)
		if t.Styles != "" {
			b.WriteString("<style>")
			b.WriteString(t.Styles)
			b.WriteString("</style>")
		}
		b.WriteString(t.Markup)
		b.WriteString("</template>")
	}
	return b.String()
}

// used reports whether doc already references any registered custom element.
func (r *Registry) used(doc string) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	for name := range r.tags {
		if strings.Contains(doc, "<"+name) {
			return true
		}
	}
	return false
}

// UsedIn reports whether doc instantiates at least one registered tag.
func (r *Registry) UsedIn(doc string) bool { return r.used(doc) }

// Inject returns doc with the tag definitions and the runtime <script> added
// when — and only when — doc actually uses a registered tag. Injection is
// idempotent and never modifies documents that do not use tags, so plain
// static HTML is served byte-for-byte. The block is inserted before the
// closing </body> (or </html>) element when present, otherwise appended.
func (r *Registry) Inject(doc string) string {
	if r.Len() == 0 || strings.Contains(doc, "data-song-component") || !r.used(doc) {
		return doc
	}
	block := r.Definitions() + `<script type="module" src="` + RuntimePath + `"></script>`
	lower := strings.ToLower(doc)
	if i := strings.LastIndex(lower, "</body>"); i >= 0 {
		return doc[:i] + block + doc[i:]
	}
	if i := strings.LastIndex(lower, "</html>"); i >= 0 {
		return doc[:i] + block + doc[i:]
	}
	return doc + block
}

// DefaultTags returns the built-in tags that ship with song. They demonstrate
// slot projection (song-card) and style encapsulation (song-badge).
func DefaultTags() []Tag {
	return []Tag{
		{
			Name: "song-badge",
			Styles: ":host{display:inline-flex;align-items:center;gap:.25rem;" +
				"padding:.1rem .5rem;border-radius:999px;background:#ece7f6;" +
				"color:#4a148c;font:500 .8rem/1.4 system-ui,sans-serif}",
			Markup: "<slot></slot>",
		},
		{
			Name: "song-card",
			Styles: ":host{display:block;border:1px solid #e2e2ea;border-radius:.6rem;" +
				"background:#fff;box-shadow:0 1px 3px rgba(0,0,0,.08);overflow:hidden}" +
				"header{padding:.6rem .9rem;border-bottom:1px solid #eee;font-weight:600}" +
				"section{padding:.9rem}",
			Markup: "<header><slot name=\"title\"></slot></header><section><slot></slot></section>",
		},
	}
}

// DefaultRegistry returns a Registry preloaded with DefaultTags. Errors cannot
// occur for the built-in tags and are ignored.
func DefaultRegistry() *Registry {
	r := NewRegistry()
	for _, t := range DefaultTags() {
		_ = r.Register(t)
	}
	return r
}

// ErrNotFound is returned by Registry.Get-style lookups in host code.
var ErrNotFound = errors.New("component: tag not found")
