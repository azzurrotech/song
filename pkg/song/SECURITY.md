# song Security

`azzurrotech/song/pkg/song` is the HTTP surface of song. It wires the storage,
encryption, auth and templating packages into one handler. This document states
what it protects, the mitigations that are actually implemented, and the
guarantees it does **not** claim.

## Assets protected

1. **Hosted file content**, including files encrypted at rest.
2. **Silo isolation** and the store root boundary.
3. **Authentication state** (magic links) and the single-use ledger.
4. **Rendered pages** (data must not become script injection).
5. **The management surface** (`/admin`, `/api/song/*`, `/api/auth/*`).

## Threat model

- **Path traversal / silo escape.** A request tries to read another silo, the
  `.song` metadata tree, or the host filesystem.
- **Stored-file XSS.** A tenant uploads HTML/SVG/JS and a victim loads it from
  song's origin.
- **Magic-link replay.** A captured link is reused.
- **Plaintext at rest.** Someone with disk access reads encrypted content.
- **Unauthenticated management.** Anyone reachable on the network administers
  song.

Clients, headers, bodies, paths and file names are untrusted. The process host,
store root and secret are trusted.

## Mitigations implemented

- **Delegated path confinement.** All filesystem access goes through
  `pkg/static` (`ParseRel`/`cleanRel`/`resolve`), which rejects `..`, NUL,
  backslashes and `.song` segments and asserts the joined path stays inside the
  silo. The API calls `static.ParseRel` before every read/write/delete.
- **Reserved-route separation.** `splitSiloPath` + `static.IsReservedSilo`
  prevent `api`, `admin`, `health`, `song` and `.song` from being addressed as
  silos, so metadata and management endpoints are not reachable through the
  serving path.
- **Encryption at rest with transparency.** Files created with `encrypt=true`
  are stored as AES-256-GCM `nonce || ciphertext`; serving decrypts them and
  sets `X-Song-Encrypted: 1` so downstream systems know plaintext crossed the
  wire.
- **Contextual escaping.** `/render` and template routes execute through
  `pkg/tmpl`/`html/template`; ordinary data values are escaped for their
  context. The `json` helper HTML-escapes `</script>`.
- **Magic links.** Generated via `pkg/auth` with a 24-hour expiry, optional
  device binding, and a single-use ledger. `/api/auth/validate` rejects a link
  whose `Used` flag is set (`404`), and marks an expired link used.
- **Method and body discipline.** Handlers restrict methods and return JSON
  `{"error": …}`; `mediaTypeOf` normalizes content types before parsing.
- **Bounded uploads.** Multipart file parts/`/upload` streams are capped at
  `maxUploadBytes` (64 MiB); multipart parsing spills beyond 16 MiB.

## Honest limits / non-claims

- **Management is unauthenticated.** `/admin`, `/api/song/*` and `/api/auth/*`
  have no authentication, authorization or CSRF protection in this build. Put
  song behind an authenticating reverse proxy or private network.
- **No rate limiting, throttling or lockout** on any endpoint, including magic
  link generation and validation.
- **No audit logging** of reads, writes, deletes, logins or revocations.
- **Magic-link single-use state is in-memory.** The ledger in `pkg/auth` resets
  on restart and is not shared across instances, so replay protection does not
  survive a restart or horizontal scaling.
- **No TLS.** song is HTTP-only; terminate TLS at a proxy.
- **Stored files are served with their declared content type.** An `index.html`,
  `.svg`, `.js` or `.wasm` uploaded by a tenant is served as active content from
  song's origin. Do not host mutually untrusted tenants on one origin; use
  separate hosts/domains per tenant if that matters.
- **No Content-Security-Policy, X-Frame-Options or CORS headers** are set by
  song.
- **Plaintext for unencrypted files.** Only files explicitly flagged `encrypt`
  are protected at rest; everything else is readable on disk.
- **Raw bodies are unbounded by `maxUploadBytes`.** Raw-body create and JSON/form
  `content` fields are read into memory and are not capped by that constant.
- **Template authors are trusted within their silo** and can invoke unescaped
  `html`/`js` helpers (see `pkg/tmpl`).
- **Errors can echo input.** Some `400`/`500` bodies include path or store error
  text; run a sanitizing proxy if error detail must not reach clients.
- **No per-user authorization.** A validated magic link proves token control
  only; there are no roles or per-silo ACLs.
- **The documented default secret** (`main.go`) must never be used; set
  `SONG_SECRET`.

## Dependencies

Standard library only, plus module-internal packages `azzurrotech/song/pkg/auth`,
`azzurrotech/song/pkg/static` and `azzurrotech/song/pkg/tmpl`. No third-party
modules.

## Reporting

Report vulnerabilities privately to `security@azzurro.tech`. Do not open a
public issue for a live vulnerability.
