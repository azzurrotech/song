package song

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"

	"azzurrotech/song/pkg/static"
	"azzurrotech/song/pkg/tmpl"
)

// maxUploadBytes caps file content accepted through the API.
const maxUploadBytes = 64 << 20 // 64 MiB

const apiBase = "/api/song/silos"

// handleAPI routes all /api/song/silos/* requests.
func (s *Song) handleAPI(w http.ResponseWriter, r *http.Request) {
	if !strings.HasPrefix(r.URL.Path, apiBase) {
		writeErr(w, http.StatusNotFound, "not found")
		return
	}
	parts := splitSegments(strings.TrimPrefix(r.URL.Path, apiBase))
	switch len(parts) {
	case 0:
		// /api/song/silos
		switch r.Method {
		case http.MethodGet:
			s.apiListSilos(w, r)
		case http.MethodPost:
			s.apiCreateSilo(w, r)
		default:
			methodNotAllowed(w, "GET, POST")
		}
	case 1:
		// /api/song/silos/{silo}
		switch r.Method {
		case http.MethodDelete:
			s.apiDeleteSilo(w, r, parts[0])
		default:
			methodNotAllowed(w, "DELETE")
		}
	default:
		s.handleFileAPI(w, r, parts)
	}
}

// splitSegments splits a path into non-empty, slash-free segments.
func splitSegments(p string) []string {
	if p == "" {
		return nil
	}
	var out []string
	for _, seg := range strings.Split(strings.Trim(p, "/"), "/") {
		if seg != "" {
			out = append(out, seg)
		}
	}
	return out
}

func (s *Song) apiListSilos(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	names, err := s.store.ListSilos()
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type siloInfo struct {
		Name  string `json:"name"`
		Files int    `json:"files"`
	}
	out := make([]siloInfo, 0, len(names))
	for _, name := range names {
		n := 0
		if files, err := s.store.List(name, ""); err == nil {
			n = len(files)
		}
		out = append(out, siloInfo{Name: name, Files: n})
	}
	writeJSON(w, http.StatusOK, map[string]any{"silos": out})
}

func (s *Song) apiCreateSilo(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	name := ""
	switch mediaTypeOf(r) {
	case "application/json":
		var body struct {
			Name string `json:"name"`
		}
		if err := jsonDecode(r, &body); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		name = body.Name
	default:
		if err := r.ParseForm(); err != nil {
			writeErr(w, http.StatusBadRequest, "invalid form body")
			return
		}
		name = r.FormValue("name")
	}
	if name == "" {
		writeErr(w, http.StatusBadRequest, "name is required")
		return
	}
	if err := s.store.CreateSilo(name); err != nil {
		if errors.Is(err, static.ErrExists) {
			writeErr(w, http.StatusConflict, fmt.Sprintf("silo %q already exists", name))
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]string{"name": name, "message": "silo created"})
}

func (s *Song) apiDeleteSilo(w http.ResponseWriter, r *http.Request, name string) {
	if r.Method != http.MethodDelete {
		methodNotAllowed(w, "DELETE")
		return
	}
	if err := s.store.RemoveSilo(name); err != nil {
		if errors.Is(err, static.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "silo removed"})
}

// handleFileAPI dispatches the per-silo file/render/meta/templates/upload
// endpoints.
func (s *Song) handleFileAPI(w http.ResponseWriter, r *http.Request, parts []string) {
	silo := parts[0]
	if !s.store.HasSilo(silo) {
		writeErr(w, http.StatusNotFound, fmt.Sprintf("silo %q not found", silo))
		return
	}
	action := parts[1]
	switch action {
	case "files":
		s.apiListFiles(w, r, silo)
	case "file":
		s.apiFile(w, r, silo)
	case "upload":
		s.apiUpload(w, r, silo)
	case "render":
		s.apiRender(w, r, silo)
	case "meta":
		s.apiMeta(w, r, silo)
	case "templates":
		s.apiTemplates(w, r, silo)
	default:
		writeErr(w, http.StatusNotFound, "unknown endpoint")
	}
}

func (s *Song) apiListFiles(w http.ResponseWriter, r *http.Request, silo string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	rel := r.URL.Query().Get("path")
	files, err := s.store.List(silo, rel)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, static.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"silo": silo, "path": "/" + rel, "files": files})
}

