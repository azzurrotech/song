# static — siloed static-file store

## Overview

`static` is the storage engine of the `azzurrotech/song` module. It manages a
root directory containing one subdirectory per **silo**:

```
<root>/<silo>/{index.html, assets/…, .templates/…, .song/meta.json}
```

Files are stored as plain bytes. A hidden per-silo `.song/meta.json` records the
server-side **template routes** that map a silo-relative request path onto a
named Go template (see `pkg/tmpl`). `pkg/song` uses this package for all file
access and serving. Standard library only.

## Public API

### Sentinel errors

```go
var (
    ErrInvalidName = errors.New("invalid name")
    ErrReserved    = errors.New("name is reserved")
    ErrTraversal   = errors.New("path escapes the store root")
    ErrNotFound    = errors.New("not found")
    ErrExists      = errors.New("already exists")
    ErrIsDir       = errors.New("is a directory")
    ErrIsFile      = errors.New("is a file")
)
```

`ReservedSiloNames` maps the segments song/hosts conventionally own (`api`,
`admin`, `health`, `song`) and song's private ones (`.song`, `_song`) to
`true`. `IsReservedSilo(name string) bool` reports membership.

```go
func ParseRel(rel string) (string, error)
```
Validates and normalizes a silo-relative path from the HTTP layer; the result is
safe to pass to store methods.

### Data types

```go
type FileInfo struct {
    Name        string    `json:"name"`
    Path        string    `json:"path"`
    Size        int64     `json:"size"`
    ContentType string    `json:"content_type"`
    IsDir       bool      `json:"is_dir"`
    Modified    time.Time `json:"modified"`
}

type FileEntry struct {
    FileInfo
    Content []byte `json:"-"`
}

type ReaderEntry struct {
    Reader  io.ReadSeekCloser
    Size    int64
    Name    string
    ModTime time.Time
}
```

`FileEntry` adds the file content; `ReaderEntry` is a seekable handle prepared
for `http.ServeContent` (so conditional and Range requests work). `Store` owns
the silo tree (a single `root` path).

### Metadata types

```go
type Meta struct {
    Version        int               `json:"version"`
    TemplateRoutes map[string]string `json:"template_routes,omitempty"`
}
```

`TemplateRoutes` maps a silo-relative path (e.g. `about`) onto the name of a Go
template living in the silo's `.templates/` directory.

### Store methods

```go
func NewStore(root string) (*Store, error)
```
Resolves and creates `root` (default `"./data"`). `Root() string` returns the
absolute root.

```go
func (s *Store) HasSilo(name string) bool
func (s *Store) ListSilos() ([]string, error)
func (s *Store) CreateSilo(name string) error
func (s *Store) RemoveSilo(name string) error
```
Silo lifecycle. `CreateSilo` fails with `ErrExists`; `RemoveSilo` errors with
`ErrNotFound` when absent. `ListSilos` returns sorted, validated, non-hidden
directory names.

```go
func (s *Store) List(silo, rel string) ([]FileInfo, error)
func (s *Store) Stat(silo, rel string) (*FileInfo, error)
func (s *Store) Read(silo, rel string, withContent bool) (*FileEntry, error)
func (s *Store) Open(silo, rel string) (*ReaderEntry, error)
```
Read paths. `List` hides dot-prefixed entries (`.song`, `.templates`) and sorts
directories first; it errors with `ErrIsFile` on a file. `Stat`/`Read`/`Open`
error with `ErrIsDir` on a directory.

```go
func (s *Store) Create(silo, rel, content string) (*FileInfo, error)
func (s *Store) Update(silo, rel, content string) (*FileInfo, error)
func (s *Store) Delete(silo, rel string) error
```
Write paths. `Create` fails with `ErrExists`; `Update` fails with `ErrNotFound`
when the file is not stored yet. `writeFile` streams content and creates parent
directories as needed.

```go
func (s *Store) Meta(silo string) (*Meta, error)
func (s *Store) SetTemplateRoute(silo, route, tpl string) error
```
Metadata access. `Meta` returns initialized defaults when `.song/meta.json` is
missing. `SetTemplateRoute` registers a route, or deletes it when `tpl == ""`.

## Configuration, defaults and limits

- **Root:** default `"./data"`; created with mode `0755`.
- **Silo names:** `^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$` — must start
  and end with a letter or digit; reserved names rejected.
- **Path rules (`cleanRel`):** trim space; `/` and empty → `""`; strip one
  leading `/`; reject NUL and backslashes; `filepath.Clean`; reject `..`/`..<sep>`
  prefixes; strip a leading `./`; reject any raw `.song` segment or cleaned path
  equal to/under `.song`. `resolve` asserts the joined absolute path stays inside
  the silo directory (`ErrTraversal` otherwise).
- **Modes/listing/types:** files `0644`, directories `0755`; dot-prefixed names
  (`.song`, `.templates`) are skipped in listings; content types use a built-in
  extension table, then `mime.TypeByExtension`, then `application/octet-stream`.

## Testing

```bash
go test ./pkg/static/
```

`store_test.go` covers silo-name validation (bad/good names, duplicates), root
creation, file CRUD and error sentinels, plain-bytes-on-disk storage, path
traversal rejection, hidden/metadata exclusion in listings, `ErrIsFile` on
listing a file, template-route metadata, silo removal, sorted silo listing, and
default meta initialization.

## Design notes and invariants

- Every path that reaches the filesystem goes through `filePath` →
  `siloDir`/`cleanRel`/`resolve`, so traversal and `.song` access are rejected
  before any I/O.
- Writes are **not** atomic (`os.Create` targets the final path); `updateMeta`
  reloads, mutates and rewrites the metadata file. Concurrency is single-writer;
  there is no file locking.
