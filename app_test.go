package main

import (
	"path/filepath"
	"testing"

	"github.com/kingzad/drill/internal/book"
	"github.com/kingzad/drill/internal/progress"
)

func testApp(t *testing.T) *App {
	t.Helper()
	return &App{
		dir:      t.TempDir(),
		progress: progress.New(filepath.Join(t.TempDir(), "progress.json")),
	}
}

func TestCreateBookFillsDefaults(t *testing.T) {
	a := testApp(t)

	created, err := a.CreateBook(book.Book{Title: "Release 1.4.0"})
	if err != nil {
		t.Fatalf("CreateBook: %v", err)
	}
	if created.Version != book.SchemaVersion {
		t.Errorf("version = %d", created.Version)
	}
	if created.ID != "release-1-4-0" {
		t.Errorf("id = %q", created.ID)
	}

	listing, err := a.ListBooks()
	if err != nil {
		t.Fatalf("ListBooks: %v", err)
	}
	if len(listing.Books) != 1 || listing.Books[0].StepCount != 0 {
		t.Fatalf("listing = %+v", listing)
	}
	if listing.Books[0].Path == "" {
		t.Error("listing should include the file path for the UI")
	}
}

func TestAppStepLifecycle(t *testing.T) {
	a := testApp(t)

	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatalf("CreateBook: %v", err)
	}

	for _, title := range []string{"Alpha", "Bravo", "Charlie"} {
		if _, err := a.AddStep("demo", book.Step{Title: title, Command: "echo " + title}); err != nil {
			t.Fatalf("AddStep(%s): %v", title, err)
		}
	}

	got, err := a.GetBook("demo")
	if err != nil {
		t.Fatalf("GetBook: %v", err)
	}
	if len(got.Steps) != 3 {
		t.Fatalf("got %d steps", len(got.Steps))
	}

	// Move the last step to the front.
	if _, err := a.MoveStep("demo", "charlie", 0); err != nil {
		t.Fatalf("MoveStep: %v", err)
	}
	got, _ = a.GetBook("demo")
	if got.Steps[0].ID != "charlie" {
		t.Fatalf("order after move: %s %s %s", got.Steps[0].ID, got.Steps[1].ID, got.Steps[2].ID)
	}

	// Replace a step in place.
	if _, err := a.UpdateStep("demo", "alpha", book.Step{Title: "Alpha (edited)", Command: "echo edited"}); err != nil {
		t.Fatalf("UpdateStep: %v", err)
	}
	got, _ = a.GetBook("demo")
	if idx := got.StepIndex("alpha"); idx < 0 || got.Steps[idx].Title != "Alpha (edited)" {
		t.Fatalf("update did not stick: %+v", got.Steps)
	}
	if got.Steps[got.StepIndex("alpha")].ID != "alpha" {
		t.Error("UpdateStep must keep the id when the payload omits it")
	}

	if _, err := a.DeleteStep("demo", "bravo"); err != nil {
		t.Fatalf("DeleteStep: %v", err)
	}
	got, _ = a.GetBook("demo")
	if len(got.Steps) != 2 {
		t.Fatalf("got %d steps after delete", len(got.Steps))
	}

	if err := a.DeleteBook("demo"); err != nil {
		t.Fatalf("DeleteBook: %v", err)
	}
	listing, _ := a.ListBooks()
	if len(listing.Books) != 0 {
		t.Fatalf("book still listed: %+v", listing.Books)
	}
}

func TestAppRejectsBadEdits(t *testing.T) {
	a := testApp(t)
	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddStep("demo", book.Step{Title: "Alpha", Command: "echo a"}); err != nil {
		t.Fatal(err)
	}

	// Unknown book.
	if _, err := a.GetBook("nope"); err == nil {
		t.Error("expected an error for an unknown book")
	}
	// Unknown step.
	if _, err := a.DeleteStep("demo", "nope"); err == nil {
		t.Error("expected an error for an unknown step")
	}
	// Duplicate book id.
	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err == nil {
		t.Error("expected an error for a duplicate book id")
	}
	// Both payloads at once.
	if _, err := a.AddStep("demo", book.Step{Title: "Bad", Command: "x", Script: "y"}); err == nil {
		t.Error("expected an error for command+script")
	}
	// Out-of-range move.
	if _, err := a.MoveStep("demo", "alpha", 5); err == nil {
		t.Error("expected an error for an out-of-range move")
	}
}

