# song Security

song is a static-file hosting and template-processing service. This document
describes what the code actually does today — the security model, the
guarantees, and the limits. Everything listed here is implemented in
standard-library Go.

## Threat model

song stores web applications in per-tenant silos and serves them over HTTP. It
assumes:

- The process runs on a host it trusts (filesystem, store root).
- Clients are untrusted: paths, headers and bodies all arrive hostile.

## What song protects

### 1. Path traversal and silo isolation

Every store path is normalized before touching the filesystem: absolute paths,
`..` segments, backslashes and raw `.song` metadata segments are rejected
(`pkg/static`). Reserved silo names (`api`, `admin`, `health`, `song`, `.song`,
`_song`) can never be addressed as silos, so neither the metadata tree nor a
host application's conventional routes can be shadowed through the serving path.

### 2. Contextual escaping in server-side rendering

Pages are rendered with `html/template`, whose contextual auto-escaping is
applied for HTML, attributes, JS, CSS and URLs. Plain strings stored in files
are never injected as raw markup on re-render. The `json` helper HTML-escapes
`</script>` so even developer-typed data cannot break out of a `<script>` block.
Typed strings (`template.HTML`, `template.JS`, …) are available intentionally —
only use them for content you trust. Text templates are rendered with
`text/template` and served as `text/plain`, which is not an HTML injection
vector.

### 3. Bounded request handling

A POST upsert reads at most 64 MiB (`maxUploadBytes`); inline HTML processing
reads at most 4 MiB (`maxInlineHTML`) before falling back to verbatim streaming.
A single request therefore cannot consume unbounded memory or disk.

## Configuration checklist

- Run behind TLS (Caddy/nginx) in production; song itself is HTTP-only.
- Mount the store root (`--root` / `SONG_ROOT`) as private storage, never as
  public web content outside song.
- Keep `SONG_ROOT` on a filesystem with appropriate POSIX permissions; song
  creates silos `0755`-ish on disk (files `0644`, dirs `0755` under the store
  root).
- No secret is required: song stores plain files and renders templates.

## Out of scope / honest limits

- **No authentication or authorization.** song has no access control of its own.
  Any client that can reach it can read every silo and POST to every silo. Put
  song behind an authenticating reverse proxy, or mount it only on a private
  network, when its surface must not be public.
- **No encryption at rest.** File content is stored as plain bytes; protect the
  store root with host-level access control.
- **No rate limiting.**
- **No audit logs.**
- **No per-user authorization** beyond silo name separation.

These are deliberate simplifications for the current iteration, not
implemented-but-unwritten claims.

## Reporting a vulnerability

Contact the maintainers privately via the Azzurro Technology org. Do not open a
public issue for a live vulnerability. Advisories will be published as they are
addressed.

---

*MIT License © Azzurro Technology Inc.*
