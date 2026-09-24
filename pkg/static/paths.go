package static

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
)

// Sentinel errors surfaced by the store.
var (
	ErrInvalidName = errors.New("invalid name")
	ErrReserved    = errors.New("name is reserved")
	ErrTraversal   = errors.New("path escapes the store root")
	ErrNotFound    = errors.New("not found")
	ErrExists      = errors.New("already exists")
	ErrIsDir       = errors.New("is a directory")
	ErrIsFile      = errors.New("is a file")
)

// siloNameRe allows a conservative web-safe name: letters, digits, dot,
// dash and underscore, starting and ending with a letter or digit.
var siloNameRe = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9._-]*[A-Za-z0-9])?$`)

// ReservedSiloNames cannot be used as silo names because the HTTP layer
// routes those segments to built-in surfaces (API, admin UI, health).
var ReservedSiloNames = map[string]bool{
	"api":    true,
	"admin":  true,
	"health": true,
	"song":   true,
	".song":  true,
}

// IsReservedSilo reports whether a first path segment collides with a
// built-in route of the song HTTP layer.
func IsReservedSilo(name string) bool {
	return ReservedSiloNames[name]
}

func validateSiloName(name string) error {
	if !siloNameRe.MatchString(name) {
		return fmt.Errorf("%w: %q", ErrInvalidName, name)
	}
	if IsReservedSilo(name) {
		return fmt.Errorf("%w: %q is reserved", ErrReserved, name)
	}
	return nil
}

// ParseRel validates and normalizes a silo-relative path supplied through
// the HTTP API. The returned value is safe to pass to store methods.
func ParseRel(rel string) (string, error) { return cleanRel(rel) }

// cleanRel normalizes a silo-relative path and rejects anything that could
// escape the silo directory or touch the private .song metadata tree.
func cleanRel(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "/" {
		return "", nil
	}
	rel = strings.TrimPrefix(rel, "/")
	if rel == "" {
		return "", nil
	}
	if strings.Contains(rel, "\x00") {
		return "", ErrTraversal
	}
	// Backslashes separate path segments on Windows; refusing them here keeps
	// behaviour identical on every platform.
	if strings.Contains(rel, "\\") {
		return "", fmt.Errorf("%w: backslashes are not allowed", ErrTraversal)
	}
	clean := filepath.Clean(filepath.FromSlash(rel))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrTraversal, rel)
	}
	if strings.HasPrefix(clean, "."+string(filepath.Separator)) {
		clean = clean[len("."):] // strip leading ./
		clean = strings.TrimPrefix(clean, string(filepath.Separator))
	}
	if clean == "." {
		return "", nil
	}
	// Reject any path whose raw segments mention .song, even if cleaning
	// would collapse them away (e.g. ".song/../x").
	for _, seg := range strings.Split(rel, "/") {
		if seg == ".song" {
			return "", fmt.Errorf("%w: %q is private", ErrReserved, rel)
		}
	}
	if clean == ".song" || strings.HasPrefix(clean, ".song"+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q is private", ErrReserved, rel)
	}
	return clean, nil
}

// resolve joins base with an already-cleaned relative path and asserts the
// result stays strictly inside base. Both paths are absolute.
func resolve(base, rel string) (string, error) {
	joined := filepath.Join(base, filepath.FromSlash(rel))
	bc := filepath.Clean(base)
	if joined != bc && !strings.HasPrefix(joined, bc+string(filepath.Separator)) {
		return "", fmt.Errorf("%w: %q", ErrTraversal, rel)
	}
	return joined, nil
}