func TestBooksDir(t *testing.T) {
	a := testApp(t)
	if a.BooksDir() == "" {
		t.Error("BooksDir should return the configured folder")
	}
}

func TestCheckmarks(t *testing.T) {
	a := testApp(t)
	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Alpha", "Bravo", "Charlie"} {
		if _, err := a.AddStep("demo", book.Step{Title: title, Command: "true"}); err != nil {
			t.Fatal(err)
		}
	}

	got, err := a.SetStepChecked("demo", "alpha", true)
	if err != nil {
		t.Fatalf("SetStepChecked: %v", err)
	}
	if len(got) != 1 || got[0] != "alpha" {
		t.Fatalf("after tick = %v", got)
	}

	if _, err := a.SetStepChecked("demo", "charlie", true); err != nil {
		t.Fatal(err)
	}
	checked, err := a.CheckedSteps("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) != 2 || checked[0] != "alpha" || checked[1] != "charlie" {
		t.Fatalf("checked = %v", checked)
	}

	got, err = a.SetStepChecked("demo", "alpha", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0] != "charlie" {
		t.Fatalf("after untick = %v", got)
	}

	got, err = a.ResetProgress("demo")
	if err != nil {
		t.Fatalf("ResetProgress: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("after reset = %v", got)
	}
	if checked, _ := a.CheckedSteps("demo"); len(checked) != 0 {
		t.Fatalf("reset did not stick: %v", checked)
	}
}

func TestCheckmarkRejectsUnknownStep(t *testing.T) {
	a := testApp(t)
	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddStep("demo", book.Step{Title: "Alpha"}); err != nil {
		t.Fatal(err)
	}

	if _, err := a.SetStepChecked("demo", "nope", true); err == nil {
		t.Error("expected an error for an unknown step")
	}
	if _, err := a.SetStepChecked("nope", "alpha", true); err == nil {
		t.Error("expected an error for an unknown book")
	}
}

func TestDeletedStepIsNotCounted(t *testing.T) {
	a := testApp(t)
	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatal(err)
	}
	for _, title := range []string{"Alpha", "Bravo"} {
		if _, err := a.AddStep("demo", book.Step{Title: title, Command: "true"}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := a.SetStepChecked("demo", "alpha", true); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetStepChecked("demo", "bravo", true); err != nil {
		t.Fatal(err)
	}

	if _, err := a.DeleteStep("demo", "alpha"); err != nil {
		t.Fatal(err)
	}
	checked, err := a.CheckedSteps("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) != 1 || checked[0] != "bravo" {
		t.Fatalf("deleted step still counted: %v", checked)
	}
}

func TestCheckmarksSurviveRestart(t *testing.T) {
	dir := t.TempDir()
	progressPath := filepath.Join(t.TempDir(), "progress.json")

	first := &App{dir: dir, progress: progress.New(progressPath)}
	if _, err := first.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.AddStep("demo", book.Step{Title: "Alpha", Command: "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := first.SetStepChecked("demo", "alpha", true); err != nil {
		t.Fatal(err)
	}

	second := &App{dir: dir, progress: progress.New(progressPath)}
	checked, err := second.CheckedSteps("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) != 1 || checked[0] != "alpha" {
		t.Fatalf("checkmarks did not survive a restart: %v", checked)
	}
}

func TestDeletingABookClearsItsCheckmarks(t *testing.T) {
	a := testApp(t)
	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddStep("demo", book.Step{Title: "Alpha", Command: "true"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.SetStepChecked("demo", "alpha", true); err != nil {
		t.Fatal(err)
	}

	if err := a.DeleteBook("demo"); err != nil {
		t.Fatal(err)
	}
	if _, err := a.CreateBook(book.Book{ID: "demo", Title: "Demo"}); err != nil {
		t.Fatal(err)
	}
	if _, err := a.AddStep("demo", book.Step{Title: "Alpha", Command: "true"}); err != nil {
		t.Fatal(err)
	}
	checked, err := a.CheckedSteps("demo")
	if err != nil {
		t.Fatal(err)
	}
	if len(checked) != 0 {
		t.Fatalf("a recreated book inherited old ticks: %v", checked)
	}
}
