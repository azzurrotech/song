package encryption

import (
	"bytes"
	"strings"
	"testing"
	"time"
)

func TestNewEncryptionManagerRequiresLongSecret(t *testing.T) {
	if _, err := NewEncryptionManager("short"); err == nil {
		t.Fatal("expected error for short secret")
	}
	if _, err := NewEncryptionManager(strings.Repeat("x", 32)); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestEncryptDecryptBytes(t *testing.T) {
	em, err := NewEncryptionManager(strings.Repeat("k", 40))
	if err != nil {
		t.Fatal(err)
	}
	plain := []byte("The quick brown fox jumps over the lazy dog. 混んだ")
	ct, err := em.EncryptBytes(plain)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(ct, plain) {
		t.Fatal("ciphertext must differ from plaintext")
	}
	if len(ct) <= len(plain) {
		t.Fatalf("ciphertext should carry nonce+tag overhead, got %d <= %d", len(ct), len(plain))
	}
	out, err := em.DecryptBytes(ct)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(out, plain) {
		t.Fatal("round trip mismatch")
	}

	// Unique nonces: encrypting the same plaintext twice must differ.
	ct2, _ := em.EncryptBytes(plain)
	if bytes.Equal(ct, ct2) {
		t.Fatal("same plaintext encrypted twice must produce different ciphertext")
	}

	// Tampering must fail authentication.
	ct[len(ct)-1] ^= 0xFF
	if _, err := em.DecryptBytes(ct); err == nil {
		t.Fatal("tampered ciphertext must fail to decrypt")
	}
}

func TestEncryptBytesRejectShortCiphertext(t *testing.T) {
	em, _ := NewEncryptionManager(strings.Repeat("k", 32))
	if _, err := em.DecryptBytes([]byte("tiny")); err == nil {
		t.Fatal("expected error for short ciphertext")
	}
}

func TestMagicLinkRoundTripAndValidation(t *testing.T) {
	em, err := NewEncryptionManager(strings.Repeat("s", 32))
	if err != nil {
		t.Fatal(err)
	}
	user := "alice@example.com"
	device := `{"type":"mobile","id":"abc"}`
	expiry := time.Now().Add(2 * time.Hour)
	link, err := em.GenerateMagicLink(user, device, expiry)
	if err != nil {
		t.Fatal(err)
	}
	ml, err := em.ValidateLink(link, user, device)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if ml.UserID != user {
		t.Fatalf("user mismatch: %q", ml.UserID)
	}
	if ml.Expiry.Unix() != expiry.Unix() {
		t.Fatalf("expiry mismatch")
	}

	// Wrong device binding must fail.
	if _, err := em.ValidateLink(link, user, "other-device"); err == nil {
		t.Fatal("device binding must reject a different device")
	}
	// Wrong user must fail.
	if _, err := em.ValidateLink(link, "bob@example.com", device); err == nil {
		t.Fatal("user mismatch must fail")
	}
	// Empty device means no binding check.
	if _, err := em.ValidateLink(link, user, ""); err != nil {
		t.Fatalf("empty device should skip binding: %v", err)
	}
}

func TestMagicLinkExpiry(t *testing.T) {
	em, _ := NewEncryptionManager(strings.Repeat("s", 32))
	link, err := em.GenerateMagicLink("u", "", time.Now().Add(-time.Minute)) // already expired
	if err != nil {
		t.Fatal(err)
	}
	if _, err := em.ValidateLink(link, "u", ""); err == nil {
		t.Fatal("expired link must fail validation")
	}
}

func TestGenerateMagicToken(t *testing.T) {
	em, _ := NewEncryptionManager(strings.Repeat("s", 32))
	a, _ := em.GenerateMagicToken()
	b, _ := em.GenerateMagicToken()
	if a == "" || b == "" || a == b {
		t.Fatalf("tokens must be present and unique: %q %q", a, b)
	}
}
