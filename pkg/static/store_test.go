package static

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testSecret = "test-secret-key-0123456789abcdefghij"

func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	st, err := NewStore(root, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func mustCreateSilo(t *testing.T, s *Store, name string) {
	t.Helper()
	if err := s.CreateSilo(name); err != nil {
		t.Fatalf("create silo %q: %v", name, err)
	}
}

func TestSiloValidation(t *testing.T) {
	s := newTestStore(t)
	bad := []string{"", "..", ".", "a/b", "a\\b", ".hidden", ".song",
		"api", "admin", "health", "x y", "-lead", "trail-", "x\x00y"}
	for _, name := range bad {
		if err := s.CreateSilo(name); err == nil {
			t.Errorf("expected error creating silo %q", name)
		}
	}
	ok := []string{"demo", "my-app", "site_1", "A.B-c2"}
	for _, name := range ok {
		if err := s.CreateSilo(name); err != nil {
			t.Errorf("unexpected error creating silo %q: %v", name, err)
		}
	}
	// Duplicate must fail.
	if err := s.CreateSilo("demo"); err == nil || !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists for duplicate silo, got %v", err)
	}
}

func TestStoreRootCreation(t *testing.T) {
	root := filepath.Join(t.TempDir(), "nested", "store")
	s, err := NewStore(root, testSecret)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Root()); err != nil {
		t.Fatalf("root not created: %v", err)
	}
	if s.Root() != filepath.Clean(root) {
		t.Fatalf("root mismatch: got %q want %q", s.Root(), filepath.Clean(root))
	}
	if _, err := NewStore(root, "short"); err == nil {
		t.Fatal("expected error for short secret")
	}
}

func TestFileCRUD(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")

	// Create + read.
	fi, err := s.Create("demo", "css/app.css", "body { color: red }", false)
	if err != nil {
		t.Fatal(err)
	}
	if fi.Encrypted {
		t.Fatal("file must not be encrypted")
	}
	if fi.Path != "/css/app.css" {
		t.Fatalf("path mismatch: %q", fi.Path)
	}
	entry, err := s.Read("demo", "css/app.css", true)
	if err != nil {
		t.Fatal(err)
	}
	if string(entry.Content) != "body { color: red }" {
		t.Fatalf("content mismatch: %q", entry.Content)
	}

	// Duplicate create fails; update works.
	if _, err := s.Create("demo", "css/app.css", "x", false); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	if _, err := s.Update("demo", "css/app.css", "body { color: blue }", nil); err != nil {
		t.Fatal(err)
	}
	entry, _ = s.Read("demo", "css/app.css", true)
	if string(entry.Content) != "body { color: blue }" {
		t.Fatalf("update failed: %q", entry.Content)
	}

	// Delete.
	if err := s.Delete("demo", "css/app.css"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Read("demo", "css/app.css", true); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound after delete, got %v", err)
	}
	if err := s.Delete("demo", "css/app.css"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound deleting twice, got %v", err)
	}
}

