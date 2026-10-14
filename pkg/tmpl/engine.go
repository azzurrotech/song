// Package tmpl implements server-side rendering for song on top of Go's two
// standard templating packages:
//
//   - html/template (the default) for HTML documents, with contextual
//     auto-escaping of HTML, attributes, JavaScript, CSS and URLs;
//   - text/template for non-HTML text, selected by file extension
//     (.txt, .text, .md, .markdown).
//
// Every silo may carry a .templates/ directory; all non-hidden regular files
// in it are parsed into one template set per format, so templates compose
// freely with {{define}}, {{template}}, {{block}}, variables, pipelines,
// whitespace trimming, delimiters and template.FuncMap helpers.
//
// System-defined HTML template tags (the server side of song's Web Components)
// are registered once with AddComponent and are then parsed into every silo's
// HTML set, so any HTML file — a .templates document, an SSR route or a plain
// served .html file processed with RenderInline — can instantiate them with
// {{template "tag-name" .}}.
package tmpl

import (
	"errors"
	"fmt"
	htmltemplate "html/template"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	texttemplate "text/template"
	"time"
)

// ErrNoTemplates is returned when a silo has no .templates directory (or it
// contains no regular files).
var ErrNoTemplates = errors.New("no templates found for silo")

// Format identifies which standard templating package parses and executes a
// template.
type Format int

const (
	// FormatHTML selects html/template, which performs contextual
	// auto-escaping for HTML, attribute, script, style and URL contexts.
	FormatHTML Format = iota
	// FormatText selects text/template, which performs no escaping and is
	// used for non-HTML resources such as plain text or Markdown.
	FormatText
)

// String renders the format name for logs and diagnostics.
func (f Format) String() string {
	if f == FormatText {
		return "text"
	}
	return "html"
}

// textExtensions maps the file extensions that are parsed with text/template.
// Every other extension (in particular .html, .htm and .tmpl) is parsed with
// html/template so existing documents keep their contextual escaping.
var textExtensions = map[string]bool{
	".txt":      true,
	".text":     true,
	".md":       true,
	".markdown": true,
}

// FormatOf reports the templating format implied by a template file name.
func FormatOf(name string) Format {
	if textExtensions[strings.ToLower(filepath.Ext(name))] {
		return FormatText
	}
	return FormatHTML
}

// componentSource is a system-defined HTML template tag compiled into every
// silo's HTML set. source is a complete Go template (usually a single
// {{define "name"}}…{{end}} block).
type componentSource struct {
	name   string
	source string
}

// Engine parses, caches and executes per-silo Go template sets in both HTML
// and text formats.
type Engine struct {
	root       string
	mu         sync.RWMutex
	custom     htmltemplate.FuncMap
	delims     [2]string
	cache      map[string]*templateSet
	components []componentSource
}

// templateSet is a cached parse together with the file mod times it was built
// from, so the engine can reload transparently on change.
type templateSet struct {
	files map[string]time.Time
	html  *htmltemplate.Template
	text  *texttemplate.Template
}

// New creates an Engine rooted at root (the same directory that holds the
// silos: templates live in <root>/<silo>/.templates).
func New(root string) *Engine {
	if root == "" {
		root = "."
	}
	return &Engine{
		root:   root,
		custom: htmltemplate.FuncMap{},
		cache:  map[string]*templateSet{},
	}
}

// SetDelims customizes the template delimiters for both the HTML and the text
// sets. Calling it drops the cache.
func (e *Engine) SetDelims(left, right string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.delims = [2]string{left, right}
	e.cache = map[string]*templateSet{}
}

// AddFuncs merges host-supplied functions into the engine. Custom functions
// override the built-in set of the same name. Drops the cache.
func (e *Engine) AddFuncs(fm htmltemplate.FuncMap) {
	e.mu.Lock()
	defer e.mu.Unlock()
	for k, v := range fm {
		e.custom[k] = v
	}
	e.cache = map[string]*templateSet{}
}

// SetFuncs replaces the host-supplied function map wholesale. Drops the cache.
func (e *Engine) SetFuncs(fm htmltemplate.FuncMap) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.custom = fm
	if e.custom == nil {
		e.custom = htmltemplate.FuncMap{}
	}
	e.cache = map[string]*templateSet{}
}

