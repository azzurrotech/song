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

// Meta is the per-silo configuration stored in .song/meta.json.
//
// Version identifies the document schema. TemplateRoutes maps a silo-relative
// request path onto a named Go template in the silo's .templates directory,
// which is the HTTP surface for text/template and html/template processing.
type Meta struct {
	Version        int               `json:"version"`
	TemplateRoutes map[string]string `json:"template_routes,omitempty"`
}

func defaultMeta() *Meta {
	return &Meta{
		Version:        1,
		TemplateRoutes: map[string]string{},
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
