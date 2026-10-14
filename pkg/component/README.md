# component — system-defined Web Components

## Overview

`component` implements song's Web Component support with the Go standard
library plus baseline browser HTML, CSS and JavaScript. It follows
[MDN Web Components](https://developer.mozilla.org/en-US/docs/Web/API/Web_components):
custom elements, shadow DOM, and the `<template>`/`<slot>` elements.

A **tag** is a system-defined HTML template tag: a custom element name (it must
contain a hyphen, e.g. `song-badge`) together with markup and styles. A tag is
supported in two places at once:

- **Server side** — `Tag.ServerTemplate()` returns a Go
  `html/template` definition, registered with `tmpl.Engine.AddComponent`, so
  `{{template "song-badge" .}}` resolves in *every* silo's HTML template. This
  is what makes the tags "accessible from any provided HTML file".
- **Client side** — `Registry.Definitions()` renders each tag as a declarative
  `<template data-song-component="…">`, and `Registry.Inject(doc)` adds those
  definitions plus the baseline runtime to any served HTML document that
  instantiates a tag. The runtime defines the custom element and clones the
  template into an open shadow root, so `<slot>` projection works as MDN
  describes.

`Song` wires all of this automatically. Because injection only happens when a
document actually uses a registered tag, ordinary static HTML is served
byte-for-byte.

## Public API

```go
const (
    RuntimePath       = "/_song/components.js"
    RuntimePathPrefix = "/_song/"
    DefinitionsPath   = "/_song/components"
)
```

```go
type Tag struct {
    Name   string // custom element name, e.g. "song-badge"
    Markup string // markup placed in the shadow root (may use <slot>)
    Styles string // CSS placed in the shadow root
}

func (t Tag) ServerTemplate() string // {{define "Name"}}…{{end}}
```

```go
type Registry struct { /* tags, order, mutex */ }

func NewRegistry() *Registry
func (r *Registry) Register(t Tag) error // validates and replaces by name
func (r *Registry) Tags() []Tag
func (r *Registry) Names() []string
func (r *Registry) Get(name string) (Tag, bool)
func (r *Registry) Len() int
func (r *Registry) Definitions() string       // declarative <template> blocks
func (r *Registry) UsedIn(doc string) bool    // does doc instantiate a tag?
func (r *Registry) Inject(doc string) string  // add definitions + runtime
```

```go
func Runtime() string      // baseline ES module source
func DefaultTags() []Tag   // built-ins: song-badge, song-card
func DefaultRegistry() *Registry
```

`Names`/`Register` require a valid custom-element name: lower-case ASCII,
hyphen-separated, at least one hyphen. Invalid names and empty markup are
rejected at registration so a bad definition can never break served pages.

## HTTP surfaces (served by pkg/song)

| Path | Method | Result |
|---|---|---|
| `/_song/components.js` | GET/HEAD | the baseline custom-element runtime (ES module) |
| `/_song/components` | GET/HEAD | JSON list of registered tags |

## Usage

```go
srv, _ := song.New(song.Config{
    Root: "./data",
    Components: []component.Tag{
        {Name: "host-hello", Markup: `<b>hello</b> <slot></slot>`},
    },
})
```

Client side, any served `.html` file can now use the tag:

```html
<host-hello>world</host-hello>
```

song detects the tag, injects the `<template data-song-component="host-hello">`
definition and the runtime script, and the browser upgrades the element with an
open shadow root.

Server side, any HTML template can instantiate it:

```html
{{define "page"}}<main>{{template "host-hello" .name}}</main>{{end}}
```

## Testing

```bash
go test ./pkg/component/
```

`component_test.go` covers: the built-in registry, name/markup validation and
replacement, `ServerTemplate` and `Definitions` shape, `UsedIn` detection,
idempotent `Inject` (including append when there is no `</body>`), and the
runtime's use of `customElements.define` / `attachShadow`.

End-to-end coverage lives in `pkg/song/song_test.go`:
`TestHTMLComponentServing`, `TestComponentRuntimeRoutes`,
`TestServerSideComponentTag` and `TestConfigComponents`.

## Design notes

- Standard library only; no client framework, CDN or build step.
- The runtime is embedded with `//go:embed`, so the server stays a single
  self-contained binary.
- `Inject` is idempotent and a no-op for documents that do not use a tag.
- Server-rendered tags include their styles in an inline `<style>`; the client
  runtime scopes the same styles inside the shadow root.
