// Package book defines the on-disk Book format: the schema, its validation
// rules, and atomic JSON persistence.
//
// A Book is one process — a release runbook, an onboarding checklist, a
// recovery procedure — stored as a single human-readable, diffable JSON file.
package book

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// SchemaVersion is the only Book format version this build understands.
const SchemaVersion = 1

// MaxIDLen bounds book and step identifiers.
const MaxIDLen = 64

// Shells lists the interpreters a step may name in its "shell" field.
var Shells = []string{"sh", "bash", "pwsh", "powershell", "cmd", "python", "python3"}

var (
	idPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)
	notSlug   = regexp.MustCompile(`[^a-z0-9_-]+`)
)

// Defaults are book-wide settings a step may override.
type Defaults struct {
	Cwd            string `json:"cwd,omitempty"`
	Shell          string `json:"shell,omitempty"`
	TimeoutSeconds int    `json:"timeoutSeconds,omitempty"`
}

// Step is one ordered item of a Book. It always has a title and may carry a
// markdown description plus exactly one executable payload: a command or an
// inline script.
type Step struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Description     string `json:"description,omitempty"`
	Command         string `json:"command,omitempty"`
	Script          string `json:"script,omitempty"`
	Shell           string `json:"shell,omitempty"`
	Cwd             string `json:"cwd,omitempty"`
	TimeoutSeconds  int    `json:"timeoutSeconds,omitempty"`
	ContinueOnError bool   `json:"continueOnError,omitempty"`
}

// Book is a single runbook document.
type Book struct {
	Version     int      `json:"version"`
	ID          string   `json:"id"`
	Title       string   `json:"title"`
	Description string   `json:"description,omitempty"`
	Tags        []string `json:"tags,omitempty"`
	Defaults    Defaults `json:"defaults"`
	Steps       []Step   `json:"steps"`
}

// Runnable reports whether the step carries something to execute.
func (s Step) Runnable() bool { return s.Command != "" || s.Script != "" }

// ValidateShell reports whether s is a supported shell name ("" means default).
func ValidateShell(s string) bool {
	if s == "" {
		return true
	}
	for _, known := range Shells {
		if s == known {
			return true
		}
	}
	return false
}

// ValidID reports whether id is safe to use as a file stem and URL segment.
func ValidID(id string) bool {
	return len(id) > 0 && len(id) <= MaxIDLen && idPattern.MatchString(id)
}

// Slug converts arbitrary text into a valid ID ("" when nothing usable remains).
func Slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = notSlug.ReplaceAllString(s, "-")
	s = strings.Trim(s, "-_")
	if len(s) > MaxIDLen {
		s = strings.Trim(s[:MaxIDLen], "-_")
	}
	return s
}

// HasStepID reports whether b already contains a step with this id.
func (b *Book) HasStepID(id string) bool {
	for _, s := range b.Steps {
		if s.ID == id {
			return true
		}
	}
	return false
}

// StepIndex returns the position of the step with the given id, or -1.
func (b *Book) StepIndex(id string) int {
	for i, s := range b.Steps {
		if s.ID == id {
			return i
		}
	}
	return -1
}

// UniqueStepID derives an id from title that does not collide with b's steps.
func UniqueStepID(b *Book, title string) string {
	base := Slug(title)
	if base == "" {
		base = "step"
	}
	if !b.HasStepID(base) {
		return base
	}
	for n := 2; ; n++ {
		candidate := fmt.Sprintf("%s-%d", base, n)
		if !b.HasStepID(candidate) {
			return candidate
		}
	}
}

// Validate checks the whole document and reports every problem found.
func (b *Book) Validate() error {
	var problems []string
	add := func(format string, args ...any) {
		problems = append(problems, fmt.Sprintf(format, args...))
	}

	if b.Version != SchemaVersion {
		add("version: want %d, got %d", SchemaVersion, b.Version)
	}
	if !ValidID(b.ID) {
		add("id: %q is not a valid id (lowercase letters, digits, '_', '-')", b.ID)
	}
	if strings.TrimSpace(b.Title) == "" {
		add("title: required")
	}
	if !ValidateShell(b.Defaults.Shell) {
		add("defaults.shell: unknown shell %q", b.Defaults.Shell)
	}
	if b.Defaults.TimeoutSeconds < 0 {
		add("defaults.timeoutSeconds: must not be negative")
	}
	if b.Steps == nil {
		add("steps: required (use [] for an empty book)")
	}

	seen := make(map[string]int, len(b.Steps))
	for i, s := range b.Steps {
		where := fmt.Sprintf("steps[%d]", i)
		if s.ID == "" {
			add("%s.id: required", where)
		} else {
			if !ValidID(s.ID) {
				add("%s.id: %q is not a valid id", where, s.ID)
			}
			if prev, ok := seen[s.ID]; ok {
				add("%s.id: duplicate of steps[%d] (%q)", where, prev, s.ID)
			} else {
				seen[s.ID] = i
			}
		}
		if strings.TrimSpace(s.Title) == "" {
			add("%s.title: required", where)
		}
		// A step with neither command nor script is valid: it is a checklist item.
		if s.Command != "" && s.Script != "" {
			add("%s: has both command and script; pick one", where)
		}
		if !ValidateShell(s.Shell) {
			add("%s.shell: unknown shell %q", where, s.Shell)
		}
		if s.TimeoutSeconds < 0 {
			add("%s.timeoutSeconds: must not be negative", where)
		}
	}

	if len(problems) == 0 {
		return nil
	}
	return errors.New(strings.Join(problems, "\n"))
}

// Marshal renders a Book as indented JSON with a trailing newline. HTML
// escaping is disabled so commands containing <, > or & stay readable.
func Marshal(b *Book) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(b); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// Load reads and validates a Book. Unknown fields are rejected so that typos
// in hand-edited files are reported instead of silently ignored.
func Load(path string) (*Book, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	b := &Book{}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(b); err != nil {
		return nil, fmt.Errorf("%s: invalid JSON: %w", path, err)
	}
	if b.Steps == nil {
		b.Steps = []Step{}
	}
	if err := b.Validate(); err != nil {
		return nil, fmt.Errorf("%s:\n%w", path, err)
	}
	return b, nil
}

// Save validates b and writes it atomically: a temporary file in the target
// directory is renamed over path, so a crash never leaves a half-written book.
func Save(path string, b *Book) error {
	if err := b.Validate(); err != nil {
		return err
	}
	data, err := Marshal(b)
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".drill-*.json")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op once renamed
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