// apiFile handles GET (read), POST (create), PUT (update), DELETE.
func (s *Song) apiFile(w http.ResponseWriter, r *http.Request, silo string) {
	switch r.Method {
	case http.MethodGet:
		s.apiReadFile(w, r, silo)
	case http.MethodPost:
		s.apiCreateFile(w, r, silo)
	case http.MethodPut:
		s.apiUpdateFile(w, r, silo)
	case http.MethodDelete:
		s.apiDeleteFile(w, r, silo)
	default:
		methodNotAllowed(w, "GET, POST, PUT, DELETE")
	}
}

func (s *Song) apiReadFile(w http.ResponseWriter, r *http.Request, silo string) {
	rel, err := static.ParseRel(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	entry, err := s.store.Read(silo, rel, true)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, static.ErrNotFound) {
			status = http.StatusNotFound
		}
		if errors.Is(err, static.ErrIsDir) {
			status = http.StatusBadRequest
		}
		writeErr(w, status, err.Error())
		return
	}
	resp := map[string]any{
		"name":         entry.Name,
		"path":         entry.Path,
		"size":         entry.Size,
		"content_type": entry.ContentType,
		"encrypted":    entry.Encrypted,
		"modified":     entry.Modified.Format(time.RFC3339),
	}
	if enc := r.URL.Query().Get("encoding"); enc == "base64" {
		resp["content_base64"] = base64.StdEncoding.EncodeToString(entry.Content)
	} else {
		resp["content"] = string(entry.Content)
	}
	writeJSON(w, http.StatusOK, resp)
}

func (s *Song) apiCreateFile(w http.ResponseWriter, r *http.Request, silo string) {
	op, files, err := decodeFileOp(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rel, err := static.ParseRel(op.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if rel == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	encrypt := false
	if op.Encrypt != nil {
		encrypt = *op.Encrypt
	}
	content, err := contentFromParts(op, files, maxUploadBytes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	fi, err := s.store.Create(silo, rel, content, encrypt)
	if err != nil {
		if errors.Is(err, static.ErrExists) {
			if !op.Overwrite {
				writeErr(w, http.StatusConflict, fmt.Sprintf("%q already exists (set overwrite=true to replace)", rel))
				return
			}
			fi, err = s.store.Update(silo, rel, content, op.Encrypt)
			if err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			writeJSON(w, http.StatusOK, map[string]any{"message": "file replaced", "file": fi})
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"message": "file created", "file": fi})
}

func (s *Song) apiUpdateFile(w http.ResponseWriter, r *http.Request, silo string) {
	op, files, err := decodeFileOp(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	rel, err := static.ParseRel(op.Path)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if rel == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	content, err := contentFromParts(op, files, maxUploadBytes)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	fi, err := s.store.Update(silo, rel, content, op.Encrypt)
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, static.ErrNotFound) {
			status = http.StatusNotFound
		}
		writeErr(w, status, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"message": "file updated", "file": fi})
}

func (s *Song) apiDeleteFile(w http.ResponseWriter, r *http.Request, silo string) {
	rel, err := static.ParseRel(r.URL.Query().Get("path"))
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	if rel == "" {
		writeErr(w, http.StatusBadRequest, "path is required")
		return
	}
	if err := s.store.Delete(silo, rel); err != nil {
		if errors.Is(err, static.ErrNotFound) {
			writeErr(w, http.StatusNotFound, err.Error())
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"message": "file deleted"})
}

// apiUpload accepts a multipart form with a path/dir field and one or more
// "file" parts, streaming each into the silo.
func (s *Song) apiUpload(w http.ResponseWriter, r *http.Request, silo string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	op, files, err := decodeFileOp(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	dir, err := static.ParseRel(op.Dir)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	encrypt := false
	if op.Encrypt != nil {
		encrypt = *op.Encrypt
	}
	type uploadResult struct {
		Name    string `json:"name"`
		Path    string `json:"path"`
		Created bool   `json:"created"`
		Error   string `json:"error,omitempty"`
	}
	var results []uploadResult
	for _, fh := range files {
		result := s.uploadOne(silo, dir, fh, encrypt)
		results = append(results, result)
	}
	if len(results) == 0 {
		writeErr(w, http.StatusBadRequest, "no file parts in multipart body")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"silo": silo, "uploads": results})
}

func (s *Song) uploadOne(silo, dir string, fh *multipart.FileHeader, encrypt bool) (res struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Created bool   `json:"created"`
	Error   string `json:"error,omitempty"`
}) {
	res.Name = fh.Filename
	full := path.Join(dir, fh.Filename)
	res.Path = "/" + full
	clean, err := static.ParseRel(full)
	if err != nil {
		res.Error = err.Error()
		return
	}
	f, err := fh.Open()
	if err != nil {
		res.Error = fmt.Sprintf("open upload: %v", err)
		return
	}
	defer f.Close()
	if _, err := s.store.Upload(silo, clean, io.LimitReader(f, maxUploadBytes), encrypt); err != nil {
		res.Error = err.Error()
		return
	}
	res.Created = true
	return
}

// apiMeta GET returns the silo metadata; PUT replaces it.
func (s *Song) apiMeta(w http.ResponseWriter, r *http.Request, silo string) {
	switch r.Method {
	case http.MethodGet:
		m, err := s.store.Meta(silo)
		if err != nil {
			writeErr(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, m)
	case http.MethodPut:
		var m static.Meta
		if err := jsonDecode(r, &m); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := s.store.SaveMeta(silo, &m); err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"message": "meta saved"})
	default:
		methodNotAllowed(w, "GET, PUT")
	}
}

