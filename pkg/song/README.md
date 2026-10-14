# song — static hosting with template processing and Web Components

## Overview

`song` is the top-level HTTP surface of the `azzurrotech/song` module. It wires
two packages together:

- `static.Store` — siloed filesystem storage.
- `tmpl.Engine` — per-silo `html/template` / `text/template` server-side
  rendering.

plus the `component.Registry` that defines the system's Web Components.

It runs in two modes:

- **Server mode** — `Song.Handler()` owns every route; unknown paths return 404.
- **Middleware mode** — `Song.Middleware(next)` serves only the routes song owns
  and delegates everything else to `next`, so it drops into an existing Go HTTP
  server without fighting it.

Built entirely on the Go standard library.

## Public API

```go
const Version = "2.0.0"
```

```go
type Config struct {
    Root       string           // store root directory (default "./data")
    Funcs      template.FuncMap // optional extra server-side template functions
    Components []component.Tag  // extra system-defined Web Components
}
```

```go
type Song struct { /* store, engine, components */ }
```

```go
func New(cfg Config) (*Song, error)
```
Builds the store, the template engine and the Web Component registry from the
configuration. Every registered component tag is added to the engine, so it is
usable as `{{template "tag" .}}` in any silo's HTML templates.

```go
func (s *Song) Store() *static.Store
func (s *Song) Components() *component.Registry
```
Accessors for the wired dependencies.

```go
func (s *Song) Handler() http.Handler
```
Complete server-mode handler: `s.Middleware(http.NotFoundHandler())`.

```go
func (s *Song) Middleware(next http.Handler) http.Handler
```
Serves the Web Component runtime (`/_song/...`) and any existing silo path; all
other paths go to `next`.

## Routes

| Path | Handler |
|---|---|
| `/_song/components.js` | baseline Web Components runtime (GET, HEAD) |
| `/_song/components` | registered tag definitions as JSON (GET, HEAD) |
| `/{silo}/<path>` | serve a static resource (GET, HEAD) |
| `/{silo}/<path>` | upsert a static resource (POST) |

`splitSiloPath` treats the first path segment as a silo unless it is empty or
reserved (`api`, `admin`, `health`, `song`, `.song`, `_song`), and only if the
store reports the silo exists; otherwise the request falls through to `next`.

## Serving a silo

`handleSilo`:

1. A `template_routes` entry in `.song/meta.json` matching the path is rendered
   server-side (HTML or text, see `pkg/tmpl`); the response carries
   `X-Song-Template: <name>`.
2. A directory request serves its `index.html` document when present.
3. HTML documents are processed as Go templates and gain the system-defined Web
   Component tags; when a document needs no processing it is streamed
   byte-for-byte, and processed documents carry `X-Song-Template-Processed: 1`.
4. Everything else is streamed with a content type from the extension table and
   cache headers. `http.ServeContent` handles conditional and Range requests.
5. A directory without an index document canonicalizes to a trailing slash
   (`301`) then returns `404`.

### Upserting a resource

`POST /{silo}/<path>` creates or replaces a resource whose content is the raw
request body — the README's upsertion surface. It answers `201 Created` for a
new resource and `200 OK` when an existing one was replaced, with a small JSON
body. The body is capped at `maxUploadBytes` (64 MiB) so a single request cannot
fill the disk.

## Configuration, defaults and limits

- **`maxUploadBytes = 64 << 20` (64 MiB)** caps the content accepted by a POST
  upsert.
- **`maxInlineHTML = 4 << 20` (4 MiB)** caps the size of an HTML document that
  is read into memory for template/component processing; larger documents are
  streamed verbatim.
- `Root` defaults to `"./data"`.
- Template-route data comes from the URL query via `valuesToData`/`inferScalar`:
  exact `true`/`false` become booleans, integers and floats are parsed, repeated
  keys become slices, and the `template` key is dropped.
- The `.song` and `.templates` trees are hidden; reserved segments are never
  treated as silos.

## Testing

```bash
go test ./pkg/song/
```

`song_test.go` is a black-box HTTP suite using `httptest`: middleware delegation
and reserved-path handling; static resource upsert (create/update, traversal
rejection, missing silo); directory `index.html` serving; HTML/Web Component
serving and injection; component runtime routes; server-side render via a
metadata template route; text-template rendering; server-side component tags and
host-registered components; render errors; and unknown paths.

## Design notes and invariants

- `Middleware` is the single routing authority; `Handler` is just it wrapped
  around `http.NotFoundHandler`.
- Reserved first segments and non-existent silos fall through, so mounting song
  under a larger mux never hijacks the host's `/api` or `/admin`.
- Silos are directories under the store root; they are created through the
  `static.Store` API (or by dropping a directory in place), not through HTTP.
