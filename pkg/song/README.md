# song — static hosting, server-side rendering and encrypted files

## Overview

`song` is the top-level HTTP surface of the `azzurrotech/song` module. It ties
three packages together:

- `static.Store` — siloed filesystem storage, optional AES-256-GCM encryption
  at rest, index routing rules.
- `auth.AuthService` — magic-link generate/validate/revoke.
- `tmpl.Engine` — per-silo `html/template` server-side rendering.

It runs in two modes:

- **Server mode** — `Song.Handler()` owns every route; unknown paths return 404.
- **Middleware mode** — `Song.Middleware(next)` serves only the routes song owns
  and delegates everything else to `next`.

The package also ships an embedded `/admin` UI (via `//go:embed ui`) and
`SeedDemo`, which creates a demo silo exercising static files, header-based
index routing, template routes and encrypted files.

Built entirely on the Go standard library.

## Public API

```go
const Version = "2.0.0"
```

```go
type Config struct {
    Root       string           // store root directory (default "./data")
    Secret     string           // encryption secret, at least 32 characters
    Funcs      template.FuncMap // optional extra server-side template functions
    PublicBase string           // public base URL for silos; empty => relative /c/{silo}/
    DisableUI  bool             // hides the /admin management interface
}
```

```go
type Song struct { /* cfg, store, auth, engine */ }
```

```go
func New(cfg Config) (*Song, error)
```
Builds the store, auth service and template engine. Returns an error for an
invalid root or a secret < 32 characters.

```go
func (s *Song) Store() *static.Store
func (s *Song) Auth() *auth.AuthService
func (s *Song) Templates() *tmpl.Engine
func (s *Song) Config() Config
```
Accessors for the wired dependencies and the configuration.

```go
func (s *Song) AddFuncs(fm template.FuncMap)
```
Injects extra template functions into the engine.

```go
func (s *Song) Handler() http.Handler
```
Complete server-mode handler: `s.Middleware(http.NotFoundHandler())`.

```go
func (s *Song) Middleware(next http.Handler) http.Handler
```
Serves `/health`, `/api/song/*`, `/api/auth/*`, `/admin` (unless `DisableUI`),
and any existing silo path; all other paths go to `next`.

```go
func SeedDemo(s *Song) error
```
Creates/populates the `demo` silo idempotently and saves its metadata. Safe to
call repeatedly.

## Routes

| Path | Handler |
|---|---|
| `/health` | JSON health/status (GET, HEAD) |
| `/api/song/silos...` | file/silo CRUD API |
| `/api/auth/*` | magic-link endpoints |
| `/admin`, `/admin/` | embedded management UI (`DisableUI` hides it) |
| `/{silo}/...` | serve a silo (GET, HEAD) |

`splitSiloPath` treats the first path segment as a silo unless it is empty or
reserved (`api`, `admin`, `health`, `song`, `.song`), and only if the store
reports the silo exists; otherwise the request falls through to `next`.

## File and silo API

Base path `/api/song/silos`. Silos:

| Method | Path | Body | Result |
|---|---|---|---|
| GET | `/api/song/silos` | — | `{"silos":[{"name","files"}…]}` |
| POST | `/api/song/silos` | `name` (JSON or form) | `201`, `409` if exists |
| DELETE | `/api/song/silos/{silo}` | — | `200` or `404` |
| GET/PUT | `/api/song/silos/{silo}/base-url` | GET validates; PUT `{"base_url"}` | base URL |

Files and rendering:

| Method | Path | Body / query | Result |
|---|---|---|---|
| GET | `/api/song/silos/{silo}/files` | `?path=` | directory listing |
| GET | `/api/song/silos/{silo}/file` | `?path=&encoding=base64` | file + content |
| POST/PUT/DELETE | `/api/song/silos/{silo}/file` | `path`, `content`, `encrypt`, `overwrite` | CRUD |
| POST | `/api/song/silos/{silo}/upload` | multipart `file` parts + `dir`, `encrypt` | per-file results |
| GET/PUT | `/api/song/silos/{silo}/meta` | full `static.Meta` JSON | metadata |
| GET | `/api/song/silos/{silo}/templates` | — | template names + files |
| GET/POST | `/api/song/silos/{silo}/render` | `template` + data (JSON/form/query) | HTML or JSON |

