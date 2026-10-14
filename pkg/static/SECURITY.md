# static Security

`azzurrotech/song/pkg/static` owns all filesystem access for song. It is the
component that turns untrusted HTTP paths and silo names into on-disk
operations. This document describes the assets it protects, the mitigations it
implements, and the limits it does not hide.

## Assets protected

1. **Silo isolation.** One tenant's files must not be readable or writable from
   another silo's path.
2. **The store root boundary.** No operation may read or write outside
   `<root>/<silo>/`.
3. **Private metadata.** The hidden `.song/meta.json` (template routes) must be
   unreachable and unlistable through the serving path.

## Threat model

- **Path traversal.** `../`, absolute paths, backslashes, NUL bytes, symlink
  games and `.song/../x`-style inputs attempting to escape the silo.
- **Silo-name attacks.** `..`, `.`, names with separators, and reserved
  segments that belong to song's own surfaces or to host applications.
- **Information disclosure.** Directory listings or content reads revealing
  another silo or the metadata tree.

## Mitigations implemented

- **Normalize before I/O.** `filePath` always calls `cleanRel`, then `resolve`.
  `cleanRel` rejects NUL (`\x00`) and backslashes, cleans the path with
  `filepath.Clean`, and rejects `..` and `..<sep>` prefixes.
- **Strict containment.** `resolve` joins the cleaned relative path onto the
  silo directory and fails with `ErrTraversal` unless the result equals the base
  or is prefixed by `base + separator` — an exact, string-verified jail.
- **Metadata is off-limits.** `cleanRel` rejects any path containing a raw
  `.song` segment (even one that cleaning would collapse) and any cleaned path
  equal to or under `.song`. `ErrReserved` is returned before disk access.
- **Silo names are constrained.** `siloNameRe`
  (`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`) forbids separators, leading
  dots and edge punctuation; `ReservedSiloNames` blocks `api`, `admin`,
  `health`, `song`, `.song` and `_song`, so a silo can never shadow a
  conventional host route or the Web Component runtime.
- **Hidden entries are never listed.** `List` and `ListSilos` skip names
  beginning with `.`, so `.song` and `.templates` never appear in responses.
- **Traversal before I/O on every verb.** Create, Update, Delete, List, Stat,
  Read and Open all begin with `filePath`.

## Honest limits / non-claims

- **Symlinks are not explicitly rejected.** Containment is checked on the
  joined path string; a symlink *inside* a silo pointing outside would be
  followed by `os.Open`/`os.ReadFile`. The assumption is that tenants cannot
  create symlinks through the HTTP surface (it only writes regular files).
- **No filesystem permissions hardening.** Silos are `0755` and files `0644`,
  world-readable within the host. File content is stored as **plain bytes**: this
  package provides no confidentiality at rest, so protect the store root with
  host-level access control.
- **Writes are not atomic.** There is no temp-file-plus-rename; a crash or
  concurrent writer can leave a partially written file or metadata JSON.
- **Single-writer assumption.** No file locking; concurrent writes to the same
  path can interleave.
- **No content scanning.** Any bytes with any name may be stored; SVG/HTML are
  served with their declared types (see `pkg/song`).
- **No at-rest integrity.** Plain files can be modified by anyone with disk
  access without detection.
- **No quotas or size ceiling in this package.** Size limiting lives in
  `pkg/song`.

## Dependencies

Standard library only. Imports: `encoding/json`, `errors`, `fmt`, `io`, `mime`,
`os`, `path/filepath`, `sort`, `strings`, `time`.

## Reporting

Report vulnerabilities privately to `security@azzurro.tech`. Do not open a
public issue for a live vulnerability.
