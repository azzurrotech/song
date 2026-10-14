package static

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	st, err := NewStore(root)
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
	s, err := NewStore(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(s.Root()); err != nil {
		t.Fatalf("root not created: %v", err)
	}
	if s.Root() != filepath.Clean(root) {
		t.Fatalf("root mismatch: got %q want %q", s.Root(), filepath.Clean(root))
	}
}

func TestFileCRUD(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")

	// Create + read.
	fi, err := s.Create("demo", "css/app.css", "body { color: red }")
	if err != nil {
		t.Fatal(err)
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
	if _, err := s.Create("demo", "css/app.css", "x"); !errors.Is(err, ErrExists) {
		t.Fatalf("expected ErrExists, got %v", err)
	}
	if _, err := s.Update("demo", "css/app.css", "body { color: blue }"); err != nil {
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

// TestFilesStoredAsPlainBytes confirms the store keeps file content verbatim on
// disk (song's README describes plain static hosting).
func TestFilesStoredAsPlainBytes(t *testing.T) {
	s := newTestStore(t)
	mustCreateSilo(t, s, "demo")
	if _, err := s.Create("demo", "note.txt", "hello world"); err != nil {
		t.Fatal(err)
	}
	abs, err := s.filePath("demo", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(abs)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw) != "hello world" {
		t.Fatalf("content must be stored verbatim, got %q", raw)
	}
	entry, err := s.Read("demo", "note.txt", true)
	if err != nil {
		t.Fatal(err)
	}
	if string(entry.Content) != "hello world" {
		t.Fatalf("read content mismatch: %q", entry.Content)
	}
	// Open() exposes the same bytes and their size.
	ent, err := s.Open("demo", "note.txt")
	if err != nil {
		t.Fatal(err)
	}
	defer ent.Reader.Close()
	if ent.Size != int64(len("hello world")) {
		t.Fatalf("size mismatch: %d", ent.Size)
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
		if _, err := s.Create("demo", rel, "x"); err == nil {
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
	if _, err := s.Create("demo", "index.html", "<h1>hi</h1>"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Create("demo", "nested/style.css", "a{}"); err != nil {
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
	if _, err := s.Create("demo", "a.txt", "x"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.List("demo", "a.txt"); !errors.Is(err, ErrIsFile) {
		t.Fatalf("expected ErrIsFile, got %v", err)
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
	if m.TemplateRoutes == nil {
		t.Fatal("default meta must have initialized maps")
	}
}
