// Package static implements song's siloed static-file store. Files live
// under <root>/<silo>/ and may be stored encrypted at rest (AES-256-GCM via
// the shared encryption manager). A hidden .song/meta.json per silo records
// encryption state, index-routing rules and server-side template routes.
package static

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"azzurrotech/song/internal/encryption"
)

// FileInfo describes a stored file or directory for listings and stats.
type FileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	IsDir       bool      `json:"is_dir"`
	Encrypted   bool      `json:"encrypted,omitempty"`
	Modified    time.Time `json:"modified"`
}

// FileEntry is a FileInfo plus the (optionally decrypted) content.
type FileEntry struct {
	FileInfo
	Content []byte `json:"-"`
}

// ReaderEntry is a seekable, closable handle on file content prepared for
// serving (encrypted files are transparently decrypted into memory).
type ReaderEntry struct {
	Reader    io.ReadSeekCloser
	Size      int64
	Encrypted bool
	Name      string
	ModTime   time.Time
}

// readSeekCloser adapts a *bytes.Reader into an io.ReadSeekCloser.
type readSeekCloser struct{ *bytes.Reader }

func (readSeekCloser) Close() error { return nil }

// Store manages the siloed filesystem tree rooted at <root>.
type Store struct {
	root string
	em   *encryption.EncryptionManager
}

// NewStore validates the secret, resolves and creates the root directory.
func NewStore(root, secret string) (*Store, error) {
	if root == "" {
		root = "./data"
	}
	em, err := encryption.NewEncryptionManager(secret)
	if err != nil {
		return nil, err
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create store root %q: %w", abs, err)
	}
	return &Store{root: abs, em: em}, nil
}

// Root returns the absolute store root directory.
func (s *Store) Root() string { return s.root }

// Encryption exposes the shared encryption manager.
func (s *Store) Encryption() *encryption.EncryptionManager { return s.em }

// siloDir returns the absolute, validated directory of a silo.
func (s *Store) siloDir(name string) (string, error) {
	if err := validateSiloName(name); err != nil {
		return "", err
	}
	return filepath.Join(s.root, name), nil
}

// HasSilo reports whether the silo exists as a directory.
func (s *Store) HasSilo(name string) bool {
	dir, err := s.siloDir(name)
	if err != nil {
		return false
	}
	fi, err := os.Stat(dir)
	return err == nil && fi.IsDir()
}

// ListSilos returns the sorted names of existing silos.
func (s *Store) ListSilos() ([]string, error) {
	entries, err := os.ReadDir(s.root)
	if err != nil {
		return nil, fmt.Errorf("list store root: %w", err)
	}
	var names []string
	for _, e := range entries {
		if !e.IsDir() || strings.HasPrefix(e.Name(), ".") {
			continue
		}
		if err := validateSiloName(e.Name()); err != nil {
			continue
		}
		names = append(names, e.Name())
	}
	sort.Strings(names)
	return names, nil
}

// CreateSilo makes a new silo directory with an initialized metadata tree.
func (s *Store) CreateSilo(name string) error {
	dir, err := s.siloDir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("%w: %q", ErrExists, name)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create silo %q: %w", name, err)
	}
	if err := s.saveMeta(name, defaultMeta()); err != nil {
		return err
	}
	return nil
}

// RemoveSilo deletes a silo and everything inside it.
func (s *Store) RemoveSilo(name string) error {
	dir, err := s.siloDir(name)
	if err != nil {
		return err
	}
	if _, err := os.Stat(dir); err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: silo %q", ErrNotFound, name)
		}
		return err
	}
	return os.RemoveAll(dir)
}

// filePath resolves silo + rel to an absolute path guaranteed to stay inside
// the silo directory.
func (s *Store) filePath(silo, rel string) (string, error) {
	dir, err := s.siloDir(silo)
	if err != nil {
		return "", err
	}
	rel, err = cleanRel(rel)
	if err != nil {
		return "", err
	}
	return resolve(dir, rel)
}

func (s *Store) isEncrypted(silo, rel string) bool {
	m, err := s.loadMeta(silo)
	if err != nil {
		return false
	}
	return m.Encrypted[rel]
}