func TestEncryptedFiles(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "vault")

	if _, err := s.Create("vault", "secrets/note.txt", "top secret", true); err != nil {
		t.Fatal(err)
	}

	// On-disk bytes must not contain the plaintext.
	abs, err := s.filePath("vault", "secrets/note.txt")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "top secret") {
		t.Fatal("plaintext leaked to disk")
	}

	// Read decrypts transparently.
	entry, err := s.Read("vault", "secrets/note.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if !entry.Encrypted {
		t.Fatal("file must be flagged encrypted")
	}
	if string(entry.Content) != "top secret" {
		t.Fatalf("decrypted content mismatch: %q", entry.Content)
	}

	// Update keeping encryption.
	if _, err := s.Update("vault", "secrets/note.txt", "updated secret", nil); err != nil {
		t.Fatal(err)
	}
	entry, _ = s.Read("vault", "secrets/note.txt", true)
	if string(entry.Content) != "updated secret" {
		t.Fatalf("update of encrypted file failed: %q", entry.Content)
	}

	// Update switching to plain.
	keep := false
	if _, err := s.Update("vault", "secrets/note.txt", "now plain", &keep); err != nil {
		t.Fatal(err)
	}
	entry, _ = s.Read("vault", "secrets/note.txt", true)
	if entry.Encrypted {
		t.Fatal("file should no longer be encrypted")
	}
	raw, _ = os.ReadFile(abs)
	if !strings.Contains(string(raw), "now plain") {
		t.Fatal("plain file should be readable on disk")
	}
	if s.isEncrypted("vault", "secrets/note.txt") {
		t.Fatal("meta should no longer mark the file encrypted")
	}

	// Open() serves decrypted content.
	if _, err := s.Create("vault", "secret2.txt", "abc123", true); err != nil {
		t.Fatal(err)
	}
	ent, err := s.Open("vault", "secret2.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer ent.Reader.Close()
	if !ent.Encrypted {
		t.Fatal("Open must flag encrypted")
	}
	if ent.Size != 6 {
		t.Fatalf("logical size mismatch: %d", ent.Size)
	}
}

func TestPathTraversalRejected(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")

	for _, rel := range []string{"../evil", "a/../../evil", "..\\evil",
		".song/meta.json", ".song/../x", "sub/../../x", "a\x00b"} {
		if _, err := s.filePath("demo", rel); err == nil {
			t.Errorf("expected error for rel %q", rel)
		} else if !errors.Is(err, ErrTraversal) && !errors.Is(err, ErrReserved) {
			t.Errorf("rel %q: expected ErrTraversal/ErrReserved, got %v", rel, err)
		}
		if _, err := s.Create("demo", rel, "x", false); err == nil {
			t.Errorf("expected error creating rel %q", rel)
		}
	}
	// Leading slashes are fine and normalize away.
	if fp, err := s.filePath("demo", "/index.html"); err != nil {
		t.Fatalf("leading slash should be allowed: %v", err)
	} else if !strings.HasSuffix(fp, "index.html") {
		t.Fatal("unexpected path")
	}
}

