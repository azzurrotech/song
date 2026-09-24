package main

import (
	"html/template"
	"strings"
)

// templateFuncs demonstrates how a host injects additional functions into
// song's server-side rendering engine.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		"who":  func(name string) string { return "hello, " + name },
		"loud": func(s string) string { return strings.ToUpper(s) + "!" },
	}
}
