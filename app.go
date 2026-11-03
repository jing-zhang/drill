package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"

	"github.com/kingzad/drill/internal/book"
	"github.com/kingzad/drill/internal/engine"
	"github.com/kingzad/drill/internal/progress"
	"github.com/kingzad/drill/internal/store"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App is the desktop bridge. Every exported method is callable from the UI as
// window.go.main.App.<Method>. The CLI does not go through this file: it calls
// the same store and engine packages directly.
type App struct {
	ctx context.Context

	mu       sync.Mutex
	dir      string
	cancel   context.CancelFunc // set while a run is in progress
	progress *progress.Store    // ticked-off steps, stored outside the books
}

// BookSummary is the sidebar representation of a book.
type BookSummary struct {
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description"`
	Tags        []string `json:"tags"`
	StepCount   int      `json:"stepCount"`
	Path        string   `json:"path"`
}

// Listing is everything the UI needs to draw the sidebar.
type Listing struct {
	Dir      string          `json:"dir"`
	Books    []BookSummary   `json:"books"`
	Problems []store.Problem `json:"problems"`
}

// NewApp creates the desktop application.
func NewApp() *App { return &App{} }

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.mu.Lock()
	a.dir = resolveBooksDir()
	a.mu.Unlock()

	if path, err := progress.DefaultPath(); err == nil {
		a.progress = progress.New(path)
	}
}

func (a *App) beforeClose(ctx context.Context) bool {
	a.StopRun()
	return false
}

// BooksDir reports the folder currently being edited.
func (a *App) BooksDir() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.dir
}

func (a *App) books() (*store.Store, error) {
	a.mu.Lock()
	dir := a.dir
	a.mu.Unlock()
	if dir == "" {
		return nil, errors.New("no books folder selected")
	}
	return store.New(dir)
}

func (a *App) entry(id string) (store.Entry, error) {
	st, err := a.books()
	if err != nil {
		return store.Entry{}, err
	}
	return st.Resolve(id)
}

// ListBooks scans the books folder for runbooks.
func (a *App) ListBooks() (Listing, error) {
	st, err := a.books()
	if err != nil {
		return Listing{}, err
	}
	listing, err := st.List()
	if err != nil {
		return Listing{}, err
	}

	out := Listing{Dir: st.Dir, Books: []BookSummary{}, Problems: []store.Problem{}}
	out.Problems = append(out.Problems, listing.Problems...)
	for _, e := range listing.Entries {
		out.Books = append(out.Books, BookSummary{
			ID:          e.Book.ID,
			Title:       e.Book.Title,
			Description: e.Book.Description,
			Tags:        e.Book.Tags,
			StepCount:   len(e.Book.Steps),
			Path:        e.Path,
		})
	}
	return out, nil
}

// GetBook loads one book by id.
func (a *App) GetBook(id string) (*book.Book, error) {
	e, err := a.entry(id)
	if err != nil {
		return nil, err
	}
	return e.Book, nil
}

// CreateBook writes a new book file.
func (a *App) CreateBook(b book.Book) (*book.Book, error) {
	if b.Version == 0 {
		b.Version = book.SchemaVersion
	}
	if b.ID == "" {
		b.ID = book.Slug(b.Title)
	}
	if b.Steps == nil {
		b.Steps = []book.Step{}
	}
	st, err := a.books()
	if err != nil {
		return nil, err
	}
	if _, err := st.Create(&b); err != nil {
		return nil, err
	}
	return &b, nil
}

// UpdateBook writes an existing book back to its file.
func (a *App) UpdateBook(b book.Book) (*book.Book, error) {
	e, err := a.entry(b.ID)
	if err != nil {
		return nil, err
	}
	if b.Steps == nil {
		b.Steps = []book.Step{}
	}
	if err := book.Save(e.Path, &b); err != nil {
		return nil, err
	}
	return &b, nil
}

// DeleteBook removes a book file.
func (a *App) DeleteBook(id string) error {
	e, err := a.entry(id)
	if err != nil {
		return err
	}
	if err := os.Remove(e.Path); err != nil {
		return err
	}
	if a.progress != nil {
		_ = a.progress.Reset(e.Path)
	}
	return nil
}

// AddStep appends a step to a book.
func (a *App) AddStep(bookID string, step book.Step) (*book.Book, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}
	if step.ID == "" {
		step.ID = book.UniqueStepID(e.Book, step.Title)
	}
	if e.Book.HasStepID(step.ID) {
		return nil, fmt.Errorf("a step with id %q already exists", step.ID)
	}
	e.Book.Steps = append(e.Book.Steps, step)
	if err := book.Save(e.Path, e.Book); err != nil {
		return nil, err
	}
	return e.Book, nil
}

// UpdateStep replaces a step in place, keeping its position.
func (a *App) UpdateStep(bookID, stepID string, step book.Step) (*book.Book, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}
	idx := e.Book.StepIndex(stepID)
	if idx < 0 {
		return nil, fmt.Errorf("step %q not found", stepID)
	}
	if step.ID == "" {
		step.ID = stepID
	}
	if step.ID != stepID && e.Book.HasStepID(step.ID) {
		return nil, fmt.Errorf("a step with id %q already exists", step.ID)
	}
	e.Book.Steps[idx] = step
	if err := book.Save(e.Path, e.Book); err != nil {
		return nil, err
	}
	return e.Book, nil
}

