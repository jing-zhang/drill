// Package store maps a directory of Book JSON files to the Book model.
package store

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kingzad/drill/internal/book"
)

// Store is a directory holding one JSON file per book.
type Store struct {
	Dir string // absolute path
}

// Entry is a book together with the file it was loaded from.
type Entry struct {
	Path string
	Book *book.Book
}

// Problem is a file that could not be loaded as a Book.
type Problem struct {
	Path  string `json:"path"`
	Error string `json:"error"`
}

// Listing is the result of scanning the directory.
type Listing struct {
	Entries  []Entry
	Problems []Problem
}

// New returns a Store rooted at dir, which is created if it does not exist.
func New(dir string) (*Store, error) {
	if dir == "" {
		dir = "."
	}
	abs, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(abs, 0o755); err != nil {
		return nil, err
	}
	return &Store{Dir: abs}, nil
}

// PathFor returns the canonical file path for a book id.
func (s *Store) PathFor(id string) string {
	return filepath.Join(s.Dir, id+".json")
}

// List loads every *.json book in the directory. Files that fail to parse or
// validate are reported as problems rather than aborting the scan.
func (s *Store) List() (Listing, error) {
	dirents, err := os.ReadDir(s.Dir)
	if err != nil {
		return Listing{}, err
	}
	var out Listing
	for _, de := range dirents {
		name := de.Name()
		if de.IsDir() || strings.HasPrefix(name, ".") || !strings.HasSuffix(name, ".json") {
			continue
		}
		path := filepath.Join(s.Dir, name)
		b, err := book.Load(path)
		if err != nil {
			out.Problems = append(out.Problems, Problem{Path: path, Error: err.Error()})
			continue
		}
		out.Entries = append(out.Entries, Entry{Path: path, Book: b})
	}
	sort.Slice(out.Entries, func(i, j int) bool {
		return out.Entries[i].Book.ID < out.Entries[j].Book.ID
	})
	return out, nil
}

// Resolve loads a book by reference: either a path to a .json file or a book id.
func (s *Store) Resolve(ref string) (Entry, error) {
	if ref == "" {
		return Entry{}, errors.New("book reference is empty")
	}
	looksLikePath := strings.HasSuffix(ref, ".json") ||
		strings.ContainsAny(ref, `/\`) ||
		strings.HasPrefix(ref, ".")
	if looksLikePath {
		path := ref
		if !filepath.IsAbs(path) {
			abs, err := filepath.Abs(path)
			if err != nil {
				return Entry{}, err
			}
			path = abs
		}
		b, err := book.Load(path)
		if err != nil {
			return Entry{}, err
		}
		return Entry{Path: path, Book: b}, nil
	}

	if !book.ValidID(ref) {
		return Entry{}, fmt.Errorf("%q is not a book id or a path to a .json file", ref)
	}
	path := s.PathFor(ref)
	if _, err := os.Stat(path); err == nil {
		b, err := book.Load(path)
		if err != nil {
			return Entry{}, err
		}
		return Entry{Path: path, Book: b}, nil
	}
	// Fall back to a scan so files renamed away from <id>.json still resolve.
	listing, err := s.List()
	if err != nil {
		return Entry{}, err
	}
	for _, e := range listing.Entries {
		if e.Book.ID == ref {
			return e, nil
		}
	}
	return Entry{}, fmt.Errorf("book %q not found in %s", ref, s.Dir)
}

// Create writes a new book to <dir>/<id>.json and fails if that file exists.
func (s *Store) Create(b *book.Book) (string, error) {
	if err := b.Validate(); err != nil {
		return "", err
	}
	path := s.PathFor(b.ID)
	if _, err := os.Stat(path); err == nil {
		return "", fmt.Errorf("%s already exists", path)
	}
	if err := book.Save(path, b); err != nil {
		return "", err
	}
	return path, nil
}

// SaveTo writes an already-loaded book back to the file it came from.
func (s *Store) SaveTo(path string, b *book.Book) error {
	return book.Save(path, b)
}

// Delete resolves ref and removes its file.
func (s *Store) Delete(ref string) error {
	e, err := s.Resolve(ref)
	if err != nil {
		return err
	}
	return os.Remove(e.Path)
}
