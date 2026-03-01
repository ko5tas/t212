package history

import (
	"sync"

	"github.com/ko5tas/t212/internal/api"
)

// Store is a thread-safe cache of per-ticker ReturnInfo.
type Store struct {
	mu   sync.RWMutex
	data map[string]api.ReturnInfo
}

// NewStore returns an initialised Store.
func NewStore() *Store {
	return &Store{data: make(map[string]api.ReturnInfo)}
}

// Get returns a copy of the ReturnInfo for the given ticker, or nil if not present.
func (s *Store) Get(ticker string) *api.ReturnInfo {
	s.mu.RLock()
	defer s.mu.RUnlock()
	ri, ok := s.data[ticker]
	if !ok {
		return nil
	}
	return &ri
}

// Set stores the ReturnInfo for a single ticker.
func (s *Store) Set(ticker string, ri api.ReturnInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[ticker] = ri
}

// SetAll replaces all stored data with the provided map.
func (s *Store) SetAll(m map[string]api.ReturnInfo) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = m
}
