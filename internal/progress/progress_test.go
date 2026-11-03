package progress

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func newStore(t *testing.T) (*Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "progress.json")
	return New(path), path
}

func TestSetAndRead(t *testing.T) {
	st, _ := newStore(t)
	book := "/books/release.json"

	if got, err := st.Checked(book); err != nil || len(got) != 0 {
		t.Fatalf("empty store: got %v, %v", got, err)
	}

	got, err := st.Set(book, "build", true)
	if err != nil {
		t.Fatalf("Set: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"build"}) {
		t.Fatalf("Set returned %v", got)
	}

	if _, err := st.Set(book, "announce", true); err != nil {
		t.Fatal(err)
	}
	got, err = st.Checked(book)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"announce", "build"}) {
		t.Fatalf("Checked = %v, want sorted ids", got)
	}

	got, err = st.Set(book, "build", false)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, []string{"announce"}) {
		t.Fatalf("after untick = %v", got)
	}
}

func TestPersistsAcrossStores(t *testing.T) {
	st, path := newStore(t)
	if _, err := st.Set("/books/a.json", "s1", true); err != nil {
		t.Fatal(err)
	}

	reopened := New(path)
	got, err := reopened.Checked("/books/a.json")
	if err != nil {
		t.Fatalf("Checked: %v", err)
	}
	if !reflect.DeepEqual(got, []string{"s1"}) {
		t.Fatalf("after reopen = %v", got)
	}
}

func TestBooksAreIndependent(t *testing.T) {
	st, _ := newStore(t)
	if _, err := st.Set("/books/a.json", "s1", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Set("/books/b.json", "s2", true); err != nil {
		t.Fatal(err)
	}

	a, _ := st.Checked("/books/a.json")
	b, _ := st.Checked("/books/b.json")
	if !reflect.DeepEqual(a, []string{"s1"}) || !reflect.DeepEqual(b, []string{"s2"}) {
		t.Fatalf("a=%v b=%v", a, b)
	}
}

func TestReset(t *testing.T) {
	st, _ := newStore(t)
	for _, id := range []string{"a", "b", "c"} {
		if _, err := st.Set("/books/x.json", id, true); err != nil {
			t.Fatal(err)
		}
	}

	if err := st.Reset("/books/x.json"); err != nil {
		t.Fatalf("Reset: %v", err)
	}
	got, err := st.Checked("/books/x.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("after reset = %v", got)
	}

	// Resetting an unknown book is a no-op, not an error.
	if err := st.Reset("/books/nope.json"); err != nil {
		t.Fatalf("Reset unknown: %v", err)
	}
}

func TestResetIsPersisted(t *testing.T) {
	st, path := newStore(t)
	if _, err := st.Set("/books/x.json", "a", true); err != nil {
		t.Fatal(err)
	}
	if err := st.Reset("/books/x.json"); err != nil {
		t.Fatal(err)
	}

	got, err := New(path).Checked("/books/x.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("reset did not reach the file: %v", got)
	}
}

func TestUncheckingTheLastStepPersists(t *testing.T) {
	st, path := newStore(t)
	if _, err := st.Set("/books/x.json", "a", true); err != nil {
		t.Fatal(err)
	}
	if _, err := st.Set("/books/x.json", "a", false); err != nil {
		t.Fatal(err)
	}

	got, err := New(path).Checked("/books/x.json")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 {
		t.Fatalf("untick was not written: %v", got)
	}
}

func TestCorruptFileIsTreatedAsEmpty(t *testing.T) {
	st, path := newStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("{ not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	got, err := st.Checked("/books/x.json")
	if err != nil {
		t.Fatalf("a corrupt progress file must not break the app: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("got %v", got)
	}

	// Writing recovers the file.
	if _, err := st.Set("/books/x.json", "a", true); err != nil {
		t.Fatalf("Set after corruption: %v", err)
	}
	got, _ = New(path).Checked("/books/x.json")
	if !reflect.DeepEqual(got, []string{"a"}) {
		t.Fatalf("got %v", got)
	}
}

func TestFileIsWrittenAtomically(t *testing.T) {
	st, path := newStore(t)
	for i := 0; i < 3; i++ {
		if _, err := st.Set("/books/x.json", "a", true); err != nil {
			t.Fatal(err)
		}
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "progress.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only progress.json, found %v", names)
	}
}

func TestDefaultPathIsUnderConfigDir(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", "/tmp/xdg-test")
	path, err := DefaultPath()
	if err != nil {
		t.Fatalf("DefaultPath: %v", err)
	}
	want := filepath.Join("/tmp/xdg-test", "drill", "progress.json")
	if path != want {
		t.Fatalf("DefaultPath = %q, want %q", path, want)
	}
}
