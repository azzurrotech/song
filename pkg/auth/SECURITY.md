# auth Security

`azzurrotech/song/pkg/auth` provides magic-link authentication for song. It is a
thin stateful wrapper over `internal/encryption`. This document describes the
protections it actually implements and the guarantees it does not provide.

## Assets protected

1. **Authentication decisions** — whether a presented link authenticates a given
   user (optionally on a given device).
2. **Single-use state** — the record that a link has already been consumed or
   revoked.

## Threat model

- **Link replay.** An attacker who obtains a token tries to use it again, from a
  different user ID, or from a different device.
- **Token forgery/tampering.** An attacker modifies or fabricates a token.
- **Expiry abuse.** An old token is presented after it should have lapsed.
- **Cross-user use.** A valid token for Alice is presented as Bob.

The secret is trusted. All tokens and request fields are untrusted.

## Mitigations implemented

- **Authenticated, encrypted tokens.** Token contents are sealed with
  AES-256-GCM (`EncryptionManager.EncryptData`) and only returned by
  `ValidateMagicLink` after successful decryption/authentication.
- **User binding.** `GenerateMagicLink` and `ValidateMagicLink` pass the user ID
  into the encryption layer, which rejects a mismatch.
- **Expiry.** Tokens expire after 24 hours by default and are rejected when
  `time.Now().Unix() > Expiry`.
- **Optional device binding.** A SHA-256 device signature is embedded; when
  `deviceInfo` is non-empty it must match.
- **Single-use ledger.** `MarkLinkAsUsed` refuses an unknown link and refuses to
  consume a link twice, backing the HTTP layer's replay rejection.
- **Revocation entry point.** A caller can mark any issued link used to revoke
  it.

## Honest limits / non-claims

- **The ledger is in-memory and process-local.** `issuedLinks` is a plain map
  with no persistence. A restart forgets every issued/used/revoked link; replay
  protection does not survive a restart or span multiple instances.
- **No concurrency control.** The map has no mutex. Concurrent
  generate/validate/revoke calls can race; run `go test -race` before relying on
  concurrent access.
- **No expiry sweep.** Consumed and expired entries are never removed, so the
  map grows for the life of the process (a slow memory leak under load).
- **`ValidateMagicLink` does not enforce single use.** It happily returns an
  unexpired token again; only the HTTP handler in `pkg/song` checks `Used` and
  rejects. Anything using this package directly must call `MarkLinkAsUsed`.
- **`Used` is a mutable field, not a claim.** The pointer returned is the same
  object stored in the ledger.
- **No rate limiting, throttling, lockout or audit log.** Nothing limits how
  often `GenerateMagicLink` or `ValidateMagicLink` may be called.
- **No delivery channel.** The service only mints tokens; safe out-of-band
  delivery is the caller's responsibility.
- **No per-user authorization.** A validated link proves control of the token,
  not a role or permission.
- **Secret strength is only length-checked** (≥ 32 characters) by the underlying
  encryption manager.

## Dependencies

Standard library only, plus the module-internal
`azzurrotech/song/internal/encryption`. Imports: `fmt`, `time`, and that internal
package. No third-party modules.

## Reporting

Report vulnerabilities privately to `security@azzurro.tech`. Do not open a
public issue for a live vulnerability.
