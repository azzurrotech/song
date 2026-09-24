# song

**Static-file hosting and management — with server-side rendering, header-based
index routing, and encrypted-at-rest files.**

song is a small web platform built entirely on the Go standard library. It stores
web applications (HTML/CSS/JS and any other bytes) in **per-tenant silos** on the
filesystem, serves them with full HTTP semantics, renders pages server-side with
`html/template`, and keeps sensitive files encrypted with AES-256-GCM at rest.
Magic-link authentication and an embedded management UI are included.

© Azzurro Technology Inc. — MIT License.

---

## Why song

| Need | What song gives you |
|---|---|
| Host a static web app | serve a silo at `/{silo}/...` with correct MIME types, gzip, caching |
| Many tenants/projects | one silo per project; silos are isolated directories |
| `index.html` by device | choose the index document from a client header (`User-Agent`, `X-Index-Variant`, …) |
| Sensitive files | store files as AES-256-GCM ciphertext, transparently decrypted on serve |
| Landing pages with data | full `html/template` server-side rendering with a batteries-included FuncMap |
| HTML-form uploads | the file API accepts JSON, urlencoded forms, multipart and raw bodies |
| Passwordless login | magic-link generate/validate/revoke endpoints |
| Management | embedded `/admin` UI plus a complete JSON API |

Everything is the native web platform and the Go stdlib: no frameworks, no
external modules, one small binary.

---

## Quick start

```bash
go build -o song-server .
./song-server --seed --secret "a-real-secret-key-at-least-32-characters"
```

Then:

- `http://localhost:8080/` — silo listing / admin (`/admin`)
- `http://localhost:8080/demo/` — the seeded demo web app
- `http://localhost:8080/demo/` **with header `X-Index-Variant: mobile`** — the
  mobile variant of the index document
- `http://localhost:8080/demo/about` — a server-side-rendered template route
- `http://localhost:8080/demo/secrets/notes.txt` — an encrypted-at-rest file,
  served decrypted with `X-Song-Encrypted: 1`
- `http://localhost:8080/health` — health check

## Configuration

Flags (environment variables in parentheses — flags win):

```
--port   <n>    HTTP port                       (SONG_PORT)   default 8080
--root   <dir>  silo store root                 (SONG_ROOT)   default ./data
--secret <key>  encryption secret, >= 32 chars  (SONG_SECRET)
--seed          create the demo silo            (SONG_SEED=false)
--version       print version and exit
--help          show usage
```

Docker:

```bash
docker build -t song .
docker run -p 8083:8083 -v song-data:/data song
```

The container listens on 8083 and stores silos under `/data`.

## Two integration modes

song is one `http.Handler` with two personalities:

1. **Server mode** — `Song.Handler()` owns all routes; anything it does not know
   returns 404.

   ```go
   http.ListenAndServe(":8080", srv.Handler())
   ```

2. **Middleware mode** — `Song.Middleware(next)` serves only what song owns
   (silos, `/api/song/*`, `/api/auth/*`, `/health`, `/admin`) and delegates every
   other path to `next`. Mount song inside your own Go server:

   ```go
   mux := http.NewServeMux()
   mux.Handle("/", songSvc.Middleware(myAppHandler))
   ```

   See [`examples/middleware`](examples/middleware) for a complete example.

## Serving a silo

Silk store layout:

```
<root>/
  <silo>/
    index.html            # default index document
    index.mobile.html     # selected when a rule matches
    app.js, style.css, …  # any files
    assets/…              # sub-directories
    secrets/notes.txt     # encrypted file: ciphertext on disk (nonce||ct)
    .templates/           # go html/template files for SSR
    .song/meta.json       # per-silo rules (index rules, template routes)
```

Request handling for `/{silo}/...`:

1. Resolve the silo name from the first path segment (reserved names `api`,
   `admin`, `health`, `song`, `.song` are never silos).
2. For a directory, select the index document: the first matching
   `index_rules` entry wins, then `index.html`, then `index.<variant>.html` for
   a built-in `X-Index-Variant: <v>` header.
3. Files marked encrypted are decrypted transparently and served with
   `X-Song-Encrypted: 1`.
4. Directories without an index return 404 (after a 301 that canonicalizes a
   missing trailing slash).

### Index rules in `.song/meta.json`

```json
{
  "version": 1,
  "index_rules": [
    {"header": "X-Index-Variant", "value": "mobile",  "file": "index.mobile.html"},
    {"header": "X-Index-Variant", "value": "desktop", "file": "index.desktop.html"},
    {"header": "User-Agent",      "pattern": "(?i)mobile|android|iphone", "file": "index.mobile.html", "redirect": true}
  ],
  "template_routes": {
    "page":  "page",
    "about": "about"
  },
  "encrypted": {
    "secrets/notes.txt": true
  }
}
```

## File management API

Base path: `/api/song/silos`. Accepts JSON (`application/json`), url-encoded
forms (`application/x-www-form-urlencoded`), multipart uploads
(`multipart/form-data`) and raw bodies (the body *is* the content, path comes
from `?path=`).

### Silos

| Method | Path | Body | Result |
|---|---|---|---|
| GET | `/api/song/silos` | — | `{"silos":[{"name","files"}…]}` |
| POST | `/api/song/silos` | `name` | `201`, or `409` if it exists |
| DELETE | `/api/song/silos/{silo}` | — | `200` or `404` |

### Files