func TestListSkipsHiddenAndMeta(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")
	if _, err := s.Create("demo", "index.html", "<h1>hi</h1>", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("demo", "nested/style.css", "a{}", false); err != nil {
		t.Fatal(err)
	}
	files, err := s.List("demo", "")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range files {
		names[f.Name] = true
	}
	if !names["index.html"] || !names["nested"] {
		t.Fatalf("missing expected entries: %v", names)
	}
	if names[".song"] {
		t.Fatal(".song metadata tree must be hidden from listings")
	}
	sub, err := s.List("demo", "nested")
	if err != nil {
		t.Fatal(err)
	}
	if len(sub) != 1 || sub[0].Name != "style.css" {
		t.Fatalf("unexpected sub listing: %+v", sub)
	}
}

func TestListFileOnFileFails(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")
	if _, err := s.Create("demo", "a.txt", "x", false); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List("demo", "a.txt"); !errors.Is(err, ErrIsFile) {
		t.Fatalf("expected ErrIsFile, got %v", err)
	}
}

func TestResolveIndexByHeader(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")
	for _, f := range []string{"index.html", "index.mobile.html", "index.desktop.html"} {
		if _, err := s.Create("demo", f, "<h1>"+f+"</h1>", false); err != nil {
			t.Fatal(err)
		}
	}
	m, err := s.Meta("demo")
	if err != nil {
		t.Fatal(err)
	}
	m.IndexRules = []IndexRule{
		{Header: "X-Index-Variant", Value: "mobile", File: "index.mobile.html"},
		{Header: "X-Index-Variant", Value: "desktop", File: "index.desktop.html", Redirect: true},
		{Header: "User-Agent", Pattern: "(?i)iphone|android", File: "index.mobile.html"},
	}
	if err := s.SaveMeta("demo", m); err != nil {
		t.Fatal(err)
	}

	hdr := http.Header{}

	// Default: no header -> index.html.
	f, redir, err := s.ResolveIndex("demo", "", hdr)
	if err != nil {
		t.Fatal(err)
	}
	if f != "index.html" || redir {
		t.Fatalf("expected index.html, got %q redir=%v", f, redir)
	}

	// Explicit variant header.
	hdr.Set("X-Index-Variant", "mobile")
	f, redir, err = s.ResolveIndex("demo", "", hdr)
	if err != nil {
		t.Fatal(err)
	}
	if f != "index.mobile.html" || redir {
		t.Fatalf("expected index.mobile.html, got %q redir=%v", f, redir)
	}

	// Redirect rule honored.
	hdr.Set("X-Index-Variant", "desktop")
	f, redir, err = s.ResolveIndex("demo", "", hdr)
	if err != nil {
		t.Fatal(err)
	}
	if f != "index.desktop.html" || !redir {
		t.Fatalf("expected index.desktop.html with redirect, got %q redir=%v", f, redir)
	}

	// User-Agent pattern.
	hdr = http.Header{}
	hdr.Set("User-Agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_0 like Mac OS X)")
	f, _, err = s.ResolveIndex("demo", "", hdr)
	if err != nil {
		t.Fatal(err)
	}
	if f != "index.mobile.html" {
		t.Fatalf("expected mobile via User-Agent, got %q", f)
	}

	// Subdirectory resolution with rules present.
	if _, err := s.Create("demo", "sub/index.html", "x", false); err != nil {
		t.Fatal(err)
	}
	hdr = http.Header{}
	f, _, err = s.ResolveIndex("demo", "sub", hdr)
	if err != nil {
		t.Fatal(err)
	}
	if f != "index.html" {
		t.Fatalf("expected sub/index.html, got %q", f)
	}

	// Missing index document.
	hdr = http.Header{}
	if _, _, err := s.ResolveIndex("demo", "does-not-exist", hdr); err == nil {
		t.Fatal("expected error for missing index")
	}
}

func TestTemplateRouteMeta(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")
	if err := s.SetTemplateRoute("demo", "page", "page"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetTemplateRoute("demo", "about", "about"); err != nil {
		t.Fatal(err)
	}
	m, err := s.Meta("demo")
	if err != nil {
		t.Fatal(err)
	}
	if m.TemplateRoutes["page"] != "page" || m.TemplateRoutes["about"] != "about" {
		t.Fatalf("template routes not saved: %+v", m.TemplateRoutes)
	}
	if err := s.SetTemplateRoute("demo", "page", ""); err != nil {
		t.Fatal(err)
	}
	m, _ = s.Meta("demo")
	if _, ok := m.TemplateRoutes["page"]; ok {
		t.Fatal("route should have been removed")
	}
}

func TestRemoveSilo(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")
	if !s.HasSilo("demo") {
		t.Fatal("silo should exist")
	}
	if err := s.RemoveSilo("demo"); err != nil {
		t.Fatal(err)
	}
	if s.HasSilo("demo") {
		t.Fatal("silo should be gone")
	}
	if err := s.RemoveSilo("demo"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}

func TestSiloListOrdered(t *testing.T) {
	s := newTestStore(t)
	for _, name := range []string{"zeta", "alpha", "demo"} {
		mustCreateSilo(t, s, name)
	}
	names, err := s.ListSilos()
	if err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(names, ",")
	if joined != "alpha,demo,zeta" {
		t.Fatalf("expected sorted silos, got %q", joined)
	}
}

func TestMetaMissingReturnsDefault(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")
	m, err := s.Meta("demo")
	if err != nil {
		t.Fatal(err)
	}
	if m.TemplateRoutes == nil || m.Encrypted == nil {
		t.Fatal("default meta must have initialized maps")
	}
}
