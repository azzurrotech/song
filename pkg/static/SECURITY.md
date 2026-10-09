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
3. **Private metadata.** The hidden `.song/meta.json` (encryption flags, index
   rules, template routes) must be unreachable and unlistable through the API.
4. **File content at rest.** Files flagged encrypted are ciphertext on disk.

## Threat model

- **Path traversal.** `../`, absolute paths, backslashes, NUL bytes, symlink
  games and `.song/../x`-style inputs attempting to escape the silo.
- **Silo-name attacks.** `..`, `.`, names with separators, reserved names tied
  to `/api`, `/admin`, `/health`, `/song`, `.song`.
- **Information disclosure.** Directory listings or content reads revealing
  another silo or the metadata tree.
- **Integrity.** Stored plaintext being readable by someone with host disk
  access.

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
  `health`, `song`, `.song`.
- **Hidden entries are never listed.** `List` and `ListSilos` skip names
  beginning with `.`, so `.song` and `.templates` never appear in responses.
- **Encryption at rest.** Files written with `encrypt=true` go through
  `EncryptionManager.EncryptBytes` (`nonce || ciphertext`, AES-256-GCM) and are
  recorded in `Meta.Encrypted`; `Read`/`Open` decrypt only flagged paths.
- **Traversal before I/O on every verb.** Create, Update, Upload, Delete,
  List, Stat, Read and Open all begin with `filePath`.

## Honest limits / non-claims

- **Symlinks are not explicitly rejected.** Containment is checked on the
  joined path string; a symlink *inside* a silo pointing outside would be
  followed by `os.Open`/`os.ReadFile`. The assumption is that tenants cannot
  create symlinks through the API (the API only writes regular files).
- **No filesystem permissions hardening.** Silos are `0755` and files `0644`,
  world-readable within the host; protection of ciphertext depends on the
  secret, not on POSIX modes.
- **Writes are not atomic.** There is no temp-file-plus-rename; a crash or
  concurrent writer can leave a partially written file or metadata JSON. The
  `updateMeta` comment ("atomically enough for the single-writer model")
  describes the intent, not a transactional guarantee.
- **Single-writer assumption.** No file locking; concurrent writes to the same
  path can interleave.
- **No content scanning.** Any bytes with any name may be stored; SVG/HTML are
  served with their declared types (see `pkg/song`).
- **No at-rest integrity for unencrypted files.** Plain files can be modified by
  anyone with disk access without detection.
- **No quotas or size ceiling in this package.** Size limiting lives in
  `pkg/song`.

## Dependencies

Standard library only, plus the module-internal
`azzurrotech/song/internal/encryption`. Imports include `bytes`, `errors`, `fmt`,
`io`, `mime`, `os`, `path/filepath`, `sort`, `strings`, `time`,
`encoding/json`, `net/http`, `path`, `regexp`.

## Reporting

Report vulnerabilities privately to `security@azzurro.tech`. Do not open a
public issue for a live vulnerability.