| Method | Path | Body / query | Result |
|---|---|---|---|
| GET | `/api/song/silos/{silo}/files` | `?path=` (optional) | file listing |
| GET | `/api/song/silos/{silo}/file` | `?path=&content=1&encoding=base64` | file + content |
| POST | `/api/song/silos/{silo}/file` | path, content, encrypt, overwrite | `201` created / `409` exists |
| PUT | `/api/song/silos/{silo}/file` | path, content, encrypt | `200` updated / `404` |
| DELETE | `/api/song/silos/{silo}/file` | `?path=` | `200` / `404` |
| POST | `/api/song/silos/{silo}/upload` | multipart `file` parts + `dir`, `encrypt` | per-file results |

Form/JSON field names: `path`, `content`, `encrypt` (`true`/`false`,
`1`/`0`/`on`/`yes`…), `overwrite`, and `dir` for uploads.

```bash
# create a silo and an encrypted JS file from a plain HTML form
curl -X POST localhost:8080/api/song/silos -d 'name=shop'
curl -X POST localhost:8080/api/song/silos/shop/file \
  -d 'path=app.js&content=console.log("hi")&encrypt=true'

# JSON on the way in, JSON on the way out
curl -X POST localhost:8080/api/song/silos/shop/file \
  -H 'Content-Type: application/json' \
  -d '{"path":"index.html","content":"<h1>Shop</h1>"}'

# raw body
curl -X POST 'localhost:8080/api/song/silos/shop/file?path=data.txt' \
  -H 'Content-Type: text/plain' --data-binary 'raw bytes'

# multipart upload
curl -X POST localhost:8080/api/song/silos/shop/upload \
  -F 'dir=assets' -F 'file=@logo.svg'

# multi-file upload (multiple -F 'file=@…' parts are supported)
```

### Meta and templates

| Method | Path | Body | Result |
|---|---|---|---|
| GET / PUT | `/api/song/silos/{silo}/meta` | full `Meta` JSON (see above) | metadata |
| POST | `/api/song/silos/{silo}/render` | JSON/form/query: `template` + data | rendered HTML |
| GET | `/api/song/silos/{silo}/templates` | — | template names + files |

`/render` data comes from the JSON `data` object, a urlencoded form, or query
parameters — one handler for every way a page can be requested.

## Server-side rendering

Templates live in `<silo>/.templates/*.tmpl` and are parsed as one
`html/template` set with a rich default `FuncMap`:

- **Typed strings** — `html`, `htmlAttr`, `js`, `jsStr`, `css`, `url`, `srcset`
- **JSON** — `json` (safe in `<script>`), `toJSON`, `prettyJSON`, `fromJSON`
- **Strings** — `upper`, `lower`, `title`, `trim`, `trimPrefix/Suffix`,
  `replace`, `repeat`, `contains`, `hasPrefix`, `hasSuffix`, `split`, `fields`,
  `join`, `cat`, `abbrev`, `slug`
- **Defaults/logic** — `default`, `coalesce`, `empty`, `ternary`
- **Data structures** — `dict`, `list`, `keys`, `get`, `hasKey`
- **Math** — `add`, `sub`, `mul`, `div`, `mod`, `seq`
- **Casting** — `atoi`, `itoa`, `str`, `fmt`
- **Time** — `now`, `date`, `rfc3339`, `unixTime`, `addTime`
- **Encoding** — `b64enc`, `b64dec`, `urlencode`, `urlpath`
- **Regex / hashing / misc** — `regexMatch`, `regexReplace`, `regexQuote`,
  `sha256`, `fileSize`

Custom functions can be injected via `Config.Funcs` or `Song.AddFuncs`, custom
delimiters via the engine (`Templates().SetDelims("[[","]]")`). If a template
file's mtime changes, the engine reloads it on the next render.

Page composition follows the usual html/template pattern (a `layout` template
with `title`/`content` blocks, overridden per page). Note that shared block
names resolve to a single definition per parsed set, so only one document may
override a given block.

## Magic-link authentication

Passwordless, time-limited, single-use, device-bound links — backed by the same
AES-256-GCM key, managed in memory:

| Method | Path | Body |
|---|---|---|
| POST | `/api/auth/generate` | `{"user_id","device_info"?}` |
| POST | `/api/auth/validate` | `{"link","user_id","device_info"?}` |
| POST | `/api/auth/revoke` | `{"link","user_id"}` |

## Security

See [`SECURITY.md`](SECURITY.md). Highlights:

- **Encrypted at rest** — AES-256-GCM (`nonce‖ciphertext`), keyed by `--secret`;
  decrypted only when served.
- **Hostile-path-safe store** — `../`, absolute paths, raw `.song` segments and
  reserved silo names are rejected before touching disk.
- **No raw HTML injection from stored values** — the serving layer sets MIME
  types from content; server-side rendering uses `html/template` contextual
  escaping by default (escape before `innerHTML`-style output).
- **No external modules** — `go.mod` has zero `require` lines; all crypto,
  templating and HTTP are stdlib.

## Development

```bash
go test ./...        # unit + integration tests for all packages
go vet ./...
go build ./...
go run . --seed      # demo silo + admin UI on :8080
```

The HTTP surface is black-box tested with `httptest` in
[`pkg/song/song_test.go`](pkg/song/song_test.go); the store, engine and
encryption packages have their own test files.

---

*MIT License © Azzurro Technology Inc.*