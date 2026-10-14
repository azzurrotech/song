# tmpl Security

`azzurrotech/song/pkg/tmpl` renders server-side HTML from per-silo template
files with Go's `html/template`. It is standard-library-only Go. This document
describes the protections it implements and the ones it deliberately does not.

## Assets protected

1. **Rendered output** — HTML/JS/CSS/URLs must not be injectable by data values.
2. **Host process integrity** — template parsing and execution must not be a
   path-traversal or arbitrary-file-read primitive.

## Threat model

- **Data-driven XSS.** Attacker-controlled values passed as template data
  (`?name=` query parameters) are placed into HTML, attributes, JS, CSS or URLs.
- **Template-name abuse.** A caller supplies a template name or silo name.
- **Malicious template files.** A tenant writes template files into their own
  silo (this is expected and by design; a template author is trusted within
  their silo).
- **Breakout via JSON embedding.** Data serialized into a `<script>` block.

## Mitigations implemented

- **Contextual auto-escaping.** `Render` executes through `html/template`
  (`t.ExecuteTemplate`), so values are escaped according to their context (HTML
  text, attribute, JS, CSS, URL) unless explicitly typed. The tests assert that
  plain `"<b>evil</b>"` is HTML-escaped and that a plain string in a JS context
  is escaped.
- **Safe JSON in scripts.** `json` returns `template.JS` built from
  `json.Marshal`, whose default HTML escaping turns `</script>` into
  `\u003c/script\u003e`; the test verifies an embedded `</script><script>` cannot
  break out.
- **Opt-in escaping bypass.** Only the explicit helpers `html`, `htmlAttr`,
  `js`, `jsStr`, `css`, `url`, `srcset` produce unescaped typed output; they are
  named so their use is visible in the template.
- **Parse errors fail closed.** A malformed template makes `load` return an
  error; `Render` never executes a partially parsed set.
- **No arbitrary template name resolution.** `Render` uses
  `t.Lookup(name)`/`ExecuteTemplate`; an undefined name returns an error and the
  template is only read from `<root>/<silo>/.templates`.
- **Silo path confinement.** The directory is `filepath.Join(root, silo,
  ".templates")`; the silo name is validated upstream by
  `pkg/static.validateSiloName`, and only files whose base name is used for
  `t.New(name)` are parsed.

## Honest limits / non-claims

- **Template authors are trusted for their own silo.** A template may call the
  unescaped helpers (`html`, `js`, …) and inject raw markup intentionally. song
  does not sandbox template execution; a template author can read the data they
  are given and render anything.
- **`.templates` is writable by anyone who can POST into the silo.** The
  resource-upsert surface accepts any path inside a silo, so any client that can
  reach song can create templates there, including templates that bypass
  escaping. In this build song has no authentication (see the root security
  notes), so do not expose it. Drop template files onto disk directly when only
  the host should author them.
- **Host-injected functions are powerful.** `AddFuncs`/`SetFuncs` may register
  functions with side effects; only the host application should supply them.
- **No sandbox / no execution timeout.** A pathological template or a slow
  custom function can consume CPU; there is no render timeout or memory cap.
- **mtime-based cache only.** A file changed without an mtime bump is not
  reloaded; this is a correctness, not a security, property.
- **No Content-Security-Policy is set by this package.** A host or reverse proxy
  should add one.
- **Concurrent first-parses may duplicate work** (parsing occurs outside the
  lock); this does not weaken escaping.

## Dependencies

Standard library only. Imports: `crypto/sha256`, `encoding/base64`,
`encoding/hex`, `encoding/json`, `errors`, `fmt`, `html/template`, `io`,
`net/url`, `os`, `path/filepath`, `reflect`, `regexp`, `sort`, `strconv`,
`strings`, `sync`, `time`, `unicode`.

## Reporting

Report vulnerabilities privately to `security@azzurro.tech`. Do not open a
public issue for a live vulnerability.
