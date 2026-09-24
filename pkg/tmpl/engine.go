// Package tmpl implements server-side rendering for song on top of the
// complete html/template surface. Every silo may carry a .templates/
// directory; all files in it are parsed as one template set, so templates
// can compose freely with {{define}}, {{template}}, {{block}}, variables,
// pipelines, whitespace trimming, delimiters and template.FuncMap helpers.
package tmpl

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// ErrNoTemplates is returned when a silo has no .templates directory (or it
// contains no regular files).
var ErrNoTemplates = errors.New("no templates found for silo")

// Engine parses, caches and executes per-silo Go template sets.
type Engine struct {
	root   string
	mu     sync.RWMutex
	custom template.FuncMap
	delims [2]string
	cache  map[string]*templateSet
}

// templateSet is a cached parse together with the file mod times it was
// built from, so the engine can reload transparently on change.
type templateSet struct {
	files map[string]time.Time
	t     *template.Template
}

// New creates an Engine rooted at root (the same directory that holds the
// silos: templates live in <root>/<silo>/.templates).
func New(root string) *Engine {
	if root == "" {
		root = "."
	}
	return &Engine{
		root:   root,
		custom: template.FuncMap{},
		cache:  map[string]*templateSet{},
	}
}

// Root returns the template store root.
func (e *Engine) Root() string { return e.root }

// SetDelims customizes the template delimiters. Calling it drops the cache.
func (e *Engine) SetDelims(left, right string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delims = [2]string{left, right}
	e.cache = map[string]*templateSet{}
}

// AddFuncs merges host-supplied functions into the engine. Custom functions
// override the built-in set of the same name. Drops the cache.
func (e *Engine) AddFuncs(fm template.FuncMap) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, v := range fm {
		e.custom[k] = v
	}
	e.cache = map[string]*templateSet{}
}

// SetFuncs replaces the host-supplied function map wholesale. Drops the cache.
func (e *Engine) SetFuncs(fm template.FuncMap) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.custom = fm
	if e.custom == nil {
		e.custom = template.FuncMap{}
	}
	e.cache = map[string]*templateSet{}
}

func sameFiles(a, b map[string]time.Time) bool {
	if len(a) != len(b) {
		return false
	}
	for k, v := range a {
		if bv, ok := b[k]; !ok || !bv.Equal(v) {
			return false
		}
	}
	return true
}

// load parses (or reuses) the template set for a silo.
func (e *Engine) load(silo string) (*template.Template, error) {
	dir := filepath.Join(e.root, silo, ".templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoTemplates
		}
		return nil, fmt.Errorf("read templates dir for silo %q: %w", silo, err)
	}
	files := map[string]time.Time{}
	var paths []string
	for _, en := range entries {
		if en.IsDir() || strings.HasPrefix(en.Name(), ".") {
			continue
		}
		info, err := en.Info()
		if err != nil {
			continue
		}
		files[en.Name()] = info.ModTime()
		paths = append(paths, filepath.Join(dir, en.Name()))
	}
	if len(paths) == 0 {
		return nil, ErrNoTemplates
	}
	sort.Strings(paths)

	e.mu.RLock()
	if c, ok := e.cache[silo]; ok && sameFiles(c.files, files) {
		t := c.t
		e.mu.RUnlock()
		return t, nil
	}
	e.mu.RUnlock()

	// Combine the run-time function map: built-ins first, custom overrides.
	fm := template.FuncMap{}
	for k, v := range defaultFuncs() {
		fm[k] = v
	}
	e.mu.RLock()
	for k, v := range e.custom {
		fm[k] = v
	}
	delims := e.delims
	e.mu.RUnlock()

	t := template.New(silo)
	t = t.Funcs(fm)
	if delims[0] != "" || delims[1] != "" {
		t = t.Delims(delims[0], delims[1])
	}
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if err != nil {
			return nil, fmt.Errorf("read template %s: %w", p, err)
		}
		name := filepath.Base(p)
		if _, err := t.New(name).Parse(string(b)); err != nil {
			return nil, fmt.Errorf("parse template %s: %w", name, err)
		}
	}

	e.mu.Lock()
	e.cache[silo] = &templateSet{files: files, t: t}
	e.mu.Unlock()
	return t, nil
}

// Template returns the cached (or freshly parsed) *template.Template for a
// silo, giving hosts direct access to the parsed set if they need to add
// parse trees, clone or execute templates themselves.
func (e *Engine) Template(silo string) (*template.Template, error) {
	return e.load(silo)
}

// TemplateNames lists every parsable template name (file base names plus
// {{define}} blocks) for a silo, in sorted order.
func (e *Engine) TemplateNames(silo string) ([]string, error) {
	t, err := e.load(silo)
	if err != nil {
		return nil, err
	}
	var names []string
	for _, tt := range t.Templates() {
		if tt.Name() == silo { // root sentinel template
			continue
		}
		names = append(names, tt.Name())
	}
	sort.Strings(names)
	return names, nil
}

// Render executes the named template of a silo with data. An empty name
// defaults to "index". Data can be any Go value; html/template performs
// contextual auto-escaping and honors template.HTML/JS/CSS/URL typed values.
func (e *Engine) Render(w io.Writer, silo, name string, data any) error {
	t, err := e.load(silo)
	if err != nil {
		return err
	}
	if name == "" {
		name = "index"
	}
	if t.Lookup(name) == nil {
		return fmt.Errorf("template %q is not defined in silo %q", name, silo)
	}
	return t.ExecuteTemplate(w, name, data)
}
