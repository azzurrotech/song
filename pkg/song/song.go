// Package song is a static-file hosting and management server with optional
// server-side rendering, built entirely on the Go standard library.
//
// It runs in two modes:
//
//   - Server mode: a standalone Go webserver. See the main package and the
//     Handler() method.
//   - Middleware mode: song.Handler is an http.Handler that can be mounted
//     into any other Go server, and song.Middleware(next) serves only the
//     routes song owns and delegates everything else to next.
//
// Features:
//
//   - Siloed static hosting for HTML/CSS/JS web applications, with a full
//     CRUD API that accepts JSON, url-encoded forms, multipart uploads and
//     raw request bodies (anything an HTML form can produce).
//   - index.html selection based on client headers (e.g. User-Agent or
//     X-Index-Variant) with per-silo rules and optional 302 redirects.
//   - Encrypted file hosting: files flagged encrypt=true are stored as
//     AES-256-GCM ciphertext at rest and transparently decrypted on serve.
//   - Server-side rendering over the complete html/template surface, with a
//     batteries-included FuncMap, custom delimiters, host-injected
//     functions and data from JSON/form/query parameters.
package song

import (
	"html/template"
	"net/http"
	"strings"

	"azzurrotech/song/pkg/auth"
	"azzurrotech/song/pkg/static"
	"azzurrotech/song/pkg/tmpl"
)

// Version of the song server/library.
const Version = "2.0.0"

// Config controls a Song instance.
type Config struct {
	Root   string           // store root directory (default "./data")
	Secret string           // encryption secret, at least 32 characters
	Funcs  template.FuncMap // optional extra server-side template functions
	// DisableUI hides the /admin management interface.
	DisableUI bool
}

// Song ties the static store, the magic-link auth service and the template
// engine into one HTTP surface usable as a server or as middleware.
type Song struct {
	cfg    Config
	store  *static.Store
	auth   *auth.AuthService
	engine *tmpl.Engine
}

// New builds a Song, creating the store root when needed.
func New(cfg Config) (*Song, error) {
	store, err := static.NewStore(cfg.Root, cfg.Secret)
	if err != nil {
		return nil, err
	}
	authSvc, err := auth.NewAuthService(cfg.Secret)
	if err != nil {
		return nil, err
	}
	engine := tmpl.New(store.Root())
	if cfg.Funcs != nil {
		engine.SetFuncs(cfg.Funcs)
	}
	return &Song{cfg: cfg, store: store, auth: authSvc, engine: engine}, nil
}

// Store exposes the underlying siloed file store.
func (s *Song) Store() *static.Store { return s.store }

// Auth exposes the magic-link auth service.
func (s *Song) Auth() *auth.AuthService { return s.auth }

// Templates exposes the server-side template engine.
func (s *Song) Templates() *tmpl.Engine { return s.engine }

// AddFuncs injects extra template functions for SSR.
func (s *Song) AddFuncs(fm template.FuncMap) { s.engine.AddFuncs(fm) }

// Config returns the configuration the song was built with.
func (s *Song) Config() Config { return s.cfg }

// Handler returns a complete http.Handler for server mode: API, admin UI,
// auth, health, and silo serving. Paths song does not own fall through to
// http.NotFound.
func (s *Song) Handler() http.Handler {
	return s.Middleware(http.NotFoundHandler())
}

// Middleware returns an http.Handler that serves the routes song owns and
// delegates everything else to next. This is how another Go server opts
// into song: serveStaticAssets := srv.Middleware(myHandler). Any request
// path that is a silo name, /api/song/*, /api/auth/*, /health or /admin
// is handled by song; all others fall through.
func (s *Song) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		switch {
		case p == "/health":
			s.handleHealth(w, r)
		case strings.HasPrefix(p, "/api/song/"):
			s.handleAPI(w, r)
		case strings.HasPrefix(p, "/api/auth/"):
			s.handleAuth(w, r)
		case !s.cfg.DisableUI && (p == "/admin" || strings.HasPrefix(p, "/admin/")):
			s.handleUI(w, r)
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
