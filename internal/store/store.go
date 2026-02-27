package store

import (
	"sync"

	"github.com/ko5tas/t212/internal/api"
)

// Store is a thread-safe in-memory cache of the latest positions.
type Store struct {
	mu        sync.RWMutex
	positions []api.Position
}

// New returns an initialised Store.
func New() *Store {
	return &Store{positions: []api.Position{}}
}

// Set replaces all stored positions.
func (s *Store) Set(positions []api.Position) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.positions = positions
}

// Get returns a copy of the current positions slice.
func (s *Store) Get() []api.Position {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]api.Position, len(s.positions))
	copy(out, s.positions)
	return out
}
