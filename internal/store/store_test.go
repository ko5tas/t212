package store_test

import (
	"sync"
	"testing"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/store"
)

func TestStore_GetEmpty(t *testing.T) {
	s := store.New()
	got := s.Get()
	if got == nil {
		t.Error("Get() on empty store should return empty slice, not nil")
	}
	if len(got) != 0 {
		t.Errorf("expected 0 positions, got %d", len(got))
	}
}

func TestStore_SetAndGet(t *testing.T) {
	s := store.New()
	positions := []api.Position{
		{Ticker: "AAPL", CurrentPrice: 182.50, AveragePrice: 173.20, Quantity: 3},
	}
	s.Set(positions)

	got := s.Get()
	if len(got) != 1 {
		t.Fatalf("expected 1 position, got %d", len(got))
	}
	if got[0].Ticker != "AAPL" {
		t.Errorf("ticker: got %q, want AAPL", got[0].Ticker)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := store.New()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.Set([]api.Position{{Ticker: "AAPL"}})
		}()
		go func() {
			defer wg.Done()
			_ = s.Get()
		}()
	}
	wg.Wait()
}
