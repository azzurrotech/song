package tmpl

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/url"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
)

// defaultFuncs returns the built-in function map that song makes available
// to every server-side template. It complements the html/template built-ins
// (and/or/not/eq/ne/lt/le/gt/ge, len, index, slice, printf, js, html,
// urlquery, call, ...) with string, math, data-structure, encoding,
// time, regexp and typed-string helpers so that templates can rely on the
// full html/template surface plus a batteries-included toolkit.
func defaultFuncs() template.FuncMap {
	return template.FuncMap{
		// --- typed strings: values rendered without contextual escaping ---
		"html":     func(v any) template.HTML { return template.HTML(fmt.Sprint(v)) },
		"htmlAttr": func(v any) template.HTMLAttr { return template.HTMLAttr(fmt.Sprint(v)) },
		"js":       func(v any) template.JS { return template.JS(fmt.Sprint(v)) },
		"jsStr":    func(v any) template.JSStr { return template.JSStr(fmt.Sprint(v)) },
		"css":      func(v any) template.CSS { return template.CSS(fmt.Sprint(v)) },
		"url":      func(v any) template.URL { return template.URL(fmt.Sprint(v)) },
		"srcset":   func(v any) template.Srcset { return template.Srcset(fmt.Sprint(v)) },

		// --- JSON / serialization ---
		"json":       jsonJS,     // template.JS: safe inside <script>
		"toJSON":     jsonString, // plain string, escaped by the context
		"prettyJSON": jsonPretty, // indented string
		"fromJSON":   jsonFrom,   // string -> any

		// --- strings ---
		"upper":      strings.ToUpper,
		"lower":      strings.ToLower,
		"title":      titleString,
		"trim":       strings.TrimSpace,
		"trimSpace":  strings.TrimSpace,
		"trimPrefix": strings.TrimPrefix,
		"trimSuffix": strings.TrimSuffix,
		"trimAll":    strings.Trim,
		"trimLeft":   strings.TrimLeft,
		"trimRight":  strings.TrimRight,
		"replace":    replaceAll,
		"replaceAll": replaceAll,
		"repeat":     strings.Repeat,
		"contains":   strings.Contains,
		"hasPrefix":  strings.HasPrefix,
		"hasSuffix":  strings.HasSuffix,
		"split":      strings.Split,
		"splitN":     strings.SplitN,
		"fields":     strings.Fields,
		"join":       joinItems,
		"cat":        catItems,
		"abbrev":     abbrev,
		"slug":       slug,

		// --- defaults & logic ---
		"default":  def,
		"coalesce": coalesce,
		"empty":    empty,
		"ternary":  ternary,

		// --- data structures ---
		"dict":   dict,
		"list":   list,
		"keys":   keys,
		"get":    mapGet,
		"hasKey": mapHasKey,

		// --- math ---
		"add": func(a, b any) any { return arith(a, b, 0) },
		"sub": func(a, b any) any { return arith(a, b, 1) },
		"mul": func(a, b any) any { return arith(a, b, 2) },
		"div": func(a, b any) any { return arith(a, b, 3) },
		"mod": func(a, b any) any { return arith(a, b, 4) },
		"seq": seq,

		// --- casting / formatting ---
		"atoi": func(s string) (int, error) { return strconv.Atoi(s) },
		"itoa": func(i int) string { return strconv.Itoa(i) },
		"str":  func(v any) string { return fmt.Sprint(v) },
		"fmt":  func(format string, args ...any) string { return fmt.Sprintf(format, args...) },

		// --- time ---
		"now":      time.Now,
		"date":     func(layout string, t time.Time) string { return t.Format(layout) },
		"rfc3339":  func(t time.Time) string { return t.Format(time.RFC3339) },
		"unixTime": func(sec int64) time.Time { return time.Unix(sec, 0) },
		"addTime":  addTime,

		// --- encoding ---
		"b64enc":    func(b []byte) string { return base64.StdEncoding.EncodeToString(b) },
		"b64dec":    func(s string) string { b, _ := base64.StdEncoding.DecodeString(s); return string(b) },
		"urlencode": url.QueryEscape,
		"urlpath":   url.PathEscape,

		// --- regexp ---
		"regexMatch":   func(pattern, s string) (bool, error) { return regexp.MatchString(pattern, s) },
		"regexReplace": regexReplace,
		"regexQuote":   regexp.QuoteMeta,

		// --- hashing ---
		"sha256": func(s string) string {
			sum := sha256.Sum256([]byte(s))
			return hex.EncodeToString(sum[:])
		},
		"fileSize": fileSize,
	}
}

