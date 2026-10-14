# tmpl — per-silo Go html/template rendering engine

## Overview

`tmpl` is the server-side-rendering engine of the `azzurrotech/song` module. Each
silo may hold a `.templates/` directory; every non-hidden regular file in it is
parsed into a **single** `html/template` set, so files compose with
`{{define}}`, `{{template}}` and `{{block}}`. The engine caches the parsed set
per silo and reloads it transparently when a template file's modification time
changes.

Templates are executed with `html/template`, which applies contextual
auto-escaping for HTML, attributes, JavaScript, CSS and URLs. The engine adds a
batteries-included `FuncMap` (see below) and accepts host-injected functions and
custom delimiters. `pkg/song` drives rendering from the metadata template routes
stored in each silo's `.song/meta.json`. Standard library only.

## Public API

```go
var ErrNoTemplates = errors.New("no templates found for silo")
```
Returned when a silo has no `.templates` directory or it contains no regular
files.

```go
type Engine struct { /* root, mu, custom, delims, cache */ }
```
Parses, caches and executes per-silo template sets. Safe for concurrent use
(guarded by a `sync.RWMutex`).

```go
func New(root string) *Engine
```
Creates an engine rooted at `root` (default `"."`). Templates live in
`<root>/<silo>/.templates`.

```go
func (e *Engine) SetDelims(left, right string)
```
Sets custom delimiters and drops the cache. Only applied when the pair is not
the zero value.

```go
func (e *Engine) AddFuncs(fm template.FuncMap)
```
Merges host functions into the engine; custom functions **override** built-ins
of the same name and drop the cache.

```go
func (e *Engine) SetFuncs(fm template.FuncMap)
```
Replaces the host-supplied function map wholesale (nil becomes empty) and drops
the cache.

```go
func (e *Engine) TemplateNames(silo string) ([]string, error)
```
Returns every parsable name (file base names plus `{{define}}` blocks) in sorted
order, excluding the engine's root sentinel.

```go
func (e *Engine) Render(w io.Writer, silo, name string, data any) error
```
Executes the named template with `data`. An empty `name` defaults to `"index"`.
Returns an error when the template set is missing (`ErrNoTemplates`) or the name
is undefined.

## Built-in template functions

`defaultFuncs` provides (names in backticks):

- **Typed strings** (rendered without contextual escaping): `html`, `htmlAttr`,
  `js`, `jsStr`, `css`, `url`, `srcset`.
- **JSON:** `json` (returns `template.JS`, safe inside `<script>`), `toJSON`
  (string), `prettyJSON`, `fromJSON`.
- **Strings:** `upper`, `lower`, `title`, `trim`, `trimSpace`, `trimPrefix`,
  `trimSuffix`, `trimAll`, `trimLeft`, `trimRight`, `replace`, `replaceAll`,
  `repeat`, `contains`, `hasPrefix`, `hasSuffix`, `split`, `splitN`, `fields`,
  `join`, `cat`, `abbrev`, `slug`.
- **Defaults/logic:** `default`, `coalesce`, `empty`, `ternary`.
- **Data structures:** `dict`, `list`, `keys`, `get`, `hasKey`.
- **Math:** `add`, `sub`, `mul`, `div`, `mod`, `seq`.
- **Casting/formatting:** `atoi`, `itoa`, `str`, `fmt`.
- **Time:** `now`, `date`, `rfc3339`, `unixTime`, `addTime`.
- **Encoding:** `b64enc`, `b64dec`, `urlencode`, `urlpath`.
- **Regexp:** `regexMatch`, `regexReplace`, `regexQuote`.
- **Hashing/misc:** `sha256`, `fileSize`.

These complement the `html/template` built-ins (`and`, `or`, `not`, `eq`, `ne`,
`lt`, `le`, `gt`, `ge`, `len`, `index`, `slice`, `printf`, `js`, `html`,
`urlquery`, `call`, …). `join` accepts `{{join .Items ","}}`,
`{{join "," .Items}}`, `{{join .Items}}` or variadic
`{{join "a" "b" "c"}}`.

## Usage

```go
eng := tmpl.New(srv.Store().Root())
eng.SetDelims("[[", "]]")
eng.AddFuncs(template.FuncMap{
    "shout": func(s string) string { return strings.ToUpper(s) + "!" },
})

var buf bytes.Buffer
if err := eng.Render(&buf, "demo", "page", map[string]any{"Title": "Hi"}); err != nil {
    log.Fatal(err)
}
```

A silo's `.templates/layout.tmpl` + `.templates/page.tmpl` compose as usual:

```
{{define "layout"}}<title>{{block "title" .}}default{{end}}</title>
<body>{{template "content" .}}</body>{{end}}
{{define "page"}}{{template "layout" .}}{{end}}
{{define "content"}}<h1>{{.Title}}</h1>{{end}}
```

## Configuration, defaults and limits

- **Root:** default `"."`; `pkg/song` passes the store root.
- **Templates directory:** `<root>/<silo>/.templates`.
- **File selection:** regular, non-dot-prefixed files only; subdirectories and
  hidden files are skipped. Files are sorted by full path before parsing.
- **Root template name:** the set's root is named after the silo; it is excluded
  from `TemplateNames`.
- **Default render name:** `"index"`.
- **Caching:** one cached `templateSet` per silo, keyed by filename→mod-time;
  any change (including a new file's presence) triggers a full re-parse.
- **Custom functions:** override built-ins of the same name.
- **Delimiters:** unset by default (`{{`/`}}`); `SetDelims` drops the cache.

## Testing

```bash
go test ./pkg/tmpl/
```

`engine_test.go` covers multi-file composition (layout + blocks + ranges) and
escaped output, typed strings not escaped while plain strings are escaped in
HTML/attribute/JS contexts, `json` breaking out of `</script>` safely, the
default function set, `dict`/`keys`/`list`, custom-function override, transparent
reload on file change, missing template and missing silo errors, custom
delimiters, whitespace trimming with comments, and the presence of expected
function names.

## Design notes and invariants

- All files in a silo's `.templates/` share **one** parse set, so a `{{define}}`
  block name is global within the silo: only one document may override a given
  block such as `title` or `content`.
- The cache is compared by modification time only; content changes that preserve
  mtime are not detected.
- `Render` looks up the template before executing so an unknown name fails with
  a clear error rather than a partial write.
- The mutex protects `custom`, `delims` and `cache`; parsing happens outside the
  lock, so two concurrent first-renders may both parse (last write wins).
- `html/template` contextual escaping is on by default; only the explicit typed
  helpers (`html`, `js`, `css`, `url`, …) bypass it.
