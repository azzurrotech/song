package song

import (
	"errors"
	"io"
	"mime"
	"net/http"
	"path"
	"path/filepath"
	"strings"

	"azzurrotech/song/pkg/static"
)

// maxInlineHTML caps the size of a served .html file that song will read into
// memory to process it as a Go template / web-component document. Larger files
// are streamed verbatim.
const maxInlineHTML = 4 << 20 // 4 MiB

// maxUploadBytes caps the content accepted by a POST resource upsert, so a
// single request cannot fill the disk or memory.
const maxUploadBytes = 64 << 20 // 64 MiB

// handleSilo serves a request for a path inside an existing silo.
//
// GET/HEAD:
//
//  1. template routes from the silo metadata take precedence (server-side
//     rendering of a named Go template, in text or HTML format),
//  2. a directory request serves its index.html document when present,
//  3. HTML documents are processed as Go templates and gain the system-defined
//     web-component tags,
//  4. everything else is served as a plain static asset.
//
// POST upserts a static resource at the requested path with the request body as
// its content (see upsertResource).
func (s *Song) handleSilo(w http.ResponseWriter, r *http.Request, silo, rest string) {
	switch r.Method {
	case http.MethodGet, http.MethodHead:
		// Serving path below.
	case http.MethodPost:
		s.upsertResource(w, r, silo, rest)
		return
	default:
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

	// 2. Directory requests serve their index.html document when present.
	dirPath := rest
	if dirPath == "" || strings.HasSuffix(r.URL.Path, "/") {
		if s.serveIndexDocument(w, r, silo, dirPath) {
			return
		}
	}

	s.serveFile(w, r, silo, rest)
}

// serveIndexDocument serves <dir>/index.html when the document exists and
// reports whether the request was handled. A directory without an index
// document is left to the caller, which turns it into a 404.
func (s *Song) serveIndexDocument(w http.ResponseWriter, r *http.Request, silo, dir string) bool {
	rel := path.Join(dir, "index.html")
	if _, err := s.store.Stat(silo, rel); err != nil || rel == "" {
		return false
	}
	s.serveFile(w, r, silo, rel)
	return true
}

// upsertResource implements POST /<silo>/<path>: create or update a static
// resource whose content is the raw request body. This is the HTTP surface the
// README describes as "POST /<path> — Upsert (create or update) static web
// resources. The request body contains the resource content."
//
// It returns 201 Created for a new resource and 200 OK when an existing
// resource was replaced; the response is JSON describing the stored file.
func (s *Song) upsertResource(w http.ResponseWriter, r *http.Request, silo, rest string) {
	rel, err := static.ParseRel(rest)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	if rel == "" {
		http.Error(w, "path is required", http.StatusBadRequest)
		return
	}
	body, err := io.ReadAll(io.LimitReader(r.Body, maxUploadBytes))
	if err != nil {
		http.Error(w, "read body: "+err.Error(), http.StatusBadRequest)
		return
	}
	content := string(body)

	fi, err := s.store.Create(silo, rel, content)
	if err == nil {
		writeJSON(w, http.StatusCreated, map[string]any{"message": "resource created", "file": fi})
		return
	}
	if errors.Is(err, static.ErrExists) {
		fi, err = s.store.Update(silo, rel, content)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"message": "resource updated", "file": fi})
		return
	}

	status := http.StatusInternalServerError
	if errors.Is(err, static.ErrTraversal) || errors.Is(err, static.ErrReserved) || errors.Is(err, static.ErrInvalidName) {
		status = http.StatusBadRequest
	}
	http.Error(w, err.Error(), status)
}

// serveFile streams a silo file with correct content type and cache headers.
// HTML documents are first given the template/web-component treatment; when
// they need none they are streamed byte-for-byte.
func (s *Song) serveFile(w http.ResponseWriter, r *http.Request, silo, rel string) {
	if isHTMLPath(rel) && s.tryServeHTML(w, r, silo, rel) {
		return
	}

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
	// ServeContent handles conditional requests and Range on the seeking
	// reader.
	http.ServeContent(w, r, entry.Name, entry.ModTime, entry.Reader)
}

// isHTMLPath reports whether a silo path names an HTML document.
func isHTMLPath(rel string) bool {
	switch strings.ToLower(filepath.Ext(rel)) {
	case ".html", ".htm":
		return true
	}
	return false
}

// tryServeHTML processes a plain .html file with the server-side HTML template
// engine, which makes every registered web-component tag resolvable with
// {{template "tag" .}}, and injects the client-side component definitions and
// runtime when the document instantiates a registered custom element.
//
// It returns false — without having written anything — when the document needs
// no processing, is a directory, exceeds maxInlineHTML, cannot be read, or is
// not valid Go template syntax. The caller then streams the file verbatim, so
// ordinary static HTML is never altered.
func (s *Song) tryServeHTML(w http.ResponseWriter, r *http.Request, silo, rel string) bool {
	info, err := s.store.Stat(silo, rel)
	if err != nil || info.IsDir || info.Size > maxInlineHTML {
		return false
	}
	entry, err := s.store.Read(silo, rel, true)
	if err != nil {
		return false
	}
	src := string(entry.Content)
	usesComponents := s.components.UsedIn(src)
	if !usesComponents && !strings.Contains(src, "{{") {
		return false
	}
	out, err := s.engine.RenderInline(silo, src, nil)
	if err != nil {
		if !usesComponents {
			// Not valid Go template syntax: serve the original file untouched.
			return false
		}
		// The document uses a registered tag but is not valid Go template
		// syntax. Serve it verbatim but still make the tag available.
		out = src
	}
	if usesComponents {
		out = s.components.Inject(out)
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	setCacheHeaders(w, rel)
	w.Header().Set("X-Song-Template-Processed", "1")
	http.ServeContent(w, r, entry.Name, entry.Modified, strings.NewReader(out))
	return true
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
	if s.engine.IsText(silo, tpl) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	} else {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
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
