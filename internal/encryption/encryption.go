// Package encryption provides secret-key AES-256-GCM encryption for song.
// The same derived key is used for magic-link tokens and for encrypting
// hosted file content at rest. Standard library only.
package encryption

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// MagicLink is a validated passwordless authentication link.
type MagicLink struct {
	Token      string
	UserID     string
	Expiry     time.Time
	DeviceInfo string
	Used       bool
}

// EncryptedPayload is the decoded content of an encrypted magic-link token.
type EncryptedPayload struct {
	UserID          string
	Expiry          int64
	DeviceSignature string
}

// EncryptionManager performs AES-256-GCM encryption with a key derived from
// the supplied secret via SHA-256. The same secret unlocks magic links and
// encrypted file content.
type EncryptionManager struct {
	key []byte
}

// NewEncryptionManager derives a 32-byte key from secretKey.
func NewEncryptionManager(secretKey string) (*EncryptionManager, error) {
	if len(secretKey) < 32 {
		return nil, errors.New("secret key must be at least 32 characters long")
	}
	sum := sha256.Sum256([]byte(secretKey))
	return &EncryptionManager{key: sum[:]}, nil
}

// newGCM builds an AES-GCM AEAD from the derived key.
func (em *EncryptionManager) newGCM() (cipher.AEAD, error) {
	block, err := aes.NewCipher(em.key)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}
	return gcm, nil
}

// seal encrypts plain and returns nonce||ciphertext.
func (em *EncryptionManager) seal(plain []byte) ([]byte, error) {
	gcm, err := em.newGCM()
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("failed to generate nonce: %w", err)
	}
	return gcm.Seal(nonce, nonce, plain, nil), nil
}

// open decrypts a nonce||ciphertext blob produced by seal.
func (em *EncryptionManager) open(data []byte) ([]byte, error) {
	gcm, err := em.newGCM()
	if err != nil {
		return nil, err
	}
	if len(data) < gcm.NonceSize() {
		return nil, errors.New("ciphertext too short")
	}
	nonce, ciphertext := data[:gcm.NonceSize()], data[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt data: %w", err)
	}
	return plain, nil
}

// EncryptBytes encrypts raw bytes (used for file content at rest).
func (em *EncryptionManager) EncryptBytes(plain []byte) ([]byte, error) {
	return em.seal(plain)
}

// DecryptBytes decrypts a blob produced by EncryptBytes.
func (em *EncryptionManager) DecryptBytes(data []byte) ([]byte, error) {
	return em.open(data)
}

// GenerateMagicToken returns a fresh cryptographically random token.
func (em *EncryptionManager) GenerateMagicToken() (string, error) {
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return "", fmt.Errorf("failed to generate random token: %w", err)
	}
	return base64.URLEncoding.EncodeToString(tokenBytes), nil
}

// EncryptData encodes userID, expiry and a device signature into a single
// self-contained, tamper-evident magic-link token.
func (em *EncryptionManager) EncryptData(userID string, expiry time.Time, deviceInfo string) (string, error) {
	deviceHash := sha256.Sum256([]byte(deviceInfo))
	deviceSignature := base64.URLEncoding.EncodeToString(deviceHash[:])
	payload := fmt.Sprintf("%s|%d|%s", userID, expiry.Unix(), deviceSignature)
	encrypted, err := em.seal([]byte(payload))
	if err != nil {
		return "", err
	}
	return base64.URLEncoding.EncodeToString(encrypted), nil
}

// DecryptData parses and authenticates a magic-link token.
func (em *EncryptionManager) DecryptData(encrypted string) (*EncryptedPayload, error) {
	data, err := base64.URLEncoding.DecodeString(encrypted)
	if err != nil {
		return nil, fmt.Errorf("failed to decode encrypted data: %w", err)
	}
	plain, err := em.open(data)
	if err != nil {
		return nil, err
	}
	parts := strings.Split(string(plain), "|")
	if len(parts) != 3 {
		return nil, errors.New("invalid encrypted data format")
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid expiry timestamp: %w", err)
	}
	return &EncryptedPayload{
		UserID:          parts[0],
		Expiry:          expiry,
		DeviceSignature: parts[2],
	}, nil
}

// ValidateLink checks authenticity, expiry, device binding and user identity.
func (em *EncryptionManager) ValidateLink(link, userID, deviceInfo string) (*MagicLink, error) {
	if link == "" || userID == "" {
		return nil, errors.New("link and userID are required")
	}
	payload, err := em.DecryptData(link)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt magic link: %w", err)
	}
	if time.Now().Unix() > payload.Expiry {
		return nil, errors.New("magic link has expired")
	}
	if !em.verifyDeviceBinding(payload, deviceInfo) {
		return nil, errors.New("device binding verification failed")
	}
	if payload.UserID != userID {
		return nil, errors.New("user ID mismatch")
	}
	return &MagicLink{
		Token:      link,
		UserID:     userID,
		Expiry:     time.Unix(payload.Expiry, 0),
		DeviceInfo: deviceInfo,
		Used:       false,
	}, nil
}

// GenerateMagicLink creates a magic-link token with the given expiry
// (zero value means 24 hours from now).
func (em *EncryptionManager) GenerateMagicLink(userID string, deviceInfo string, duration time.Time) (string, error) {
	expiry := time.Now().Add(time.Hour * 24)
	if !duration.IsZero() {
		expiry = duration
	}
	return em.EncryptData(userID, expiry, deviceInfo)
}

func (em *EncryptionManager) verifyDeviceBinding(payload *EncryptedPayload, deviceInfo string) bool {
	if deviceInfo == "" {
		return true
	}
	deviceHash := sha256.Sum256([]byte(deviceInfo))
	deviceSignature := base64.URLEncoding.EncodeToString(deviceHash[:])
	return deviceSignature == payload.DeviceSignature
}
