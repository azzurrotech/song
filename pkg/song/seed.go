package song

import (
	"fmt"

	"azzurrotech/song/pkg/static"
)

// SeedDemo creates a "demo" silo that exercises every song feature: static
// files, header-based index routing, server-side templates, template routes
// and encrypted-at-rest files. It is safe to call repeatedly.
func SeedDemo(s *Song) error {
	const silo = "demo"
	if !s.store.HasSilo(silo) {
		if err := s.store.CreateSilo(silo); err != nil {
			return fmt.Errorf("create demo silo: %w", err)
		}
	}

	files := map[string][2]string{ // rel path -> {content, "" | "encrypt"}
		"index.html": {`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>song demo</title>
<link rel="stylesheet" href="style.css">
</head>
<body>
<header><h1>🎵 song</h1><p>static web apps, server-side rendering, encrypted files</p></header>
<main>
<p>This page was selected by the default index resolution. Send the header
<code>X-Index-Variant: mobile</code> (or a mobile <code>User-Agent</code>) and song serves
<code>index.mobile.html</code> instead; <code>X-Index-Variant: desktop</code> serves
<code>index.desktop.html</code>.</p>
<ul>
<li><a href="page?title=Server+Rendering&items=one&items=two&items=three">/page — server-side rendered template (data from query)</a></li>
<li><a href="about">/about — another template</a></li>
<li><a href="secrets/notes.txt">secrets/notes.txt — encrypted at rest</a></li>
</ul>
<p class="muted">Admin UI: <a href="/admin">/admin</a> · Health: <a href="/health">/health</a></p>
</main>
<script src="app.js"></script>
</body>
</html>`, ""},
		"index.mobile.html": {`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>song demo — mobile</title><link rel="stylesheet" href="style.css"></head>
<body>
<header><h1>📱 song demo</h1></header>
<main><p>Served because you sent <code>X-Index-Variant: mobile</code> (or a mobile
User-Agent). The index document was selected by a client header.</p>
<p><a href="/">default index</a></p></main>
</body>
</html>`, ""},
		"index.desktop.html": {`<!DOCTYPE html>
<html lang="en">
<head><meta charset="UTF-8"><meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>song demo — desktop</title><link rel="stylesheet" href="style.css"></head>
<body>
<header><h1>🖥️ song demo</h1></header>
<main><p>Served because you sent <code>X-Index-Variant: desktop</code>.</p>
<p><a href="/">default index</a></p></main>
</body>
</html>`, ""},
		"style.css": {`body { font-family: system-ui, sans-serif; margin: 0; background: #f4f6fb; color: #1f2430; line-height: 1.6; }
header { background: linear-gradient(135deg, #4a148c, #6a1b9a); color: #fff; padding: 2rem; text-align: center; }
main { max-width: 760px; margin: 2rem auto; padding: 0 1rem; }
code { background: #eee; padding: .1rem .4rem; border-radius: 4px; }
a { color: #4a148c; }
.muted { color: #6b7280; font-size: .85rem; }`, ""},
		"app.js": {`console.log("song demo app loaded");`, ""},
		".templates/layout.tmpl": {`{{define "layout"}}
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>{{block "title" .}}song demo{{end}}</title>
<link rel="stylesheet" href="/demo/style.css">
</head>
<body>
<header>
  <h1>song · server-side rendering</h1>
  <nav><a href="/demo/">index</a> · <a href="/demo/about">about</a> · <a href="/demo/page?title=Hi&items=a&items=b">page</a></nav>
</header>
<main>
{{block "content" .}}<p>Default block content — overridden by pages.</p>{{end}}
</main>
<footer class="muted">
  <p>Rendered on {{now | date "2006-01-02 15:04:05"}} with the go html/template package.</p>
</footer>
<script>window.__SONG_DATA__ = {{json .}};</script>
</body>
</html>
{{end}}`, ""},
		".templates/page.tmpl": {`{{define "page"}}{{template "layout" .}}{{end}}

{{define "title"}}{{if .title}}{{.title | title}} · song demo{{else}}Untitled · song demo{{end}}{{end}}

{{define "content"}}
<h2>{{with .title}}{{.}}{{else}}A page rendered server-side{{end}}</h2>
{{if .items}}
<ul>
{{range $i, $item := .items}}<li>{{add $i 1}}. {{$item | upper}} <span class="muted">({{len $item}} chars)</span></li>{{end}}
</ul>
<p>joined: <code>{{join .items ", "}}</code></p>
{{else}}<p>no items — add <code>?items=a&amp;items=b</code> to the URL.</p>{{end}}

{{$safe := "<strong>trusted markup</strong> via template.HTML"}}{{$safe}}

<dl>
  <dt>slug of title</dt><dd>{{default "untitled" .title | slug}}</dd>
  <dt>first item or fallback</dt><dd>{{if .items}}{{coalesce (index .items 0) "nothing"}}{{else}}nothing{{end}}</dd>
  <dt>ternary</dt><dd>{{ternary (not (empty .items)) "yes" "no"}}</dd>
  <dt>hash of title</dt><dd>{{abbrev (sha256 (default "song" .title)) 20}}</dd>
</dl>
{{end}}`, ""},
		".templates/about.tmpl": {`{{define "about"}}
<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="UTF-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>About song</title>
<link rel="stylesheet" href="/demo/style.css">
</head>
<body>
<header><h1>🎵 song</h1></header>
<main>
<h2>About song</h2>
<p>song hosts static web applications in per-tenant silos, serves them with
<code>index.html</code> routing by client header, stores sensitive files
encrypted at rest, and renders pages server-side using the full
<code>html/template</code> feature set.</p>
<p>This page itself is a <code>template route</code> defined in
<code>.song/meta.json</code> under <code>template_routes</code>. Unlike the
<code>/page</code> route (composed from <code>layout.tmpl</code> with block
overrides), this page is a self-contained template — shared block names
(<code>title</code>/<code>content</code>) can only be overridden by one
document in a single parsed set.</p>
<p><a href="/demo/">← back to the demo index</a></p>
</main>
</body>
</html>
{{end}}`, ""},
		"secrets/notes.txt": {"This file is stored as AES-256-GCM ciphertext on disk and decrypted transparently when served. Keep your --secret safe.\n", "encrypt"},
	}

	for rel, pair := range files {
		encrypt := pair[1] == "encrypt"
		if _, err := s.store.Create(silo, rel, pair[0], encrypt); err != nil {
			// A repeated seed just updates files that already exist.
			if _, err = s.store.Update(silo, rel, pair[0], &encrypt); err != nil {
				return fmt.Errorf("seed file %s: %w", rel, err)
			}
		}
	}

	meta := &static.Meta{
		Version: 1,
		TemplateRoutes: map[string]string{
			"page":  "page",
			"about": "about",
		},
		IndexRules: []static.IndexRule{
			{Header: "X-Index-Variant", Value: "mobile", File: "index.mobile.html"},
			{Header: "X-Index-Variant", Value: "desktop", File: "index.desktop.html"},
			{Header: "User-Agent", Pattern: "(?i)mobile|android|iphone", File: "index.mobile.html"},
		},
		Encrypted: map[string]bool{"secrets/notes.txt": true},
	}
	return s.store.SaveMeta(silo, meta)
}
