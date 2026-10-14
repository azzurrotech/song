// Package song is a Go middleware for serving static web resources with
// server-side template processing and Web Component support, built entirely on
// the Go standard library.
//
// It runs in two modes:
//
//   - Server mode: a standalone Go webserver. See the main package and the
//     Handler() method.
//   - Middleware mode: song.Handler is an http.Handler that can be mounted
//     into any other Go server, and song.Middleware(next) serves only the
//     routes song owns and delegates everything else to next.
//
// Features, as listed in the README:
//
//   - Standard library only: no third-party server dependencies.
//   - Middleware design: Middleware(next) serves only what song owns and
//     delegates all other paths to next, so it drops into an existing Go HTTP
//     server without fighting it.
//   - Static resource access: GET /{silo}/{path} serves a stored resource.
//   - Resource upsertion: POST /{silo}/{path} creates or replaces a resource
//     with the request body as its content.
//   - Template processing: HTML documents are processed with html/template
//     (contextual auto-escaping) and non-HTML templates with text/template,
//     with a batteries-included FuncMap and host-injected functions.
//   - Web Components: system-defined HTML template tags are available from any
//     served HTML file (client-side custom elements plus an injected baseline
//     runtime) and from every server-side HTML template. See pkg/component.
package song

import (
	"fmt"
	"html/template"
	"net/http"
	"strings"

	"azzurrotech/song/pkg/component"
	"azzurrotech/song/pkg/static"
	"azzurrotech/song/pkg/tmpl"
)

// Version of the song server/library.
const Version = "2.0.0"

// Config controls a Song instance.
type Config struct {
	Root  string           // store root directory (default "./data")
	Funcs template.FuncMap // optional extra server-side template functions
	// Components are extra system-defined Web Components (custom elements)
	// registered in addition to the built-ins. Every registered tag is
	// available from any served HTML file and from every server-side HTML
	// template. See pkg/component.
	Components []component.Tag
}

// Song ties the static store, the template engine and the Web Component
// registry into one HTTP surface usable as a server or as middleware.
type Song struct {
	store      *static.Store
	engine     *tmpl.Engine
	components *component.Registry
}

// New builds a Song, creating the store root when needed.
func New(cfg Config) (*Song, error) {
	store, err := static.NewStore(cfg.Root)
	if err != nil {
		return nil, err
	}
	engine := tmpl.New(store.Root())
	if cfg.Funcs != nil {
		engine.SetFuncs(cfg.Funcs)
	}

	// System-defined Web Components: built-ins first, then host tags. Each
	// tag is registered both in the HTTP-facing registry (client-side custom
	// elements) and in the template engine (server-side {{template}} tags).
	registry := component.DefaultRegistry()
	for _, tag := range cfg.Components {
		if err := registry.Register(tag); err != nil {
			return nil, err
		}
	}
	for _, tag := range registry.Tags() {
		if err := engine.AddComponent(tag.Name, tag.ServerTemplate()); err != nil {
			return nil, fmt.Errorf("register component %q: %w", tag.Name, err)
		}
	}

	return &Song{store: store, engine: engine, components: registry}, nil
}

// Store exposes the underlying siloed file store.
func (s *Song) Store() *static.Store { return s.store }

// Components exposes the system-defined Web Components registry.
func (s *Song) Components() *component.Registry { return s.components }

// Handler returns a complete http.Handler for server mode: silo serving and
// the Web Component runtime. Paths song does not own fall through to
// http.NotFound.
func (s *Song) Handler() http.Handler {
	return s.Middleware(http.NotFoundHandler())
}

// Middleware returns an http.Handler that serves the routes song owns and
// delegates everything else to next. This is how another Go server opts into
// song: srv.Middleware(myAppHandler). A request path that names a registered
// silo (or the component runtime) is handled by song; everything else is
// passed to next untouched.
func (s *Song) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case strings.HasPrefix(p, component.RuntimePathPrefix):
			s.handleComponents(w, r)
		default:
			silo, rest, isSilo := splitSiloPath(p)
			if isSilo && s.store.HasSilo(silo) {
				s.handleSilo(w, r, silo, rest)
				return
			}
			next.ServeHTTP(w, r)
		}
	})
}

// splitSiloPath splits "/name/rest" into ("name", "rest") where rest has no
// leading or trailing slash and "/name" alone yields ("name", ""). Reserved
// segments are never silos.
func splitSiloPath(p string) (name, rest string, ok bool) {
	p = strings.TrimPrefix(p, "/")
	if p == "" {
		return "", "", false
	}
	first, remainder, _ := strings.Cut(p, "/")
	if static.IsReservedSilo(first) {
		return "", "", false
	}
	return first, strings.Trim(remainder, "/"), true
}