// List returns entries of the silo directory at rel, excluding hidden files
// (dot-prefixed) such as the .song metadata tree.
func (s *Store) List(silo, rel string) ([]FileInfo, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, rel)
		}
		return nil, err
	}
	if !fi.IsDir() {
		return nil, fmt.Errorf("%w: %q", ErrIsFile, rel)
	}
	entries, err := os.ReadDir(fp)
	if err != nil {
		return nil, fmt.Errorf("read directory %q: %w", rel, err)
	}
	files := make([]FileInfo, 0, len(entries))
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		child, err := cleanRel(rel + "/" + e.Name())
		if err != nil {
			continue
		}
		files = append(files, FileInfo{
			Name:        e.Name(),
			Path:        "/" + child,
			Size:        info.Size(),
			ContentType: contentTypeFor(e.Name(), info.IsDir()),
			IsDir:       info.IsDir(),
			Encrypted:   !info.IsDir() && s.isEncrypted(silo, child),
			Modified:    info.ModTime(),
		})
	}
	sort.Slice(files, func(i, j int) bool {
		if files[i].IsDir != files[j].IsDir {
			return files[i].IsDir
		}
		return files[i].Name < files[j].Name
	})
	return files, nil
}

// Stat returns metadata for a single path inside a silo.
func (s *Store) Stat(silo, rel string) (*FileInfo, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, rel)
		}
		return nil, err
	}
	return &FileInfo{
		Name:        filepath.Base(fp),
		Path:        "/" + rel,
		Size:        fi.Size(),
		ContentType: contentTypeFor(rel, fi.IsDir()),
		IsDir:       fi.IsDir(),
		Encrypted:   !fi.IsDir() && s.isEncrypted(silo, rel),
		Modified:    fi.ModTime(),
	}, nil
}

// Read returns a file's metadata and, when withContent is true, its
// (decrypted) content.
func (s *Store) Read(silo, rel string, withContent bool) (*FileEntry, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, rel)
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%w: %q", ErrIsDir, rel)
	}
	entry := &FileEntry{FileInfo: FileInfo{
		Name:        filepath.Base(fp),
		Path:        "/" + rel,
		Size:        fi.Size(),
		ContentType: contentTypeFor(rel, false),
		IsDir:       false,
		Encrypted:   s.isEncrypted(silo, rel),
		Modified:    fi.ModTime(),
	}}
	if withContent {
		data, err := os.ReadFile(fp)
		if err != nil {
			return nil, fmt.Errorf("read file %q: %w", rel, err)
		}
		if entry.Encrypted {
			data, err = s.em.DecryptBytes(data)
			if err != nil {
				return nil, fmt.Errorf("decrypt file %q: %w", rel, err)
			}
		}
		entry.Content = data
	}
	return entry, nil
}

// Open prepares a file for serving: plain files yield an open *os.File,
// encrypted files are decrypted into memory and returned as a seeking
// reader so http.ServeContent can still serve ranges.
func (s *Store) Open(silo, rel string) (*ReaderEntry, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	fi, err := os.Stat(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, rel)
		}
		return nil, err
	}
	if fi.IsDir() {
		return nil, fmt.Errorf("%w: %q", ErrIsDir, rel)
	}
	name := filepath.Base(fp)
	if !s.isEncrypted(silo, rel) {
		f, err := os.Open(fp)
		if err != nil {
			return nil, fmt.Errorf("open file %q: %w", rel, err)
		}
		return &ReaderEntry{Reader: f, Size: fi.Size(), Encrypted: false, Name: name, ModTime: fi.ModTime()}, nil
	}
	data, err := os.ReadFile(fp)
	if err != nil {
		return nil, fmt.Errorf("read encrypted file %q: %w", rel, err)
	}
	plain, err := s.em.DecryptBytes(data)
	if err != nil {
		return nil, fmt.Errorf("decrypt file %q: %w", rel, err)
	}
	return &ReaderEntry{
		Reader:    &readSeekCloser{bytes.NewReader(plain)},
		Size:      int64(len(plain)),
		Encrypted: true,
		Name:      name,
		ModTime:   fi.ModTime(),
	}, nil
}

// writeFile streams content into a silo path, encrypting it when requested
// and keeping the .song/meta.json encryption map consistent.
func (s *Store) writeFile(silo, rel string, r io.Reader, encrypt bool) (*FileInfo, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return nil, fmt.Errorf("create parent dirs for %q: %w", rel, err)
	}
	if encrypt {
		plain, err := io.ReadAll(r)
		if err != nil {
			return nil, fmt.Errorf("read content for %q: %w", rel, err)
		}
		ct, err := s.em.EncryptBytes(plain)
		if err != nil {
			return nil, fmt.Errorf("encrypt %q: %w", rel, err)
		}
		if err := os.WriteFile(fp, ct, 0o644); err != nil {
			return nil, fmt.Errorf("write %q: %w", rel, err)
		}
		if _, err := s.updateMeta(silo, func(m *Meta) { m.Encrypted[rel] = true }); err != nil {
			return nil, err
		}
	} else {
		f, err := os.Create(fp)
		if err != nil {
			return nil, fmt.Errorf("create %q: %w", rel, err)
		}
		_, cerr := io.Copy(f, r)
		ierr := f.Close()
		if cerr != nil {
			return nil, cerr
		}
		if ierr != nil {
			return nil, ierr
		}
		if _, err := s.updateMeta(silo, func(m *Meta) { delete(m.Encrypted, rel) }); err != nil {
			return nil, err
		}
	}
	return s.Stat(silo, rel)
}

