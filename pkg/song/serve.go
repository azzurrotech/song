package song

import (
	"errors"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"azzurrotech/song/pkg/static"
)

// handleSilo serves a request for a path inside an existing silo:
//
//  1. template routes from the silo metadata take precedence (SSR),
//  2. directory requests resolve their index document using the client
//     header rules (transparent serve or 302 redirect),
//  3. encrypted files are transparently decrypted before being served,
//  4. everything else is served as a plain static asset.
func (s *Song) handleSilo(w http.ResponseWriter, r *http.Request, silo, rest string) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// 1. Server-side template routes.
	if m, err := s.store.Meta(silo); err == nil && m != nil {
		if tpl := m.TemplateRoutes[rest]; tpl != "" {
			s.renderSiloRoute(w, r, silo, rest, tpl)
			return
		}
	}

	// 2. Index resolution for directory requests.
	dirPath := rest
	if dirPath == "" || strings.HasSuffix(r.URL.Path, "/") {
		idx, redirect, err := s.store.ResolveIndex(silo, dirPath, r.Header)
		if err == nil {
			full := path.Join(dirPath, idx)
			if redirect {
				target := path.Join("/", silo, full)
				http.Redirect(w, r, target, http.StatusFound)
				return
			}
			s.serveFile(w, r, silo, full)
			return
		}
	}

	s.serveFile(w, r, silo, rest)
}

// serveFile streams a silo file with correct content type, cache headers
// and transparent decryption of encrypted content.
func (s *Song) serveFile(w http.ResponseWriter, r *http.Request, silo, rel string) {
	entry, err := s.store.Open(silo, rel)
	if err != nil {
		switch {
		case errors.Is(err, static.ErrIsDir):
			// A directory was requested. Canonicalize the URL with a trailing
			// slash when missing; a genuine directory without an index
			// document gets a 404 rather than a redirect loop.
			if strings.HasSuffix(r.URL.Path, "/") {
				http.NotFound(w, r)
			} else {
				http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			}
		case errors.Is(err, static.ErrNotFound):
			http.NotFound(w, r)
		default:
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}
		return
	}
	defer entry.Reader.Close()

	ct := contentTypeFor(rel)
	w.Header().Set("Content-Type", ct)
	setCacheHeaders(w, rel)
	if entry.Encrypted {
		w.Header().Set("X-Song-Encrypted", "1")
	}
	// ServeContent handles conditional requests and Range on the (seeking)
	// reader, whether the underlying data is a plain file or a decrypted
	// in-memory buffer.
	http.ServeContent(w, r, entry.Name, entry.ModTime, entry.Reader)
}

// renderSiloRoute executes a template registered in the silo metadata,
// feeding it the URL query values as data (with light type inference).
func (s *Song) renderSiloRoute(w http.ResponseWriter, r *http.Request, silo, route, tpl string) {
	data := valuesToData(r.URL.Query())
	var buf strings.Builder
	if err := s.engine.Render(&buf, silo, tpl, data); err != nil {
		http.Error(w, "render error: "+err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Song-Template", tpl)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(buf.String()))
}

// contentTypeFor maps a file path to a web content type.
func contentTypeFor(rel string) string {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".html", ".htm":
		return "text/html; charset=utf-8"
	case ".css":
		return "text/css; charset=utf-8"
	case ".js", ".mjs":
		return "text/javascript; charset=utf-8"
	case ".json":
		return "application/json"
	case ".xml":
		return "application/xml"
	case ".txt":
		return "text/plain; charset=utf-8"
	case ".md":
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
	ct := mime.TypeByExtension(ext)
	if ct == "" {
		return "application/octet-stream"
	}
	return ct
}

func setCacheHeaders(w http.ResponseWriter, rel string) {
	ext := strings.ToLower(filepath.Ext(rel))
	switch ext {
	case ".html", ".htm":
		w.Header().Set("Cache-Control", "no-cache")
	case ".js", ".mjs", ".css", ".png", ".jpg", ".jpeg", ".gif", ".webp",
		".svg", ".ico", ".woff", ".woff2", ".ttf", ".eot", ".wasm", ".mp4", ".webm":
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	default:
		w.Header().Set("Cache-Control", "public, max-age=3600")
	}
}
