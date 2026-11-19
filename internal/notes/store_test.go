package notes

import (
	"errors"
	"sync"
	"testing"
)

func TestStoreCRUD(t *testing.T) {
	s := NewStore()

	a := s.Create("first", "a")
	b := s.Create("second", "b")
	if a.ID != 1 || b.ID != 2 {
		t.Fatalf("unexpected IDs: %d, %d", a.ID, b.ID)
	}

	got, err := s.Get(a.ID)
	if err != nil || got.Title != "first" {
		t.Fatalf("Get(%d) = %+v, %v", a.ID, got, err)
	}

	list := s.List()
	if len(list) != 2 || list[0].ID != 1 || list[1].ID != 2 {
		t.Fatalf("List() = %+v", list)
	}

	if err := s.Delete(a.ID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if _, err := s.Get(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
	if err := s.Delete(a.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("expected ErrNotFound on second delete, got %v", err)
	}
	if s.Len() != 1 {
		t.Fatalf("Len() = %d, want 1", s.Len())
	}
}

func TestStoreConcurrentCreate(t *testing.T) {
	s := NewStore()
	const workers = 50

	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s.Create("t", "b")
		}()
	}
	wg.Wait()

	if s.Len() != workers {
		t.Fatalf("Len() = %d, want %d", s.Len(), workers)
	}
}