`decodeFileOp` accepts every body an HTML form can produce: `application/json`,
`application/x-www-form-urlencoded`, `multipart/form-data`, and any other
content type, where the **raw body is the content**. For raw bodies the path
comes from `?path=` or the `X-Song-Path` header, and `encrypt` from `?encrypt=`
or `X-Song-Encrypt`. `parseBool` accepts `true/1/on/yes` and
`false/0/off/no`.

`/render` data is type-inferred by `valuesToData`/`inferScalar`: exact
`true`/`false` become booleans, integers and floats are parsed, repeated keys
become slices, and the `template` key is dropped. Add `?format=json` to get
`{"html":…,"template":…}` instead of an HTML document.

## Serving a silo

`handleSilo` (GET/HEAD only):

1. A `template_routes` entry in `.song/meta.json` matching the path is rendered
   server-side.
2. A directory request resolves its index document via `Store.ResolveIndex`
   using client headers; a rule with `redirect: true` issues a `302`.
3. Files are streamed with a content type from the extension table and cache
   headers; encrypted files are decrypted and served with
   `X-Song-Encrypted: 1`.
4. `http.ServeContent` handles conditional and Range requests.
5. A directory without an index canonicalizes to a trailing slash (`301`) then
   returns `404`.

## Configuration, defaults and limits

- **`maxUploadBytes = 64 << 20` (64 MiB)** caps multipart file parts read by
  `contentFromParts` and the stream passed to `Store.Upload` via `io.LimitReader`.
- **`ParseMultipartForm(16 << 20)`** (16 MiB) is the multipart in-memory
  threshold; larger parts spill to temp files.
- JSON/form `content` strings and raw request bodies are read by the body
  parser/`io.ReadAll` without the 64 MiB cap.
- `Secret` must be ≥ 32 characters (also enforced by `static`/`auth`).
- `Root` defaults to `"./data"`; `PublicBase` empty yields `/c/{silo}/` URLs.
- Auth endpoints use the default 24-hour magic-link TTL.
- The `.song` and `.templates` trees are hidden; reserved segments are never
  treated as silos.

## Testing

From `/home/matthew/Projects/Platform/stenella/atp/song`:

```bash
go test ./pkg/song/
```

`song_test.go` is a black-box HTTP suite using `httptest`: health; silo lifecycle
(create/list/delete, duplicate `409`, reserved `400`); file CRUD via JSON, form
and raw bodies; encrypted file round trip with a no-plaintext-on-disk assertion
and `X-Song-Encrypted` header; multipart upload; header-based index selection and
redirect; server-side render via JSON/form/GET plus a metadata template route;
middleware delegation and reserved-path handling; admin UI and `DisableUI`;
magic-link generate/validate; API path-traversal rejection; `SeedDemo`
(idempotent); render errors as `404`; and unknown endpoints.

## Design notes and invariants

- `Middleware` is the single routing authority; `Handler` is just it wrapped
  around `http.NotFoundHandler`.
- Reserved first segments and non-existent silos fall through, so mounting song
  under a larger mux never hijacks the host's `/api` or `/admin` unless song
  owns them.
- `handleFileAPI` validates `HasSilo` before dispatch; unknown actions return
  `404`.
- Create-vs-update: `POST /file` returns `409` on an existing path unless
  `overwrite=true`, in which case it updates; `PUT /file` requires the file to
  exist.
- Errors are JSON `{"error": …}`; method mismatches return `405` with an
  `Allow` header.
- The embedded UI is itself a static web app hosted by `handleUI`.
