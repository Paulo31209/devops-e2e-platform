// Package notes implements a thread-safe in-memory note store.
package notes

import (
	"errors"
	"sort"
	"sync"
	"time"
)

// ErrNotFound is returned when a note does not exist.
var ErrNotFound = errors.New("note not found")

// Note is a single note.
type Note struct {
	ID        int64     `json:"id"`
	Title     string    `json:"title"`
	Body      string    `json:"body"`
	CreatedAt time.Time `json:"created_at"`
}

// Store keeps notes in memory. It is safe for concurrent use.
type Store struct {
	mu     sync.RWMutex
	nextID int64
	notes  map[int64]Note
	now    func() time.Time
}

// NewStore returns an empty store.
func NewStore() *Store {
	return &Store{nextID: 1, notes: make(map[int64]Note), now: time.Now}
}

// Create adds a note and returns it with its assigned ID.
func (s *Store) Create(title, body string) Note {
	s.mu.Lock()
	defer s.mu.Unlock()

	n := Note{ID: s.nextID, Title: title, Body: body, CreatedAt: s.now().UTC()}
	s.notes[n.ID] = n
	s.nextID++
	return n
}

// Get returns the note with the given ID.
func (s *Store) Get(id int64) (Note, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	n, ok := s.notes[id]
	if !ok {
		return Note{}, ErrNotFound
	}
	return n, nil
}

// List returns all notes ordered by ID.
func (s *Store) List() []Note {
	s.mu.RLock()
	defer s.mu.RUnlock()

	out := make([]Note, 0, len(s.notes))
	for _, n := range s.notes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

// Delete removes the note with the given ID.
func (s *Store) Delete(id int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.notes[id]; !ok {
		return ErrNotFound
	}
	delete(s.notes, id)
	return nil
}

// Len returns the number of stored notes.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.notes)
}