// AddComponent registers a system-defined HTML template tag so it becomes
// available from every silo's HTML template set (and from RenderInline).
//
// source is a Go html/template document. When it does not already contain a
// {{define}} block it is wrapped in {{define "name"}}…{{end}}, so callers may
// register a bare fragment:
//
//	engine.AddComponent("song-badge", `<span class="badge">{{.}}</span>`)
//
// Registering an existing name replaces it. Registration drops the cache so
// the next render picks up the change. An invalid template is rejected with an
// error and nothing is registered.
func (e *Engine) AddComponent(name, source string) error {
	if strings.TrimSpace(name) == "" {
		return errors.New("tmpl: component name is required")
	}
	if strings.TrimSpace(source) == "" {
		return fmt.Errorf("tmpl: component %q has no source", name)
	}
	if !strings.Contains(source, "{{define") {
		source = `{{define "` + name + `"}}` + source + `{{end}}`
	}
	// Validate eagerly so a broken component fails at registration, not at
	// first request.
	if _, err := htmltemplate.New("component").Parse(source); err != nil {
		return fmt.Errorf("tmpl: parse component %q: %w", name, err)
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	replaced := false
	for i := range e.components {
		if e.components[i].name == name {
			e.components[i].source = source
			replaced = true
			break
		}
	}
	if !replaced {
		e.components = append(e.components, componentSource{name: name, source: source})
	}
	e.cache = map[string]*templateSet{}
	return nil
}

// Components returns the names of the registered system-defined HTML template
// tags in registration order.
func (e *Engine) Components() []string {
	e.mu.RLock()
	defer e.mu.RUnlock()
	names := make([]string, 0, len(e.components))
	for _, c := range e.components {
		names = append(names, c.name)
	}
	return names
}

// snapshot copies the mutable configuration so parsing happens without holding
// the lock.
func (e *Engine) snapshot() (htmltemplate.FuncMap, [2]string, []componentSource) {
	e.mu.RLock()
	defer e.mu.RUnlock()
	custom := htmltemplate.FuncMap{}
	for k, v := range e.custom {
		custom[k] = v
	}
	return custom, e.delims, append([]componentSource(nil), e.components...)
}

// buildFuncs merges the built-in function set with host overrides.
func buildFuncs(custom htmltemplate.FuncMap) htmltemplate.FuncMap {
	fm := htmltemplate.FuncMap{}
	for k, v := range defaultFuncs() {
		fm[k] = v
	}
	for k, v := range custom {
		fm[k] = v
	}
	return fm
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

// load parses (or reuses) the template set for a silo. HTML and text files are
// partitioned by FormatOf and parsed into two associated sets; both live under
// the same cache entry so a single modification-time comparison drives reload.
func (e *Engine) load(silo string) (*templateSet, error) {
	dir := filepath.Join(e.root, silo, ".templates")
	entries, err := os.ReadDir(dir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNoTemplates
		}
		return nil, fmt.Errorf("read templates dir for silo %q: %w", silo, err)
	}
	files := map[string]time.Time{}
	var htmlPaths, textPaths []string
	for _, en := range entries {
		if en.IsDir() || strings.HasPrefix(en.Name(), ".") {
			continue
		}
		info, err := en.Info()
		if err != nil {
			continue
		}
		files[en.Name()] = info.ModTime()
		p := filepath.Join(dir, en.Name())
		if FormatOf(en.Name()) == FormatText {
			textPaths = append(textPaths, p)
		} else {
			htmlPaths = append(htmlPaths, p)
		}
	}

	custom, delims, components := e.snapshot()

	if len(htmlPaths) == 0 && len(textPaths) == 0 && len(components) == 0 {
		return nil, ErrNoTemplates
	}
	sort.Strings(htmlPaths)
	sort.Strings(textPaths)

	e.mu.RLock()
	if c, ok := e.cache[silo]; ok && sameFiles(c.files, files) {
		e.mu.RUnlock()
		return c, nil
	}
	e.mu.RUnlock()

	fm := buildFuncs(custom)

	// HTML set: components first (so their defines exist), then silo files.
	var htmlSet *htmltemplate.Template
	if len(htmlPaths) > 0 || len(components) > 0 {
		htmlSet = htmltemplate.New(silo).Funcs(fm)
		if delims[0] != "" || delims[1] != "" {
			htmlSet = htmlSet.Delims(delims[0], delims[1])
		}
		for _, c := range components {
			if _, err := htmlSet.New(c.name).Parse(c.source); err != nil {
				return nil, fmt.Errorf("parse component %s: %w", c.name, err)
			}
		}
		for _, p := range htmlPaths {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("read template %s: %w", p, err)
			}
			name := filepath.Base(p)
			if _, err := htmlSet.New(name).Parse(string(b)); err != nil {
				return nil, fmt.Errorf("parse template %s: %w", name, err)
			}
		}
	}

	// Text set: same function map, no contextual escaping.
	var textSet *texttemplate.Template
	if len(textPaths) > 0 {
		tfm := texttemplate.FuncMap{}
		for k, v := range fm {
			tfm[k] = v
		}
		textSet = texttemplate.New(silo).Funcs(tfm)
		if delims[0] != "" || delims[1] != "" {
			textSet = textSet.Delims(delims[0], delims[1])
		}
		for _, p := range textPaths {
			b, err := os.ReadFile(p)
			if err != nil {
				return nil, fmt.Errorf("read template %s: %w", p, err)
			}
			name := filepath.Base(p)
			if _, err := textSet.New(name).Parse(string(b)); err != nil {
				return nil, fmt.Errorf("parse template %s: %w", name, err)
			}
		}
	}

	set := &templateSet{files: files, html: htmlSet, text: textSet}
	e.mu.Lock()
	e.cache[silo] = set
	e.mu.Unlock()
	return set, nil
}

