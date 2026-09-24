package song

import (
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// writeJSON encodes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr writes a {"error": msg} JSON response.
func writeErr(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// methodNotAllowed guards API handlers with a JSON 405.
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
}

// fileOp is the common shape of an HTML form or JSON CRUD request for a file.
type fileOp struct {
	Path      string
	Content   string
	Dir       string // multipart upload target directory
	Encrypt   *bool  // nil keeps the current state (update); false is create-default
	Overwrite bool
}

// parseBool recognizes the truthy/falsy spellings produced by HTML forms.
func parseBool(v string) (bool, bool) {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "true", "1", "on", "yes":
		return true, true
	case "false", "0", "off", "no":
		return false, true
	}
	return false, false
}

func boolPtr(v string) *bool {
	if b, ok := parseBool(v); ok {
		return &b
	}
	return nil
}

// mediaTypeOf returns the lower-cased media type of a request body.
func mediaTypeOf(r *http.Request) string {
	ct := r.Header.Get("Content-Type")
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = ct[:i]
	}
	return strings.ToLower(strings.TrimSpace(ct))
}

// decodeFileOp accepts every request format an HTML form (or an API client)
// can produce:
//
//	application/json                    {"path","content","encrypt","overwrite","dir"}
//	application/x-www-form-urlencoded   path=&content=&encrypt=&overwrite=
//	multipart/form-data                 path=&dir=&encrypt=&overwrite= + optional "file" parts
//	anything else                       raw body is the content (path from query/header)
//
// It returns the parsed operation and any uploaded file parts, which the
// caller may stream to disk.
func decodeFileOp(r *http.Request) (fileOp, []*multipart.FileHeader, error) {
	op := fileOp{}
	switch mediaTypeOf(r) {
	case "application/json":
		var body struct {
			Path      string `json:"path"`
			Content   string `json:"content"`
			Dir       string `json:"dir,omitempty"`
			Encrypt   *bool  `json:"encrypt,omitempty"`
			Overwrite bool   `json:"overwrite,omitempty"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			return op, nil, fmt.Errorf("invalid JSON body: %w", err)
		}
		op = fileOp{Path: body.Path, Content: body.Content, Dir: body.Dir, Encrypt: body.Encrypt, Overwrite: body.Overwrite}
	case "application/x-www-form-urlencoded":
		if err := r.ParseForm(); err != nil {
			return op, nil, fmt.Errorf("invalid form body: %w", err)
		}
		op = fileOp{
			Path:      r.FormValue("path"),
			Content:   r.FormValue("content"),
			Dir:       r.FormValue("dir"),
			Encrypt:   boolPtr(r.FormValue("encrypt")),
			Overwrite: boolVal(r.FormValue("overwrite")),
		}
	case "multipart/form-data":
		if err := r.ParseMultipartForm(16 << 20); err != nil {
			return op, nil, fmt.Errorf("invalid multipart body: %w", err)
		}
		if r.MultipartForm == nil {
			return op, nil, fmt.Errorf("empty multipart body")
		}
		op = fileOp{
			Path:      r.FormValue("path"),
			Content:   r.FormValue("content"),
			Dir:       r.FormValue("dir"),
			Encrypt:   boolPtr(r.FormValue("encrypt")),
			Overwrite: boolVal(r.FormValue("overwrite")),
		}
		var files []*multipart.FileHeader
		for _, fhs := range r.MultipartForm.File {
			files = append(files, fhs...)
		}
		return op, files, nil
	default:
		// Raw body upload: path comes from the query string or X-Song-Path.
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return op, nil, fmt.Errorf("read body: %w", err)
		}
		op.Path = r.URL.Query().Get("path")
		if op.Path == "" {
			op.Path = r.Header.Get("X-Song-Path")
		}
		op.Content = string(b)
		op.Encrypt = boolPtr(r.URL.Query().Get("encrypt"))
		if op.Encrypt == nil {
			op.Encrypt = boolPtr(r.Header.Get("X-Song-Encrypt"))
		}
		op.Overwrite = boolVal(r.URL.Query().Get("overwrite"))
	}
	return op, nil, nil
}

func boolVal(v string) bool {
	b, _ := parseBool(v)
	return b
}

// valuesToData converts url.Values (query strings or HTML forms) into a
// template data map with light type inference: repeated keys become slices,
// and values that look like booleans, integers or floats are converted.
// The "template" key is dropped (it selects the template to render).
func valuesToData(v url.Values) map[string]any {
	out := map[string]any{}
	for k, vals := range v {
		if k == "template" {
			continue
		}
		switch len(vals) {
		case 0:
			out[k] = ""
		case 1:
			out[k] = inferScalar(vals[0])
		default:
			items := make([]any, 0, len(vals))
			for _, s := range vals {
				items = append(items, inferScalar(s))
			}
			out[k] = items
		}
	}
	return out
}

// inferScalar turns a form value into a bool/int/float/string. Exact
// "true"/"false" map to booleans, integers and floats are parsed, otherwise
// the raw string is kept.
func inferScalar(s string) any {
	switch s {
	case "true":
		return true
	case "false":
		return false
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// contentFromParts prefers an explicit form field, else the first uploaded
// file part (up to the given limit).
func contentFromParts(op fileOp, files []*multipart.FileHeader, limit int64) (string, error) {
	if op.Content != "" {
		return op.Content, nil
	}
	if len(files) == 0 {
		return op.Content, nil
	}
	fh := files[0]
	f, err := fh.Open()
	if err != nil {
		return "", fmt.Errorf("open upload %q: %w", fh.Filename, err)
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", fmt.Errorf("read upload %q: %w", fh.Filename, err)
	}
	return string(b), nil
}
