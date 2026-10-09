# encryption — AES-256-GCM secrets and magic-link tokens

## Overview

`encryption` is the internal cryptographic core of the `azzurrotech/song` module.
It is an **internal** package: it can only be imported by code inside the
`azzurrotech/song` module (for example `pkg/static` and `pkg/auth`).

It does two jobs with one derived key:

- **Encrypt file content at rest.** `EncryptBytes` / `DecryptBytes` store bytes
  as `nonce || ciphertext` under AES-256-GCM; `pkg/static` uses this for files
  flagged `encrypt=true`.
- **Mint and validate magic-link tokens.** `GenerateMagicLink` /
  `ValidateLink` produce a self-contained, tamper-evident, expiring,
  device-bound string.

The key is derived once in `NewEncryptionManager` as `SHA-256(secretKey)`, which
yields exactly 32 bytes for AES-256. The same manager instance is shared by the
store and the auth service so file content and tokens use the same secret.

The package imports only the Go standard library.

## Public API

```go
type MagicLink struct {
    Token      string
    UserID     string
    Expiry     time.Time
    DeviceInfo string
    Used       bool
}
```
Validated passwordless authentication link. `Used` is a plain field; enforcement
is the caller's responsibility.

```go
type EncryptedPayload struct {
    UserID          string
    Expiry          int64
    DeviceSignature string
}
```
Decoded content of a magic-link token.

```go
type EncryptionManager struct { /* unexported key */ }
```
AES-256-GCM encryptor keyed by `SHA-256(secret)`.

```go
func NewEncryptionManager(secretKey string) (*EncryptionManager, error)
```
Derives a 32-byte key from `secretKey`. Returns an error when
`len(secretKey) < 32`.

```go
func (em *EncryptionManager) EncryptBytes(plain []byte) ([]byte, error)
func (em *EncryptionManager) DecryptBytes(data []byte) ([]byte, error)
```
Seal / open raw bytes as `nonce || ciphertext`. Decrypt rejects blobs shorter
than the nonce size and fails authentication on tampering.

```go
func (em *EncryptionManager) GenerateMagicToken() (string, error)
```
Returns 32 cryptographically random bytes, base64 URL-encoded.

```go
func (em *EncryptionManager) EncryptData(userID string, expiry time.Time, deviceInfo string) (string, error)
```
Builds `userID|expiryUnix|deviceSignature`, seals it and returns base64 URL.
`deviceSignature = base64(SHA-256(deviceInfo))`.

```go
func (em *EncryptionManager) DecryptData(encrypted string) (*EncryptedPayload, error)
```
Base64-decodes, decrypts and splits the payload. Errors on bad base64, bad
authentication, a payload with other than three `|`-separated fields, or a
non-numeric expiry.

```go
func (em *EncryptionManager) ValidateLink(link, userID, deviceInfo string) (*MagicLink, error)
```
Requires non-empty `link` and `userID`; decrypts; rejects when
`time.Now().Unix() > payload.Expiry`; checks device binding then that
`payload.UserID == userID`.

```go
func (em *EncryptionManager) GenerateMagicLink(userID string, deviceInfo string, duration time.Time) (string, error)
```
Returns a token. A zero `duration` means **24 hours from now**; otherwise
`duration` is used verbatim as the expiry instant.

## Usage

```go
em, err := encryption.NewEncryptionManager(os.Getenv("SONG_SECRET"))
if err != nil {
    log.Fatal(err)
}

ct, _ := em.EncryptBytes([]byte("secret"))
plain, _ := em.DecryptBytes(ct)

link, _ := em.GenerateMagicLink("alice@example.com", "device-42", time.Time{})
ml, err := em.ValidateLink(link, "alice@example.com", "device-42")
```

## Configuration, defaults and limits

- **Secret length:** minimum 32 characters, enforced in `NewEncryptionManager`.
  The value is not measurably strengthened beyond derivation; 32 random bytes
  (or more) are recommended.
- **Key derivation:** `SHA-256(secret)` → 32 bytes, AES-256.
- **Mode:** GCM with a fresh random nonce of `gcm.NonceSize()` bytes (12 for
  AES-GCM) per seal, from `crypto/rand`.
- **Byte format:** `nonce || ciphertext || tag`; base64 `URLEncoding` on the
  wire for tokens and data.
- **Default TTL:** 24 h (`GenerateMagicLink` with a zero `duration`).
- **Device binding:** omitted (treated as valid) when `deviceInfo == ""`;
  otherwise the SHA-256 device signature must match.
- **Expiry comparison:** strictly `now > expiry`; a token exactly at its expiry
  second is still accepted.
- **`newGCM`, `seal`, `open`, `verifyDeviceBinding`** are unexported.

## Testing

From `/home/matthew/Projects/Platform/stenella/atp/song`:

```bash
go test ./internal/encryption/
```

`encryption_test.go` covers: short-secret rejection; encrypt/decrypt round
trips including non-ASCII content; ciphertext differs from plaintext and carries
overhead; unique nonces; tamper detection; short-ciphertext rejection; magic
link round trip plus wrong-device and wrong-user rejection; empty device
skipping binding; expiry rejection; and uniqueness of generated magic tokens.

## Design notes and invariants

- The derived key is the single trust anchor; changing the secret makes existing
  ciphertext and outstanding links undecryptable.
- A nonce is never reused: it is regenerated with `crypto/rand` for every
  `seal`.
- GCM authentication covers the whole payload, so a modified byte fails
  `open` rather than returning altered plaintext.
- `ValidateLink` order: decrypt/authenticate → expiry → device → user. Each step
  fails closed with an error.
- `MagicLink.Used` is informational; this package never flips it. Single-use
  tracking lives in `pkg/auth`.
- No AAD is passed to `Seal`/`Open` (the `nil` argument); confidentiality and
  integrity rely on the key plus nonce uniqueness alone.
