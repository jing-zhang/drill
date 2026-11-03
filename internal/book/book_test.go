package book

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func validBook() *Book {
	return &Book{
		Version: SchemaVersion,
		ID:      "release-1-4-0",
		Title:   "Release 1.4.0",
		Defaults: Defaults{
			Shell:          "bash",
			TimeoutSeconds: 600,
		},
		Steps: []Step{
			{ID: "test", Title: "Run tests", Command: "go test ./..."},
			{ID: "build", Title: "Build", Script: "set -e\nmake\n"},
			{ID: "announce", Title: "Announce", Description: "Post in #releases."},
		},
	}
}

func TestValidateAcceptsValidBook(t *testing.T) {
	if err := validBook().Validate(); err != nil {
		t.Fatalf("valid book rejected: %v", err)
	}
}

func TestValidateAllowsEmptyBook(t *testing.T) {
	b := &Book{Version: SchemaVersion, ID: "empty", Title: "Empty", Steps: []Step{}}
	if err := b.Validate(); err != nil {
		t.Fatalf("empty book rejected: %v", err)
	}
}

func TestValidateProblems(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Book)
		wantSub string
	}{
		{"version", func(b *Book) { b.Version = 2 }, "version: want 1, got 2"},
		{"book id", func(b *Book) { b.ID = "Release 1.4" }, "id: \"Release 1.4\" is not a valid id"},
		{"empty title", func(b *Book) { b.Title = "  " }, "title: required"},
		{"no steps key", func(b *Book) { b.Steps = nil }, "steps: required"},
		{"step id", func(b *Book) { b.Steps[0].ID = "Bad Id" }, "steps[0].id: \"Bad Id\" is not a valid id"},
		{"duplicate id", func(b *Book) { b.Steps[1].ID = "test" }, `steps[1].id: duplicate of steps[0] ("test")`},
		{"missing step title", func(b *Book) { b.Steps[0].Title = "" }, "steps[0].title: required"},
		{"both payloads", func(b *Book) { b.Steps[0].Script = "echo hi" }, "steps[0]: has both command and script"},
		{"neither payload is fine", func(b *Book) { b.Steps[0].Command = "" }, ""},
		{"bad shell", func(b *Book) { b.Steps[0].Shell = "fish" }, `steps[0].shell: unknown shell "fish"`},
		{"bad default shell", func(b *Book) { b.Defaults.Shell = "fish" }, `defaults.shell: unknown shell "fish"`},
		{"negative timeout", func(b *Book) { b.Steps[0].TimeoutSeconds = -1 }, "steps[0].timeoutSeconds: must not be negative"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			b := validBook()
			tc.mutate(b)
			err := b.Validate()
			if tc.wantSub == "" {
				if err != nil {
					t.Fatalf("step without a payload should be valid: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected a validation error")
			}
			if !strings.Contains(err.Error(), tc.wantSub) {
				t.Fatalf("error %q does not contain %q", err.Error(), tc.wantSub)
			}
		})
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	b := validBook()
	b.ID = "BAD ID"
	b.Steps[0].Shell = "fish"
	b.Steps[1].Command = "make" // steps[1] already has a script
	b.Steps[1].Title = ""

	err := b.Validate()
	if err == nil {
		t.Fatal("expected a validation error")
	}
	for _, want := range []string{"id:", "steps[0].shell", "steps[1]: has both"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("error is missing %q:\n%s", want, err)
		}
	}
	if n := strings.Count(err.Error(), "\n") + 1; n < 3 {
		t.Errorf("expected at least 3 problems, got %d:\n%s", n, err)
	}
}

