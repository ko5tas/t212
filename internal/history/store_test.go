package history_test

import (
	"sync"
	"testing"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/history"
)

func TestStore_GetEmpty(t *testing.T) {
	s := history.NewStore()
	if got := s.Get("AAPL_US_EQ"); got != nil {
		t.Error("expected nil for missing ticker")
	}
}

func TestStore_SetAndGet(t *testing.T) {
	s := history.NewStore()
	ri := api.ReturnInfo{Return: 42.30, ReturnPct: 42.30}
	s.Set("AAPL_US_EQ", ri)

	got := s.Get("AAPL_US_EQ")
	if got == nil {
		t.Fatal("expected non-nil")
	}
	if got.Return != 42.30 {
		t.Errorf("Return: got %v, want 42.30", got.Return)
	}
}

func TestStore_SetAll(t *testing.T) {
	s := history.NewStore()
	s.Set("OLD_EQ", api.ReturnInfo{Return: 1.0})

	m := map[string]api.ReturnInfo{
		"AAPL_US_EQ": {Return: 10.0},
		"LLOY_EQ":    {Return: 5.0},
	}
	s.SetAll(m)

	if s.Get("OLD_EQ") != nil {
		t.Error("SetAll should replace, not merge")
	}
	if got := s.Get("AAPL_US_EQ"); got == nil || got.Return != 10.0 {
		t.Errorf("AAPL: got %v", got)
	}
}

func TestStore_ConcurrentAccess(t *testing.T) {
	s := history.NewStore()
	var wg sync.WaitGroup
	for i := 0; i < 100; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			s.Set("AAPL_US_EQ", api.ReturnInfo{Return: 1.0})
		}()
		go func() {
			defer wg.Done()
			_ = s.Get("AAPL_US_EQ")
		}()
	}
	wg.Wait()
}
