# song Security

`azzurrotech/song/pkg/song` is the top-level HTTP surface of the module. It ties
the static store, the template engine and the Web Component registry into one
handler. This document describes the security model, the guarantees and the
limits of the code as it stands.

## Threat model

song stores web applications in per-tenant silos and serves them over HTTP. It
assumes:

- The process runs on a host it trusts (filesystem, store root).
- Clients are untrusted: paths, headers and bodies all arrive hostile.

## What song protects

### 1. Path traversal and silo isolation

Every path is normalized by `pkg/static` before touching the filesystem:
absolute paths, `..` segments, backslashes and raw `.song` metadata segments are
rejected. Reserved first segments (`api`, `admin`, `health`, `song`, `.song`,
`_song`) can never be addressed as silos, so an attacker cannot reach the
metadata tree or shadow a host application's conventional routes through the
serving path.

### 2. Contextual escaping in server-side rendering

Pages are rendered with `html/template`, whose contextual auto-escaping applies
to HTML, attributes, JavaScript, CSS and URLs. Plain strings stored in files are
never injected as raw markup on re-render. Text templates are rendered with
`text/template` and served as `text/plain`, so they are not an HTML injection
vector. The `json` helper HTML-escapes `</script>` so even developer-typed data
cannot break out of a `<script>` block.

### 3. Bounded request handling

A POST upsert reads at most `maxUploadBytes` (64 MiB), and inline HTML
processing reads at most `maxInlineHTML` (4 MiB) before falling back to verbatim
streaming, so a single request cannot exhaust memory or fill the disk
unboundedly.

## Configuration checklist

- Run behind TLS (Caddy/nginx) in production; song itself is HTTP-only.
- Mount the store root (`--root`, `SONG_ROOT`) as private storage, never as
  public web content outside song.
- Keep the store root on a filesystem with appropriate POSIX permissions; song
  creates silos `0755` and files `0644`.
- There is **no secret to set**: song stores plain files and renders templates.

## Out of scope / honest limits

- **No authentication or authorization.** song has no access control of its own:
  any client that can reach it can read every silo and POST to every silo. Put
  song behind an authenticating reverse proxy, or mount it only on a private
  network, when its surface must not be public.
- **No encryption at rest.** File content is stored as plain bytes; protect the
  store root with host-level access control.
- **No rate limiting, quotas or audit logs.**
- **No per-user authorization** beyond silo name separation.

These are deliberate simplifications, not implemented-but-unwritten claims.

## Reporting a vulnerability

Contact the maintainers privately via the Azzurro Technology org. Do not open a
public issue for a live vulnerability. Advisories will be published as they are
addressed.

---

*MIT License © Azzurro Technology Inc.*
