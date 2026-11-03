// Package progress remembers which steps of a book the user has ticked off.
//
// The state deliberately lives outside the book file. A book is a document that
// belongs in git; a checkmark must not dirty the working tree, and two people
// ticking different steps during the same release must not fight over a merge.
// Progress is therefore local to this machine, keyed by the book's absolute
// path, and can be cleared with one button ("start a fresh run").
package progress

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
)

const schemaVersion = 1

type bookState struct {
	Checked   []string  `json:"checked"`
	UpdatedAt time.Time `json:"updatedAt"`
}

type state struct {
	Version int                   `json:"version"`
	Books   map[string]*bookState `json:"books"`
}

// Store persists checkmarks in a single JSON file.
type Store struct {
	path string
	mu   sync.Mutex
}

// New returns a Store backed by path. The file is created on first write.
func New(path string) *Store { return &Store{path: path} }

// DefaultPath is the per-user location of the progress file.
func DefaultPath() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "drill", "progress.json"), nil
}

// Checked returns the step ids ticked off for bookPath, sorted.
func (s *Store) Checked(bookPath string) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.load()
	bs := st.Books[bookPath]
	if bs == nil {
		return []string{}, nil
	}
	out := append([]string{}, bs.Checked...)
	sort.Strings(out)
	return out, nil
}

// Set ticks or unticks one step and returns the book's checked ids afterwards.
func (s *Store) Set(bookPath, stepID string, checked bool) ([]string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.load()
	bs := st.Books[bookPath]
	if bs == nil {
		bs = &bookState{}
	}

	current := make(map[string]bool, len(bs.Checked))
	for _, id := range bs.Checked {
		current[id] = true
	}
	if checked {
		current[stepID] = true
	} else {
		delete(current, stepID)
	}

	bs.Checked = sortedKeys(current)
	bs.UpdatedAt = time.Now().UTC()
	if len(bs.Checked) == 0 {
		delete(st.Books, bookPath)
	} else {
		st.Books[bookPath] = bs
	}
	if err := s.save(st); err != nil {
		return nil, err
	}
	return append([]string{}, bs.Checked...), nil
}

// Reset clears every checkmark for one book ("start a fresh run").
func (s *Store) Reset(bookPath string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	st := s.load()
	if _, ok := st.Books[bookPath]; !ok {
		return nil
	}
	delete(st.Books, bookPath)
	return s.save(st)
}

// load reads the state file. A missing or unreadable file is treated as "no
// checkmarks": losing ticks must never stop someone from using the app.
func (s *Store) load() state {
	st := state{Version: schemaVersion, Books: map[string]*bookState{}}
	data, err := os.ReadFile(s.path)
	if err != nil {
		return st
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return state{Version: schemaVersion, Books: map[string]*bookState{}}
	}
	if st.Books == nil {
		st.Books = map[string]*bookState{}
	}
	return st
}

// save writes atomically, so an interrupted write cannot corrupt the file.
func (s *Store) save(st state) error {
	st.Version = schemaVersion
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	tmp, err := os.CreateTemp(dir, ".progress-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	defer os.Remove(name)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(name, 0o644); err != nil {
		return err
	}
	return os.Rename(name, s.path)
}

func sortedKeys(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for id, on := range set {
		if on {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// ErrUnavailable is returned when the app cannot work out where to store
// progress (for example when the OS has no config directory).
var ErrUnavailable = errors.New("progress storage is unavailable")
