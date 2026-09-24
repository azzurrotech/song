package static

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
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
}

func defaultMeta() *Meta {
	return &Meta{
		Version:        1,
		TemplateRoutes: map[string]string{},
		Encrypted:      map[string]bool{},
	}
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
