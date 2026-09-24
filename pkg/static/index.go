package static

import (
	"fmt"
	"net/http"
	"path"
	"regexp"
	"strings"
)

// defaultDocument is served when no header rule matches.
const defaultDocument = "index.html"

// ResolveIndex picks the index document to serve for a directory request.
// Rules are evaluated in order; the first rule whose declared header is
// present and matches (exact value or regexp pattern) wins, provided the
// target document exists. As a built-in convenience the X-Index-Variant
// header maps to index.<variant>.html when present. Otherwise the plain
// index.html is served. The returned redirect flag mirrors the matched
// rule and tells the HTTP layer whether to 302 the client to the document
// rather than serving it inline.
func (s *Store) ResolveIndex(silo, dir string, hdr http.Header) (file string, redirect bool, err error) {
	m, err := s.loadMeta(silo)
	if err != nil {
		return "", false, err
	}
	for _, rule := range m.IndexRules {
		if rule.Header == "" || rule.File == "" {
			continue
		}
		value := hdr.Get(rule.Header)
		if value == "" {
			continue
		}
		matched := false
		switch {
		case rule.Value != "":
			matched = value == rule.Value
		case rule.Pattern != "":
			re, cerr := regexp.Compile(rule.Pattern)
			if cerr != nil {
				continue
			}
			matched = re.MatchString(value)
		}
		if !matched {
			continue
		}
		cand := path.Join(dir, rule.File)
		if s.fileExists(silo, cand) {
			return rule.File, rule.Redirect, nil
		}
	}

	// Built-in: X-Index-Variant: <name> -> index.<name>.html
	if v := strings.TrimSpace(hdr.Get("X-Index-Variant")); v != "" {
		if !strings.ContainsAny(v, "/\\") && !strings.HasPrefix(v, ".") {
			cand := path.Join(dir, "index."+v+".html")
			if s.fileExists(silo, cand) {
				return "index." + v + ".html", false, nil
			}
		}
	}

	if s.fileExists(silo, path.Join(dir, defaultDocument)) {
		return defaultDocument, false, nil
	}
	return "", false, fmt.Errorf("%w: no index document in %q", ErrNotFound, dir)
}