func jsonJS(v any) template.JS {
	b, err := json.Marshal(v)
	if err != nil {
		return template.JS("null")
	}
	return template.JS(b)
}

func jsonString(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonPretty(v any) (string, error) {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func jsonFrom(s string) (any, error) {
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return nil, err
	}
	return v, nil
}

func titleString(s string) string {
	prevSpace := true
	runes := []rune(s)
	for i, r := range runes {
		if prevSpace {
			runes[i] = unicode.ToTitle(r)
			prevSpace = false
		} else if unicode.IsSpace(r) {
			prevSpace = true
		}
	}
	return string(runes)
}

func replaceAll(s, old, new string) string { return strings.ReplaceAll(s, old, new) }

// joinItems joins a slice/array of values.
// Accepted call shapes: {{join .Items ","}} (2 args: collection, separator)
// or {{join .Items}} and variadic {{join "a" "b" "c"}}.
func joinItems(args ...any) (string, error) {
	if len(args) == 0 {
		return "", nil
	}
	if len(args) == 2 {
		// separator-first: {{join "," .Items}}
		if _, isSep := args[0].(string); isSep {
			parts, ok := stringifySlice(args[1])
			if !ok {
				return "", fmt.Errorf("join: cannot iterate %T", args[1])
			}
			return strings.Join(parts, args[0].(string)), nil
		}
		// collection-first: {{join .Items ","}}
		parts, ok := stringifySlice(args[0])
		if !ok {
			return "", fmt.Errorf("join: cannot iterate %T", args[0])
		}
		return strings.Join(parts, fmt.Sprint(args[1])), nil
	}
	if len(args) == 1 {
		parts, ok := stringifySlice(args[0])
		if !ok {
			return "", fmt.Errorf("join: cannot iterate %T", args[0])
		}
		return strings.Join(parts, ""), nil
	}
	parts, ok := stringifySlice(args)
	if !ok {
		return "", fmt.Errorf("join: cannot stringify arguments")
	}
	return strings.Join(parts, ","), nil
}

func catItems(args ...any) (string, error) {
	parts, ok := stringifySlice(args)
	if !ok {
		return "", fmt.Errorf("cat: cannot stringify arguments")
	}
	return strings.Join(parts, " "), nil
}

func stringifySlice(v any) ([]string, bool) {
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Slice, reflect.Array:
		out := make([]string, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			out[i] = fmt.Sprint(rv.Index(i).Interface())
		}
		return out, true
	case reflect.String:
		return []string{rv.String()}, true
	}
	return nil, false
}

func abbrev(s string, max int) string {
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	if max < 3 {
		max = 3
	}
	return string(r[:max-3]) + "..."
}

func slug(s string) string {
	var b strings.Builder
	prevDash := false
	for _, r := range strings.ToLower(strings.TrimSpace(s)) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
			prevDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_':
			if !prevDash && b.Len() > 0 {
				b.WriteByte('-')
				prevDash = true
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func def(dflt, v any) any {
	if empty(v) {
		return dflt
	}
	return v
}

func coalesce(args ...any) any {
	for _, a := range args {
		if !empty(a) {
			return a
		}
	}
	return nil
}

func empty(v any) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return rv.Len() == 0
	case reflect.Bool:
		return !rv.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return rv.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return rv.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return rv.Float() == 0
	case reflect.Interface, reflect.Ptr:
		return rv.IsNil()
	}
	return false
}

func ternary(cond bool, t, f any) any {
	if cond {
		return t
	}
	return f
}

// dict builds a map[string]any from key/value pairs.
func dict(args ...any) (map[string]any, error) {
	if len(args)%2 != 0 {
		return nil, fmt.Errorf("dict: odd number of arguments")
	}
	out := make(map[string]any, len(args)/2)
	for i := 0; i < len(args); i += 2 {
		k, ok := args[i].(string)
		if !ok {
			return nil, fmt.Errorf("dict: key #%d is not a string", i/2)
		}
		out[k] = args[i+1]
	}
	return out, nil
}

func list(args ...any) []any { return args }