// DeleteStep removes a step from a book.
func (a *App) DeleteStep(bookID, stepID string) (*book.Book, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}
	idx := e.Book.StepIndex(stepID)
	if idx < 0 {
		return nil, fmt.Errorf("step %q not found", stepID)
	}
	e.Book.Steps = append(e.Book.Steps[:idx], e.Book.Steps[idx+1:]...)
	if err := book.Save(e.Path, e.Book); err != nil {
		return nil, err
	}
	return e.Book, nil
}

// MoveStep reorders a step.
func (a *App) MoveStep(bookID, stepID string, to int) (*book.Book, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}
	from := e.Book.StepIndex(stepID)
	if from < 0 {
		return nil, fmt.Errorf("step %q not found", stepID)
	}
	if to < 0 || to >= len(e.Book.Steps) {
		return nil, fmt.Errorf("target position %d is out of range", to)
	}
	steps := e.Book.Steps
	step := steps[from]
	steps = append(steps[:from], steps[from+1:]...)
	steps = append(steps, book.Step{})
	copy(steps[to+1:], steps[to:])
	steps[to] = step
	e.Book.Steps = steps
	if err := book.Save(e.Path, e.Book); err != nil {
		return nil, err
	}
	return e.Book, nil
}

func (a *App) progressStore() (*progress.Store, error) {
	if a.progress == nil {
		return nil, progress.ErrUnavailable
	}
	return a.progress, nil
}

// existingSteps drops ids that no longer belong to the book, so deleting a step
// cannot leave a stale tick inflating the "done" counter.
func existingSteps(b *book.Book, ids []string) []string {
	out := []string{}
	for _, id := range ids {
		if b.StepIndex(id) >= 0 {
			out = append(out, id)
		}
	}
	return out
}

// CheckedSteps returns the step ids ticked off in this book.
func (a *App) CheckedSteps(bookID string) ([]string, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}
	ps, err := a.progressStore()
	if err != nil {
		return nil, err
	}
	checked, err := ps.Checked(e.Path)
	if err != nil {
		return nil, err
	}
	return existingSteps(e.Book, checked), nil
}

// SetStepChecked ticks or unticks one step and returns the book's checked ids.
func (a *App) SetStepChecked(bookID, stepID string, checked bool) ([]string, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}
	if e.Book.StepIndex(stepID) < 0 {
		return nil, fmt.Errorf("step %q not found", stepID)
	}
	ps, err := a.progressStore()
	if err != nil {
		return nil, err
	}
	ids, err := ps.Set(e.Path, stepID, checked)
	if err != nil {
		return nil, err
	}
	return existingSteps(e.Book, ids), nil
}

// ResetProgress clears every checkmark for a book: a fresh start for a new run.
func (a *App) ResetProgress(bookID string) ([]string, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}
	ps, err := a.progressStore()
	if err != nil {
		return nil, err
	}
	if err := ps.Reset(e.Path); err != nil {
		return nil, err
	}
	return []string{}, nil
}

// RunBook executes a book and streams progress to the UI as "run:event"
// events. The returned result is the same value the CLI prints.
func (a *App) RunBook(bookID string, stepIDs []string, dryRun bool) (*engine.RunResult, error) {
	e, err := a.entry(bookID)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()

	a.mu.Lock()
	if a.cancel != nil {
		a.mu.Unlock()
		return nil, errors.New("a run is already in progress")
	}
	a.cancel = cancel
	a.mu.Unlock()

	defer func() {
		a.mu.Lock()
		a.cancel = nil
		a.mu.Unlock()
	}()

	runner := &engine.Runner{
		Dir: filepath.Dir(e.Path),
		Sink: func(ev engine.Event) {
			runtime.EventsEmit(a.ctx, "run:event", ev)
		},
	}
	return runner.Run(ctx, e.Book, engine.Options{StepIDs: stepIDs, DryRun: dryRun})
}

// StopRun cancels the current run and kills its process tree.
func (a *App) StopRun() {
	a.mu.Lock()
	cancel := a.cancel
	a.mu.Unlock()
	if cancel != nil {
		cancel()
	}
}

// ChooseFolder lets the user pick a different books folder and remembers it.
func (a *App) ChooseFolder() (string, error) {
	dir, err := runtime.OpenDirectoryDialog(a.ctx, runtime.OpenDialogOptions{
		Title: "Choose the folder that holds your Books",
	})
	if err != nil {
		return "", err
	}
	if dir == "" {
		return "", nil // cancelled
	}
	st, err := store.New(dir)
	if err != nil {
		return "", err
	}
	a.mu.Lock()
	a.dir = st.Dir
	a.mu.Unlock()
	if err := saveConfig(config{Dir: st.Dir}); err != nil {
		return st.Dir, err
	}
	return st.Dir, nil
}
