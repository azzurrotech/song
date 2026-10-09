# static — siloed static-file store with encryption at rest

## Overview

`static` is the storage engine of the `azzurrotech/song` module. It manages a
root directory containing one subdirectory per **silo**:

```
<root>/<silo>/{index.html, assets/…, secrets/notes.txt (ciphertext on disk),
  .templates/…, .song/meta.json}
```

Files may be stored encrypted at rest using the shared
`internal/encryption.EncryptionManager`; the per-silo `.song/meta.json` records
which paths are encrypted and carries index-routing rules and template routes.
`pkg/song` uses this package for all file CRUD and serving. Standard library
only.

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

`ReservedSiloNames` maps `"api"`, `"admin"`, `"health"`, `"song"`, `".song"` to
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
    Encrypted   bool      `json:"encrypted,omitempty"`
    Modified    time.Time `json:"modified"`
}

type FileEntry struct {
    FileInfo
    Content []byte `json:"-"`
}

type ReaderEntry struct {
    Reader    io.ReadSeekCloser
    Size      int64
    Encrypted bool
    Name      string
    ModTime   time.Time
}
```
`FileEntry` adds the (decrypted) content; `ReaderEntry` is a seekable handle
prepared for `http.ServeContent` (encrypted files are decrypted into memory).

`Store` (fields `root`, `em`, `publicBase`) is the siloed filesystem tree.

### Metadata types

```go
type IndexRule struct {
    Header   string `json:"header"`
    Value    string `json:"value,omitempty"`
    Pattern  string `json:"pattern,omitempty"`
    File     string `json:"file"`
    Redirect bool   `json:"redirect,omitempty"`
}

type Meta struct {
    Version        int               `json:"version"`
    IndexRules     []IndexRule       `json:"index_rules,omitempty"`
    TemplateRoutes map[string]string `json:"template_routes,omitempty"`
    Encrypted      map[string]bool   `json:"encrypted,omitempty"`
    BaseURL        string            `json:"base_url,omitempty"`
}
```

### Store methods

```go
func NewStore(root, secret, publicBase string) (*Store, error)
```
Validates the secret, resolves and creates `root` (default `"./data"`). An empty
`publicBase` selects relative base URLs. Related accessors: `Root() string` and
`Encryption() *encryption.EncryptionManager`.

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
Read paths. `List` hides dot-prefixed entries and sorts directories first.
`List` errors with `ErrIsFile` on a file; `Stat`/`Read`/`Open` error with
`ErrIsDir` on a directory. `Read`/`Open` transparently decrypt flagged files.

```go
func (s *Store) Create(silo, rel, content string, encrypt bool) (*FileInfo, error)
func (s *Store) Update(silo, rel, content string, encrypt *bool) (*FileInfo, error)
func (s *Store) Upload(silo, rel string, r io.Reader, encrypt bool) (*FileInfo, error)
func (s *Store) Delete(silo, rel string) error
```
Write paths. `Create` fails with `ErrExists`. `Update` keeps the current
encryption state when `encrypt == nil`, otherwise re-stores accordingly. `Delete`
also removes the encryption flag from meta.

```go
func (s *Store) Meta(silo string) (*Meta, error)
func (s *Store) SaveMeta(silo string, m *Meta) error
func (s *Store) SetTemplateRoute(silo, route, tpl string) error
func (s *Store) TemplateDir(silo string) (string, error)
```
Metadata access. `Meta` returns initialized defaults when `.song/meta.json` is
missing. `SetTemplateRoute` deletes the route when `tpl == ""`.

```go
func (s *Store) DefaultBaseURL(silo string) string
func (s *Store) ValidateBaseURL(silo string) error
func (s *Store) SetBaseURL(silo, baseURL string) error
```
Base-URL handling. `DefaultBaseURL` returns `<publicBase>/c/<silo>/` or
`/c/<silo>/` when no public base is set. `SetBaseURL` requires an `http://`,
`https://` or `/`-prefixed value.

```go
func (s *Store) ResolveIndex(silo, dir string, hdr http.Header) (file string, redirect bool, err error)
```
Picks the index document for a directory request. It evaluates
`Meta.IndexRules` in order; a rule matches when its `Header` is present, `Value`
is non-empty and equals the header, or `Pattern` is a valid regexp matching it,
**and** the target file exists. The first match wins and its `Redirect` flag is
returned. Failing all rules, a built-in convenience maps `X-Index-Variant: <v>`
→ `index.<v>.html` (rejecting `/`, `\` and a leading dot in `<v>`), then
`index.html`; otherwise `ErrNotFound`.

## Configuration, defaults and limits

- **Root:** default `"./data"`; created with mode `0755`.
- **Secret:** must be ≥ 32 characters; passed to `NewEncryptionManager`.
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

From `/home/matthew/Projects/Platform/stenella/atp/song`:

```bash
go test ./pkg/static/
```

`store_test.go` covers silo-name validation (bad/good names, duplicates), root
creation and short-secret rejection, file CRUD and error sentinels, encrypted
create/update/serve with a no-plaintext-on-disk assertion, path-traversal
rejection, hidden/metadata exclusion in listings, `ErrIsFile` on listing a file,
header index resolution (exact, regexp, redirect, built-in variant,
subdirectories, missing index), template-route meta, silo removal, sorted silo
listing, and default meta initialization.

## Design notes and invariants

- Every path that reaches the filesystem goes through `filePath` →
  `siloDir`/`cleanRel`/`resolve`, so traversal and `.song` access are rejected
  before any I/O.
- The encryption flag lives in `.song/meta.json` and is updated on every write
  and delete, so served files are decrypted exactly when intended.
- `writeFile` buffers the whole payload in memory when encrypting; plain writes
  stream via `io.Copy`.
- Writes are **not** atomic (`os.WriteFile`/`os.Create` target the final path);
  `updateMeta` reloads, mutates and rewrites the metadata file. Concurrency is
  single-writer; there is no file locking.
