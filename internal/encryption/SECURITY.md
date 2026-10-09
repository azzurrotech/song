# encryption Security

`azzurrotech/song/internal/encryption` implements the cryptographic primitives
used by song. It is standard-library-only Go. This document states what it
actually protects, how, and where the guarantees stop.

## Assets protected

1. **File content at rest.** Bytes stored through `EncryptBytes` (used by
   `pkg/static` for `encrypt=true` files).
2. **Magic-link tokens.** The `GenerateMagicLink` → `ValidateLink` pair
   authenticates a user, an expiry and an optional device binding.

## Threat model

The secret passed to `NewEncryptionManager` is trusted and kept out of the
repository, transport and logs. Everything else is untrusted: ciphertext blobs,
token strings, `userID`, `deviceInfo`, and the clock-dependent expiry value.
Assumed adversaries:

- Someone with read access to the store directory (wants plaintext).
- Someone who can present a forged or modified token (wants authentication).
- Someone who captures a token and replays it later or from another device.

## Mitigations implemented

- **AES-256-GCM.** `EncryptionManager.newGCM` builds GCM over a key derived via
  `SHA-256(secretKey)`. GCM provides both confidentiality and integrity, so
  altered ciphertext fails to open.
- **Random per-message nonces.** `seal` fills a `gcm.NonceSize()` (12-byte)
  buffer from `crypto/rand` and returns `nonce || ciphertext`. The test asserts
  that encrypting the same plaintext twice produces different output.
- **Authenticated open.** `open` rejects data shorter than the nonce size and
  surfaces GCM authentication failures as errors; no partial plaintext is
  returned.
- **Length floor on the secret.** `NewEncryptionManager` rejects
  `len(secretKey) < 32`. This blocks trivially short secrets but is a floor, not
  a strength meter.
- **Tamper-evident tokens.** `EncryptData` packs
  `userID|expiryUnix|SHA-256(deviceInfo)` inside the AEAD; `DecryptData` rejects
  malformed payloads, and `ValidateLink` re-authenticates before returning.
- **Expiry.** `ValidateLink` rejects tokens with `now > expiry`; the default in
  `GenerateMagicLink` is 24 hours.
- **Device binding.** `verifyDeviceBinding` compares the SHA-256-based device
  signature. An empty `deviceInfo` intentionally disables the check.
- **Random magic tokens.** `GenerateMagicToken` reads 32 bytes from
  `crypto/rand` and base64 URL-encodes them.

## Honest limits / non-claims

- **No key rotation.** One secret keys all files and tokens; there is no
  re-wrap, versioning or KMS integration. Rotating the secret breaks existing
  data.
- **No additional authenticated data.** `seal`/`open` pass `nil` AAD, so there
  is no binding to filename, silo or context beyond the ciphertext itself.
- **No `Used` enforcement.** `MagicLink.Used` is a data field; replay protection
  is implemented only by the in-memory ledger in `pkg/auth`, which resets on
  every restart.
- **Secret quality is not validated.** Only the length floor is checked; a
  32-character low-entropy string is accepted.
- **Base64 is not a confidentiality boundary.** `URLEncoding` of ciphertext and
  of random tokens is transport encoding only.
- **No compression, no padding control.** The scheme is plain AEAD.
- **No zeroization.** Derived keys and plaintext slices live in ordinary Go
  memory.

## Dependencies

Standard library only. Imports are `crypto/aes`, `crypto/cipher`, `crypto/rand`,
`crypto/sha256`, `encoding/base64`, `errors`, `fmt`, `strconv`, `strings`,
`time`. There are no third-party modules.

## Reporting

Report vulnerabilities privately to `security@azzurro.tech`. Do not open a
public issue for a live vulnerability.
