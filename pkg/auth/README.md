# auth — magic-link authentication service

## Overview

`auth` is the passwordless-authentication layer of the `azzurrotech/song`
module. It wraps `internal/encryption.EncryptionManager` and adds an in-memory
ledger of issued links so that a link can be marked as used (single-use) or
revoked.

A magic link is a self-contained AES-256-GCM ciphertext containing the user ID,
an expiry timestamp and a device signature. Validation is delegated to the
encryption manager; this package records each validated link in the
`AuthService.issuedLinks` map and exposes `MarkLinkAsUsed`.

The package is used by `pkg/song`, which exposes it over HTTP at `/api/auth/*`.
It is standard-library-only Go.

## Public API

```go
type GenerateLinkRequest struct {
    UserID     string `json:"user_id"`
    DeviceInfo string `json:"device_info,omitempty"`
}
```
Request body for issueing a link.

```go
type GenerateLinkResponse struct {
    MagicLink string    `json:"magic_link"`
    ExpiresAt time.Time `json:"expires_at"`
}
```
Response for an issued link.

```go
type ValidateLinkRequest struct {
    Link       string `json:"link"`
    UserID     string `json:"user_id"`
    DeviceInfo string `json:"device_info,omitempty"`
}
```
Request body for validating a link.

```go
type ValidateLinkResponse struct {
    Valid     bool      `json:"valid"`
    UserID    string    `json:"user_id,omitempty"`
    ExpiresAt time.Time `json:"expires_at,omitempty"`
}
```
Response for a successful validation.

```go
type AuthService struct { /* unexported: encryptionManager, issuedLinks */ }
```
Magic-link service: an encryption manager plus the issued-link ledger.

```go
func NewAuthService(secretKey string) (*AuthService, error)
```
Builds the encryption manager (secret must be ≥ 32 characters) and an empty
ledger. Wraps the underlying error with `"failed to create encryption manager"`.

```go
func (s *AuthService) GenerateMagicLink(userID string, deviceInfo string) (*encryption.MagicLink, error)
```
Generates a token with the default 24-hour TTL, immediately validates it, and
stores the validated `*encryption.MagicLink` in the ledger. Returns the stored
link, whose `Token` is the token and `Expiry` the expiry.

```go
func (s *AuthService) ValidateMagicLink(link, userID, deviceInfo string) (*encryption.MagicLink, error)
```
If `link` is already in the ledger and not `Used`, returns the cached link
without re-checking expiry. Otherwise validates through the encryption manager
(authenticity, expiry, device binding, user match) and stores the result.

```go
func (s *AuthService) MarkLinkAsUsed(link string) error
```
Flips `Used` for a known link. Errors with `"magic link not found"` or
`"magic link already used"`.

## Usage

```go
svc, err := auth.NewAuthService(os.Getenv("SONG_SECRET"))
if err != nil {
    log.Fatal(err)
}

ml, err := svc.GenerateMagicLink("alice@example.com", "phone-7")
if err != nil {
    log.Fatal(err)
}
// Deliver ml.Token to the user out of band.

got, err := svc.ValidateMagicLink(ml.Token, "alice@example.com", "phone-7")
if err != nil {
    log.Fatal(err)
}
_ = svc.MarkLinkAsUsed(got.Token) // consume it
```

## Configuration, defaults and limits

- **Secret:** forwarded to `encryption.NewEncryptionManager`, which requires at
  least 32 characters.
- **TTL:** fixed at the encryption package default of **24 hours**;
  `GenerateMagicLink` passes `time.Time{}` to mean "default".
- **Single use:** enforced only through `MarkLinkAsUsed`. `ValidateMagicLink`
  itself does not consume a link.
- **Device binding:** optional; empty `deviceInfo` skips the check in the
  encryption manager.
- **Ledger:** `issuedLinks map[string]*encryption.MagicLink`, keyed by the raw
  token string. It grows without bound and is **not** persisted.
- **Concurrency:** the map has no mutex; the service assumes a single writer.

## Testing

From `/home/matthew/Projects/Platform/stenella/atp/song`:

```bash
go test ./pkg/auth/
```

This package contains **no `_test.go` files**, so the command reports
`no test files`. Its behavior is exercised end to end by
`pkg/song/song_test.go` (`TestMagicLinkAPI`) and by the encryption package's
magic-link tests, which cover generation, validation, expiry and device
binding.

## Design notes and invariants

- Generation validates before returning, so a malformed clock/secret
  combination surfaces at mint time.
- A cached link is returned as-is while `Used` is false; the ledger is the only
  replay gate. Once `MarkLinkAsUsed` flips `Used`, `ValidateMagicLink` will not
  return the cached entry and will fall through to cryptographic validation,
  which succeeds again for an unexpired token — the HTTP layer performs the
  "already used" rejection.
- The returned `*encryption.MagicLink` is a pointer stored in the ledger; the
  same struct is mutated by `MarkLinkAsUsed`.
- No logging, no cleanup, no expiry sweep: entries remain until restart.
