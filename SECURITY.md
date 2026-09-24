# song Security

song is a static-file hosting and management platform. This document describes
what the code actually does today — the security model, the guarantees, and the
limits. Everything listed here is implemented in standard-library Go.

## Threat model

song stores web applications in per-tenant silos and serves them over HTTP. It
is a **hosting and management surface**, not a general-purpose IAM. It assumes:

- The process runs on a host it trusts (filesystem root, store root, secret).
- Clients are untrusted: paths, headers, bodies, IDs and file names all arrive
  hostile.

## What song protects

### 1. Sensitive files at rest (AES-256-GCM)

Files created with `encrypt=true` are stored as `nonce ‖ ciphertext`
(`internal/encryption`), never as plaintext. The key is derived from the
configured `--secret` (minimum 32 characters). Decryption happens only when a
file is served, and every encrypted response carries `X-Song-Encrypted: 1` so
proxies and clients know the plaintext crossed the wire.

The secret is the single point of trust: **rotate it deliberately** (there is no
key-rotation story yet), keep it out of source control, and pass it via
`SONG_SECRET` in production. The same secret keys magic-link tokens.

### 2. Path traversal and silo isolation

Every store/index/API path is normalized before touching the filesystem:
absolute paths, `..` segments, backslashes and raw `.song` metadata segments are
rejected (`pkg/static`). Reserved silo names (`api`, `admin`, `health`, `song`,
`.song`) can never be addressed as silos, so an attacker cannot reach the API,
UI or metadata store through the serving path.

### 3. Contextual escaping in server-side rendering

Pages are rendered with `html/template`, whose contextual auto-escaping is
applied for HTML, attributes, JS, CSS and URLs. Plain strings stored in files
are never injected as raw markup on re-render. The `json` helper HTML-escapes
`</script>` so even developer-typed data cannot break out of a `<script>` block.
Typed strings (`template.HTML`, `template.JS`, …) are available intentionally —
only use them for content you trust.

### 4. Magic links

- Time-limited (24 h) and single-use (tracked in memory, process-local).
- Device-bound via a SHA-256 hash of device information.
- Tokens are AES-256-GCM ciphertext; validation re-derives and authenticates
  them.
- Caveat: the single-use ledger is **in-memory** today. A restart forgets it.

## Configuration checklist

- Set a real `SONG_SECRET` (≥ 32 chars); never the documented default.
- Run behind TLS (Caddy/nginx) in production; song itself is HTTP-only.
- Mount the store root `/data` as private storage, never as public web content
  outside song.
- Keep `SONG_ROOT` on a filesystem with appropriate POSIX permissions; song
  creates silos world-`0755`-ish on disk (files `0644`, dirs `0755` under the
  store root).

## Out of scope / honest limits

- **No authentication on management.** The `/admin` UI and `/api/song/*`,
  `/api/auth/*` endpoints are unauthenticated in this build. Put song behind an
  authenticating reverse proxy when it must not be publicly manageable.
- **No rate limiting.**
- **No audit logs.**
- **No key rotation or secrets vault.**
- **No per-user authorization** beyond silo name separation.

These are deliberate simplifications for the current iteration, not implemented
-but-unwritten claims.

## Reporting a vulnerability

Contact the maintainers privately via the Azzurro Technology org. Do not open a
public issue for a live vulnerability. Advisories will be published as they are
addressed.

---

*MIT License © Azzurro Technology Inc.*