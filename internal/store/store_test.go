package store

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/kingzad/drill/internal/book"
)

func newBook(id string) *book.Book {
	return &book.Book{
		Version: book.SchemaVersion,
		ID:      id,
		Title:   strings.ToUpper(id[:1]) + id[1:],
		Steps:   []book.Step{},
	}
}

func write(t *testing.T, dir, name string, b *book.Book) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := book.Save(path, b); err != nil {
		t.Fatalf("Save %s: %v", path, err)
	}
	return path
}

func TestListSortsAndReportsProblems(t *testing.T) {
	dir := t.TempDir()
	st, err := New(dir)
	if err != nil {
		t.Fatal(err)
	}
	write(t, dir, "zeta.json", newBook("zeta"))
	write(t, dir, "alpha.json", newBook("alpha"))

	broken := filepath.Join(dir, "broken.json")
	if err := os.WriteFile(broken, []byte(`{"version":1}`), 0o644); err != nil {
		t.Fatal(err)
	}
	// Files that are not books must be ignored entirely.
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("hi"), 0o644); err != nil {
		t.Fatal(err)
	}

	listing, err := st.List()
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if len(listing.Entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(listing.Entries))
	}
	if listing.Entries[0].Book.ID != "alpha" || listing.Entries[1].Book.ID != "zeta" {
		t.Errorf("wrong order: %s, %s", listing.Entries[0].Book.ID, listing.Entries[1].Book.ID)
	}
	if len(listing.Problems) != 1 || listing.Problems[0].Path != broken {
		t.Fatalf("problems = %+v", listing.Problems)
	}
	if !strings.Contains(listing.Problems[0].Error, "broken.json") {
		t.Errorf("problem should carry the path: %q", listing.Problems[0].Error)
	}
}

func TestCreateRefusesToOverwrite(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(dir)

	b := newBook("dup")
	path, err := st.Create(b)
	if err != nil {
		t.Fatalf("first Create: %v", err)
	}
	if filepath.Dir(path) != st.Dir {
		t.Errorf("created in %s, want %s", path, st.Dir)
	}
	if _, err := st.Create(b); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("second Create should fail, got %v", err)
	}
}

func TestResolveByIDAndPath(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(dir)
	write(t, dir, "demo.json", newBook("demo"))

	byID, err := st.Resolve("demo")
	if err != nil {
		t.Fatalf("Resolve by id: %v", err)
	}
	if byID.Book.ID != "demo" {
		t.Errorf("got %q", byID.Book.ID)
	}

	byPath, err := st.Resolve(filepath.Join(dir, "demo.json"))
	if err != nil {
		t.Fatalf("Resolve by path: %v", err)
	}
	if byPath.Path != byID.Path {
		t.Errorf("paths differ: %q vs %q", byPath.Path, byID.Path)
	}

	// A file renamed away from <id>.json still resolves by the id inside it.
	if err := os.Rename(filepath.Join(dir, "demo.json"), filepath.Join(dir, "renamed.json")); err != nil {
		t.Fatal(err)
	}
	renamed, err := st.Resolve("demo")
	if err != nil {
		t.Fatalf("Resolve after rename: %v", err)
	}
	if renamed.Path != filepath.Join(dir, "renamed.json") {
		t.Errorf("resolved %q", renamed.Path)
	}

	if _, err := st.Resolve("missing"); err == nil {
		t.Error("expected an error for an unknown book")
	}
	if _, err := st.Resolve("../escape"); err == nil {
		t.Error("expected an error for a non-id reference")
	}
}

func TestSaveToAndDelete(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(dir)
	path := write(t, dir, "demo.json", newBook("demo"))

	e, err := st.Resolve("demo")
	if err != nil {
		t.Fatal(err)
	}
	e.Book.Title = "Renamed"
	if err := st.SaveTo(path, e.Book); err != nil {
		t.Fatalf("SaveTo: %v", err)
	}
	reloaded, err := book.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Title != "Renamed" {
		t.Errorf("title = %q", reloaded.Title)
	}

	if err := st.Delete("demo"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Error("file still exists after Delete")
	}
	if err := st.Delete("demo"); err == nil {
		t.Error("deleting twice should fail")
	}
}

func TestPathForStaysInsideDir(t *testing.T) {
	dir := t.TempDir()
	st, _ := New(dir)
	if got := st.PathFor("demo"); got != filepath.Join(st.Dir, "demo.json") {
		t.Errorf("PathFor = %q", got)
	}
}