// TemplateNames lists every parsable template name (file base names plus
// {{define}} blocks) across both the HTML and text sets, in sorted order.
func (e *Engine) TemplateNames(silo string) ([]string, error) {
	set, err := e.load(silo)
	if err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	if set.html != nil {
		for _, tt := range set.html.Templates() {
			if tt.Name() == silo { // root sentinel template
				continue
			}
			seen[tt.Name()] = true
		}
	}
	if set.text != nil {
		for _, tt := range set.text.Templates() {
			if tt.Name() == silo { // root sentinel template
				continue
			}
			seen[tt.Name()] = true
		}
	}
	names := make([]string, 0, len(seen))
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

// IsText reports whether the named template is served from the text set (and
// therefore must be sent without HTML contextual escaping).
func (e *Engine) IsText(silo, name string) bool {
	set, err := e.load(silo)
	if err != nil {
		return false
	}
	if set.html != nil && set.html.Lookup(name) != nil {
		return false
	}
	return set.text != nil && set.text.Lookup(name) != nil
}

// Render executes the named template of a silo with data. An empty name
// defaults to "index". HTML names are rendered with html/template (contextual
// auto-escaping, honoring template.HTML/JS/CSS/URL typed values); names that
// exist only in the text set are rendered with text/template.
func (e *Engine) Render(w io.Writer, silo, name string, data any) error {
	set, err := e.load(silo)
	if err != nil {
		return err
	}
	if name == "" {
		name = "index"
	}
	if set.html != nil && set.html.Lookup(name) != nil {
		return set.html.ExecuteTemplate(w, name, data)
	}
	if set.text != nil && set.text.Lookup(name) != nil {
		return set.text.ExecuteTemplate(w, name, data)
	}
	return fmt.Errorf("template %q is not defined in silo %q", name, silo)
}

// RenderText executes the named template of a silo with text/template, forcing
// non-HTML rendering. It errors when the name is not defined in the text set.
func (e *Engine) RenderText(w io.Writer, silo, name string, data any) error {
	set, err := e.load(silo)
	if err != nil {
		return err
	}
	if set.text == nil || set.text.Lookup(name) == nil {
		return fmt.Errorf("text template %q is not defined in silo %q", name, silo)
	}
	return set.text.ExecuteTemplate(w, name, data)
}

// RenderInline parses source as a single HTML template on top of the silo's
// function map and registered component tags, then executes it with data and
// returns the rendered document. It is used to process plain .html files on
// the fly while making every system-defined template tag available to them.
// A malformed source is reported as an error so callers can fall back to
// serving the file verbatim.
func (e *Engine) RenderInline(silo, source string, data any) (string, error) {
	custom, delims, components := e.snapshot()
	t := htmltemplate.New("inline").Funcs(buildFuncs(custom))
	if delims[0] != "" || delims[1] != "" {
		t = t.Delims(delims[0], delims[1])
	}
	for _, c := range components {
		if _, err := t.New(c.name).Parse(c.source); err != nil {
			return "", fmt.Errorf("parse component %s: %w", c.name, err)
		}
	}
	t, err := t.Parse(source)
	if err != nil {
		return "", fmt.Errorf("parse inline template: %w", err)
	}
	var buf strings.Builder
	if err := t.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute inline template: %w", err)
	}
	return buf.String(), nil
}