// apiTemplates lists the template files and parseable template names of a silo.
func (s *Song) apiTemplates(w http.ResponseWriter, r *http.Request, silo string) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, "GET")
		return
	}
	names, err := s.engine.TemplateNames(silo)
	if err != nil && !errors.Is(err, tmpl.ErrNoTemplates) {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	dir, err := s.store.TemplateDir(silo)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, err.Error())
		return
	}
	type tplFile struct {
		Name    string `json:"name"`
		Content string `json:"content,omitempty"`
	}
	files := []tplFile{}
	if entries, err := os.ReadDir(dir); err == nil {
		for _, en := range entries {
			if en.IsDir() || strings.HasPrefix(en.Name(), ".") {
				continue
			}
			b, _ := os.ReadFile(filepath.Join(dir, en.Name()))
			files = append(files, tplFile{Name: en.Name(), Content: string(b)})
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"silo": silo, "names": names, "files": files})
}

// apiRender performs server-side rendering. Data may arrive as a JSON body
// ({"template": "...", "data": {...}}), as HTML form fields, or as query
// parameters (GET). The response is the fully-rendered HTML document.
func (s *Song) apiRender(w http.ResponseWriter, r *http.Request, silo string) {
	if r.Method != http.MethodGet && r.Method != http.MethodPost {
		methodNotAllowed(w, "GET, POST")
		return
	}
	tplName := r.URL.Query().Get("template")
	var data any = map[string]any{}

	switch r.Method {
	case http.MethodPost:
		switch mediaTypeOf(r) {
		case "application/json":
			var raw any
			if err := jsonDecode(r, &raw); err != nil {
				writeErr(w, http.StatusBadRequest, err.Error())
				return
			}
			if m, ok := raw.(map[string]any); ok {
				if t, ok := m["template"].(string); ok && t != "" {
					tplName = t
				}
				if d, ok := m["data"]; ok {
					data = d
				} else {
					copy := map[string]any{}
					for k, v := range m {
						if k != "template" {
							copy[k] = v
						}
					}
					data = copy
				}
			} else {
				data = raw
			}
		default:
			if err := r.ParseForm(); err != nil {
				writeErr(w, http.StatusBadRequest, "invalid form body")
				return
			}
			if t := r.FormValue("template"); t != "" {
				tplName = t
			}
			vals := r.PostForm
			if len(vals) == 0 {
				vals = r.URL.Query()
			}
			data = valuesToData(vals)
		}
	default:
		data = valuesToData(r.URL.Query())
	}

	s.renderTo(w, r, silo, tplName, data)
}

func (s *Song) renderTo(w http.ResponseWriter, r *http.Request, silo, tplName string, data any) {
	var buf bytes.Buffer
	if err := s.engine.Render(&buf, silo, tplName, data); err != nil {
		status := http.StatusInternalServerError
		if errors.Is(err, tmpl.ErrNoTemplates) {
			status = http.StatusNotFound
		}
		writeErr(w, status, "render error: "+err.Error())
		return
	}
	if format := r.URL.Query().Get("format"); format == "json" {
		writeJSON(w, http.StatusOK, map[string]any{"html": buf.String(), "template": tplName})
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("X-Song-Template", tplName)
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(buf.Bytes())
}

func jsonDecode(r *http.Request, v any) error {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	return nil
}
