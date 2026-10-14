// Package static implements song's siloed static-file store. Files live under
// <root>/<silo>/ as plain bytes. A hidden .song/meta.json per silo records the
// server-side template routes that map a request path onto a named Go template.
package static

import (
	"errors"
	"fmt"
	"io"
	"mime"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// FileInfo describes a stored file or directory for listings and stats.
type FileInfo struct {
	Name        string    `json:"name"`
	Path        string    `json:"path"`
	Size        int64     `json:"size"`
	ContentType string    `json:"content_type"`
	IsDir       bool      `json:"is_dir"`
	Modified    time.Time `json:"modified"`
}

// FileEntry is a FileInfo plus the (optionally decrypted) content.
type FileEntry struct {
	FileInfo
	Content []byte `json:"-"`
}

// ReaderEntry is a seekable, closable handle on file content prepared for
// serving. It satisfies io.ReadSeekCloser so http.ServeContent can satisfy
// conditional and Range requests.
type ReaderEntry struct {
	Reader  io.ReadSeekCloser
	Size    int64
	Name    string
	ModTime time.Time
}

// Store manages the siloed filesystem tree rooted at <root>.
type Store struct {
	root string
}

// NewStore resolves and creates the store root directory.
func NewStore(root string) (*Store, error) {
	if root == "" {
		root = "./data"
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("resolve store root: %w", err)
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, fmt.Errorf("create store root %q: %w", abs, err)
	}
	return &Store{root: abs}, nil
}

// Root returns the absolute store root directory.
func (s *Store) Root() string { return s.root }

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
	meta := defaultMeta()
	if err := s.saveMeta(name, meta); err != nil {
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
		Modified:    fi.ModTime(),
	}, nil
}

// Read returns a file's metadata and, when withContent is true, its content.
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
		Modified:    fi.ModTime(),
	}}
	if withContent {
		data, err := os.ReadFile(fp)
		if err != nil {
			return nil, fmt.Errorf("read file %q: %w", rel, err)
		}
		entry.Content = data
	}
	return entry, nil
}

// Open prepares a file for serving by returning an open handle on its
// content, seekable so http.ServeContent can satisfy conditional and Range
// requests.
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
	f, err := os.Open(fp)
	if err != nil {
		return nil, fmt.Errorf("open file %q: %w", rel, err)
	}
	return &ReaderEntry{Reader: f, Size: fi.Size(), Name: filepath.Base(fp), ModTime: fi.ModTime()}, nil
}

// writeFile streams content into a silo path, creating parent directories as
// needed.
func (s *Store) writeFile(silo, rel string, r io.Reader) (*FileInfo, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return nil, fmt.Errorf("create parent dirs for %q: %w", rel, err)
	}
	f, err := os.Create(fp)
	if err != nil {
		return nil, fmt.Errorf("create %q: %w", rel, err)
	}
	if _, cerr := io.Copy(f, r); cerr != nil {
		_ = f.Close()
		return nil, cerr
	}
	if ierr := f.Close(); ierr != nil {
		return nil, ierr
	}
	return s.Stat(silo, rel)
}

// Create stores a new file, failing with ErrExists when it is already there.
func (s *Store) Create(silo, rel, content string) (*FileInfo, error) {
	fp, err := s.filePath(silo, rel)
	if err != nil {
		return nil, err
	}
	if _, err := os.Stat(fp); err == nil {
		return nil, fmt.Errorf("%w: %q", ErrExists, rel)
	}
	return s.writeFile(silo, rel, strings.NewReader(content))
}

// Update rewrites an existing file, failing with ErrNotFound when it is not
// stored yet.
func (s *Store) Update(silo, rel, content string) (*FileInfo, error) {
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
	return s.writeFile(silo, rel, strings.NewReader(content))
}

// Delete removes a file.
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
	return nil
}

// Meta returns the current metadata of a silo.
func (s *Store) Meta(silo string) (*Meta, error) { return s.loadMeta(silo) }

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
