# song

**Go middleware for serving static web resources with template processing and web component support.**

song is a Go middleware built entirely on the Go standard library. Where client-side code is needed, it uses only baseline HTML, CSS, and JavaScript. It enables serving static web resources, upserting them via HTTP, processing Go templates, and exposing HTML template tags as reusable web components accessible across any HTML file.

---

## Core Capabilities

- **Standard library only**: Pure Go standard library on the server. Client-side uses only baseline HTML, CSS, and JavaScript where needed.
- **Middleware design**: `Song.Middleware(next)` serves only what song owns and delegates all other paths to `next`, making it easy to integrate into existing Go HTTP servers.
- **Static resource access**: Retrieve static web resources via `GET` requests.
- **Resource upsertion**: Create or update static web resources via `POST` commands.
- **Template processing**: Automated processing of Go templates in both text format (`text/template`, [see docs](https://pkg.go.dev/text/template)) and HTML format (`html/template`, [see docs](https://pkg.go.dev/html/template)).
- **Web component support**: Any HTML template tags defined by the system are accessible from any provided HTML file, implementing Web Components as defined by [MDN Web Components](https://developer.mozilla.org/en-US/docs/Web/API/Web_components).

---

## Usage

### As middleware

```go
mux := http.NewServeMux()
mux.Handle("/", songSvc.Middleware(myAppHandler))
http.ListenAndServe(":8080", mux)
```

See [`examples/middleware`](examples/middleware) for a complete example.

### Static resources

- `GET /<path>` - Access static web resources served by song.
- `POST /<path>` - Upsert (create or update) static web resources. The request body contains the resource content.

### Templates

Templates are processed automatically using Go's standard templating packages:

- **Text templates**: Use `text/template` for non-HTML text processing.
- **HTML templates**: Use `html/template` for HTML with automatic contextual escaping.

Template definitions and their associated tags are registered in the system and made available to any HTML file, enabling reusable web components that can be instantiated across your application.

---

## Notes

All functionality is implemented using only the Go standard library plus baseline HTML, CSS, and JavaScript where client-side behavior is required. No external frameworks or dependencies are used.