package static

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Layout of the hidden per-silo metadata tree.
const (
	metaDir  = ".song"
	metaFile = "meta.json"
)

// IndexRule maps a client header to an index document. The first rule whose
// header is present and matches (exact value or regexp pattern) wins.
//
//	Header   header to inspect, e.g. "X-Index-Variant" or "User-Agent"
//	Value    exact header value that triggers the rule
//	Pattern  regexp matched against the header value (used when Value is empty)
//	File     index document to serve inside the requested directory,
//	         e.g. "index.mobile.html"
//	Redirect when true the client is 302-redirected to File instead of the
//	         document being served inline
type IndexRule struct {
	Header   string `json:"header"`
	Value    string `json:"value,omitempty"`
	Pattern  string `json:"pattern,omitempty"`
	File     string `json:"file"`
	Redirect bool   `json:"redirect,omitempty"`
}

// Meta is the per-silo configuration stored in .song/meta.json.
type Meta struct {
	Version        int               `json:"version"`
	IndexRules     []IndexRule       `json:"index_rules,omitempty"`
	TemplateRoutes map[string]string `json:"template_routes,omitempty"`
	Encrypted      map[string]bool   `json:"encrypted,omitempty"`
	// BaseURL is the public base URL where this silo's files are served.
	// If empty, it defaults to "/c/{silo}/" where {silo} is the silo name.
	// This is validated during file uploads to ensure the public URL
	// matches the silo's actual serving location.
	BaseURL string `json:"base_url,omitempty"`
}

func defaultMeta() *Meta {
	return &Meta{
		Version:        1,
		TemplateRoutes: map[string]string{},
		Encrypted:      map[string]bool{},
	}
}

// DefaultBaseURL returns the default base URL for a silo.
// If the store has a publicBase configured, it returns an absolute URL.
// Otherwise, it returns a relative path.
func (s *Store) DefaultBaseURL(silo string) string {
	if s.publicBase != "" {
		return strings.TrimRight(s.publicBase, "/") + "/c/" + silo + "/"
	}
	return "/c/" + silo + "/"
}

// ValidateBaseURL validates that the configured base URL matches the expected
// public URL for the given silo. If the configured URL doesn't match, it updates
// it to the expected value. Returns an error only if the base URL is not set
// and cannot be auto-configured.
func (s *Store) ValidateBaseURL(silo string) error {
	m, err := s.loadMeta(silo)
	if err != nil {
		return fmt.Errorf("load meta: %w", err)
	}
	expected := s.DefaultBaseURL(silo)
	configured := m.BaseURL
	if configured == "" {
		// Auto-set the base URL if not configured
		_, err := s.updateMeta(silo, func(m *Meta) {
			m.BaseURL = expected
		})
		return err
	}
	if configured != expected {
		// Auto-update if mismatched
		_, err := s.updateMeta(silo, func(m *Meta) {
			m.BaseURL = expected
		})
		return err
	}
	return nil
}

// SetBaseURL sets the base URL for a silo. Accepts any valid URL format.
// If the silo's publicBase is configured, the base URL should be an absolute URL
// including the public base. Otherwise, it should be a relative path like "/c/{silo}/".
func (s *Store) SetBaseURL(silo, baseURL string) error {
	// Validate that it's a valid URL format
	if baseURL == "" {
		return fmt.Errorf("base URL cannot be empty")
	}
	// Accept both absolute URLs (with scheme) and relative paths
	if !strings.HasPrefix(baseURL, "http://") && !strings.HasPrefix(baseURL, "https://") && !strings.HasPrefix(baseURL, "/") {
		return fmt.Errorf("base URL must be an absolute URL (http:// or https://) or a relative path starting with /")
	}
	_, err := s.updateMeta(silo, func(m *Meta) {
		m.BaseURL = baseURL
	})
	return err
}

// metaPath returns the absolute path of the silo metadata file.
func (s *Store) metaPath(silo string) (string, error) {
	dir, err := s.siloDir(silo)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, metaDir, metaFile), nil
}

func (s *Store) loadMeta(silo string) (*Meta, error) {
	mp, err := s.metaPath(silo)
	if err != nil {
		return nil, err
	}
	b, err := os.ReadFile(mp)
	if err != nil {
		if os.IsNotExist(err) {
			return defaultMeta(), nil
		}
		return nil, fmt.Errorf("read meta for silo %q: %w", silo, err)
	}
	m := &Meta{}
	if err := json.Unmarshal(b, m); err != nil {
		return nil, fmt.Errorf("parse meta for silo %q: %w", silo, err)
	}
	if m.TemplateRoutes == nil {
		m.TemplateRoutes = map[string]string{}
	}
	if m.Encrypted == nil {
		m.Encrypted = map[string]bool{}
	}
	if m.Version == 0 {
		m.Version = 1
	}
	return m, nil
}

func (s *Store) saveMeta(silo string, m *Meta) error {
	mp, err := s.metaPath(silo)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(mp), 0o755); err != nil {
		return fmt.Errorf("create meta dir for silo %q: %w", silo, err)
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return fmt.Errorf("encode meta for silo %q: %w", silo, err)
	}
	if err := os.WriteFile(mp, b, 0o644); err != nil {
		return fmt.Errorf("write meta for silo %q: %w", silo, err)
	}
	return nil
}

// updateMeta loads, mutates and persists the silo metadata atomically
// enough for the single-writer model of this store.
func (s *Store) updateMeta(silo string, fn func(*Meta)) (*Meta, error) {
	m, err := s.loadMeta(silo)
	if err != nil {
		return nil, err
	}
	fn(m)
	if err := s.saveMeta(silo, m); err != nil {
		return nil, err
	}
	return m, nil
}