func TestSlug(t *testing.T) {
	tests := map[string]string{
		"Release 1.4.0":       "release-1-4-0",
		"  Onboarding  ":      "onboarding",
		"a/b\\c":              "a-b-c",
		"---":                 "",
		"":                    "",
		"Ünïcode—title":       "n-code-title",
		"keep_under.score":    "keep_under-score",
		"Mixed CASE  Already": "mixed-case-already",
	}
	for in, want := range tests {
		if got := Slug(in); got != want {
			t.Errorf("Slug(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestValidID(t *testing.T) {
	valid := []string{"a", "release-1-4-0", "a_b-c", "0day", strings.Repeat("a", MaxIDLen)}
	for _, id := range valid {
		if !ValidID(id) {
			t.Errorf("ValidID(%q) = false, want true", id)
		}
	}
	invalid := []string{"", "-lead", ".dot", "has.dot", "Upper", "has space", "slash/", strings.Repeat("a", MaxIDLen+1)}
	for _, id := range invalid {
		if ValidID(id) {
			t.Errorf("ValidID(%q) = true, want false", id)
		}
	}
}

func TestUniqueStepID(t *testing.T) {
	b := &Book{Steps: []Step{{ID: "build"}, {ID: "build-2"}}}
	if got := UniqueStepID(b, "Build"); got != "build-3" {
		t.Errorf("got %q, want build-3", got)
	}
	if got := UniqueStepID(b, "Run tests"); got != "run-tests" {
		t.Errorf("got %q, want run-tests", got)
	}
	if got := UniqueStepID(b, "!!!"); got != "step" {
		t.Errorf("got %q, want step", got)
	}
}

func TestSaveLoadRoundTrip(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "release.json")

	b := validBook()
	b.Steps[0].Command = `echo "a > b" && test -n "$HOME"`
	b.Steps[0].Description = "Uses `code` and **bold**."
	if err := Save(path, b); err != nil {
		t.Fatalf("Save: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !reflect.DeepEqual(got, b) {
		t.Fatalf("round trip mismatch:\n got %+v\nwant %+v", got, b)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, `\u003e`) || strings.Contains(text, `\u0026`) {
		t.Errorf("HTML escaping leaked into the file:\n%s", text)
	}
	if !strings.Contains(text, `a > b`) || !strings.Contains(text, `&& test -n`) {
		t.Errorf("command was mangled:\n%s", text)
	}
	if !strings.HasSuffix(text, "}\n") {
		t.Errorf("file must end with a newline")
	}
	if !strings.Contains(text, "\n  \"id\": \"release-1-4-0\"") {
		t.Errorf("expected 2-space indentation, got:\n%s", text)
	}
}

func TestSaveIsAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "book.json")
	// Write twice; a leftover temp file would show up in the listing.
	for i := 0; i < 3; i++ {
		if err := Save(path, validBook()); err != nil {
			t.Fatalf("Save #%d: %v", i, err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != "book.json" {
		var names []string
		for _, e := range entries {
			names = append(names, e.Name())
		}
		t.Fatalf("expected only book.json, found %v", names)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Errorf("mode = %v, want 0644", info.Mode().Perm())
	}
}

func TestSaveRejectsInvalidBook(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	b := validBook()
	b.Version = 99
	if err := Save(path, b); err == nil {
		t.Fatal("expected validation error")
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("invalid book must not be written")
	}
}

func TestLoadRejectsUnknownField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "typo.json")
	body := `{"version":1,"id":"typo","title":"Typo","steps":[{"id":"s","title":"S","comand":"echo hi"}]}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil {
		t.Fatal("expected unknown field to be rejected")
	}
	if !strings.Contains(err.Error(), "comand") {
		t.Errorf("error should name the unknown field: %v", err)
	}
}

func TestLoadReportsPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := Load(path)
	if err == nil || !strings.Contains(err.Error(), path) {
		t.Fatalf("error should include the path, got %v", err)
	}
}

func TestLoadFillsEmptySteps(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.json")
	body := `{"version":1,"id":"empty","title":"Empty"}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	b, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if b.Steps == nil || len(b.Steps) != 0 {
		t.Fatalf("steps should load as an empty slice, got %#v", b.Steps)
	}
}
