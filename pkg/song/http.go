package song

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strconv"
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

// methodNotAllowed answers with a JSON 405 and the allowed methods.
func methodNotAllowed(w http.ResponseWriter, allow string) {
	w.Header().Set("Allow", allow)
	writeErr(w, http.StatusMethodNotAllowed, "method not allowed")
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