func keys(m any) ([]string, error) {
	rv := reflect.ValueOf(m)
	if rv.Kind() != reflect.Map {
		return nil, fmt.Errorf("keys: not a map")
	}
	out := make([]string, 0, rv.Len())
	for _, k := range rv.MapKeys() {
		out = append(out, fmt.Sprint(k.Interface()))
	}
	sort.Strings(out)
	return out, nil
}

func mapGet(m, k any) any {
	rv := reflect.ValueOf(m)
	if rv.Kind() != reflect.Map {
		return nil
	}
	v := rv.MapIndex(reflect.ValueOf(k))
	if !v.IsValid() {
		return nil
	}
	return v.Interface()
}

func mapHasKey(m, k any) bool {
	rv := reflect.ValueOf(m)
	if rv.Kind() != reflect.Map {
		return false
	}
	return rv.MapIndex(reflect.ValueOf(k)).IsValid()
}

// arith implements add(0), sub(1), mul(2), div(3), mod(4) over int64 or
// float64 operands, returning nil when conversion fails.
func arith(a, b any, op int) any {
	if ai, aok := toInt64(a); aok {
		if bi, bok := toInt64(b); bok {
			switch op {
			case 0:
				return ai + bi
			case 1:
				return ai - bi
			case 2:
				return ai * bi
			case 3:
				if bi == 0 {
					return nil
				}
				return ai / bi
			case 4:
				if bi == 0 {
					return nil
				}
				return ai % bi
			}
		}
	}
	af, aok := toFloat64(a)
	bf, bok := toFloat64(b)
	if !aok || !bok {
		return nil
	}
	switch op {
	case 0:
		return af + bf
	case 1:
		return af - bf
	case 2:
		return af * bf
	case 3:
		if bf == 0 {
			return nil
		}
		return af / bf
	case 4:
		if bf == 0 {
			return nil
		}
		return float64(int64(af) % int64(bf))
	}
	return nil
}

func toInt64(v any) (int64, bool) {
	switch x := v.(type) {
	case int:
		return int64(x), true
	case int8:
		return int64(x), true
	case int16:
		return int64(x), true
	case int32:
		return int64(x), true
	case int64:
		return x, true
	case uint:
		return int64(x), true
	case uint8:
		return int64(x), true
	case uint16:
		return int64(x), true
	case uint32:
		return int64(x), true
	case uint64:
		return int64(x), true
	case float64:
		return int64(x), true
	case float32:
		return int64(x), true
	case string:
		i, err := strconv.ParseInt(strings.TrimSpace(x), 10, 64)
		return i, err == nil
	case json.Number:
		i, err := x.Int64()
		return i, err == nil
	}
	return 0, false
}

func toFloat64(v any) (float64, bool) {
	switch x := v.(type) {
	case int:
		return float64(x), true
	case int8:
		return float64(x), true
	case int16:
		return float64(x), true
	case int32:
		return float64(x), true
	case int64:
		return float64(x), true
	case uint:
		return float64(x), true
	case uint64:
		return float64(x), true
	case float32:
		return float64(x), true
	case float64:
		return x, true
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(x), 64)
		return f, err == nil
	case json.Number:
		f, err := x.Float64()
		return f, err == nil
	}
	return 0, false
}

// seq produces [1..n] or [from..to].
func seq(args ...int) []int {
	switch len(args) {
	case 0:
		return nil
	case 1:
		n := args[0]
		if n < 0 {
			return nil
		}
		out := make([]int, n)
		for i := range out {
			out[i] = i + 1
		}
		return out
	default:
		from, to, step := args[0], args[1], 1
		if len(args) > 2 {
			step = args[2]
		}
		if step == 0 {
			return nil
		}
		var out []int
		if step > 0 {
			for i := from; i <= to; i += step {
				out = append(out, i)
			}
		} else {
			for i := from; i >= to; i += step {
				out = append(out, i)
			}
		}
		return out
	}
}

func addTime(t time.Time, years, months, days int) time.Time {
	return t.AddDate(years, months, days)
}

func regexReplace(pattern, repl, s string) (string, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return "", err
	}
	return re.ReplaceAllString(s, repl), nil
}

func fileSize(n any) string {
	v, ok := toInt64(n)
	if !ok {
		return fmt.Sprint(n)
	}
	const unit = 1024
	if v < unit {
		return fmt.Sprintf("%d B", v)
	}
	div, exp := int64(unit), 0
	for n := v / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(v)/float64(div), "KMGTPE"[exp])
}