// Create stores a new file, failing with ErrExists when it is already there.
func (s *Store) Create(silo, rel, content string, encrypt bool) (*FileInfo, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(fp); err == nil {
		return nil, fmt.Errorf("%w: %q", ErrExists, rel)
	}
	return s.writeFile(silo, rel, strings.NewReader(content), encrypt)
}

// Update rewrites an existing file. A nil encrypt keeps the current
// encryption state; otherwise the file is re-stored accordingly.
func (s *Store) Update(silo, rel, content string, encrypt *bool) (*FileInfo, error) {
	cur, err := s.Stat(silo, rel)
	if err != nil {
		if errors.Is(err, ErrNotFound) {
			return nil, fmt.Errorf("%w: %q", ErrNotFound, rel)
		}
		return nil, err
	}
	if cur.IsDir {
		return nil, fmt.Errorf("%w: %q", ErrIsDir, rel)
	}
	enc := cur.Encrypted
	if encrypt != nil {
		enc = *encrypt
	}
	return s.writeFile(silo, rel, strings.NewReader(content), enc)
}

// Upload streams a reader into a silo path (create or overwrite).
func (s *Store) Upload(silo, rel string, r io.Reader, encrypt bool) (*FileInfo, error) {
	return s.writeFile(silo, rel, r, encrypt)
}

// Delete removes a file and its encryption flag.
func (s *Store) Delete(silo, rel string) error {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return err
	}
	fi, err := os.Stat(fp)
	if err != nil {
		if os.IsNotExist(err) {
			return fmt.Errorf("%w: %q", ErrNotFound, rel)
		}
		return err
	}
	if fi.IsDir() {
		return fmt.Errorf("%w: %q", ErrIsDir, rel)
	}
	if err := os.Remove(fp); err != nil {
		return fmt.Errorf("remove %q: %w", rel, err)
	}
	_, err = s.updateMeta(silo, func(m *Meta) { delete(m.Encrypted, rel) })
	return err
}

// Meta returns the current metadata of a silo.
func (s *Store) Meta(silo string) (*Meta, error) { return s.loadMeta(silo) }

// SaveMeta replaces a silo's metadata wholesale.
func (s *Store) SaveMeta(silo string, m *Meta) error {
	if m == nil {
		m = defaultMeta()
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
	return s.saveMeta(silo, m)
}

// SetTemplateRoute registers (or, with tpl == "", removes) a route that
// server-side renders the named Go template instead of serving a file.
func (s *Store) SetTemplateRoute(silo, route, tpl string) error {
	_, err := s.updateMeta(silo, func(m *Meta) {
		if tpl == "" {
			delete(m.TemplateRoutes, route)
		} else {
			m.TemplateRoutes[route] = tpl
		}
	})
	return err
}

// TemplateDir returns the absolute templates directory of a silo.
func (s *Store) TemplateDir(silo string) (string, error) {
	dir, err := s.siloDir(silo)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, ".templates"), nil
}

// fileExists reports whether a cleaned silo-relative path names a file.
func (s *Store) fileExists(silo, rel string) bool {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return false
	}
	fi, err := os.Stat(fp)
	return err == nil && !fi.IsDir()
}

// contentTypeFor maps a file name to a sane web content type.
func contentTypeFor(name string, isDir bool) string {
	if isDir {
		return "inode/directory"
	}
	switch strings.ToLower(filepath.Ext(name)) {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".xml", ".xsl":
		return "application/xml"
	case ".txt", ".md":
		if strings.ToLower(filepath.Ext(name)) == ".txt" {
			return "text/plain; charset=utf-8"
		}
		return "text/markdown; charset=utf-8"
	case ".svg":
		return "image/svg+xml"
	case ".webmanifest":
		return "application/manifest+json"
	case ".wasm":
		return "application/wasm"
	case ".woff":
		return "font/woff"
	case ".woff2":
		return "font/woff2"
	}
	if ct := mime.TypeByExtension(strings.ToLower(filepath.Ext(name))); ct != "" {
		return ct
	}
	return "application/octet-stream"
}
