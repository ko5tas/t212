package poller_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/poller"
	"github.com/ko5tas/t212/internal/store"
)

func makeServer(t *testing.T, positions []api.Position, callCount *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("x-ratelimit-remaining", "10")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode(positions)
	}))
}

func TestPoller_PollsAndStores(t *testing.T) {
	positions := []api.Position{
		{Ticker: "AAPL_US_EQ", Quantity: 3, AveragePrice: 173.20, CurrentPrice: 182.50},
	}
	var callCount atomic.Int32
	srv := makeServer(t, positions, &callCount)
	defer srv.Close()

	s := store.New()
	h := hub.New()
	p := poller.New(api.NewClient("test-key", srv.URL, srv.Client()), s, h, 1.00, nil)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go p.Run(ctx)

	// Wait up to 2 seconds for the store to be populated.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got := s.Get()
		if len(got) > 0 {
			if got[0].Ticker != "AAPL_US_EQ" {
				t.Errorf("unexpected ticker: %q", got[0].Ticker)
			}
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("store was not populated within 2 seconds")
}

func TestPoller_BroadcastsFilteredPositions(t *testing.T) {
	positions := []api.Position{
		{Ticker: "AAPL_US_EQ", Quantity: 3, AveragePrice: 173.20, CurrentPrice: 182.50}, // profit 9.30 > 1
		{Ticker: "TSLA_US_EQ", Quantity: 1, AveragePrice: 200.00, CurrentPrice: 199.00}, // loss — filtered out
	}
	var callCount atomic.Int32
	srv := makeServer(t, positions, &callCount)
	defer srv.Close()

	s := store.New()
	h := hub.New()
	ch, unsub := h.Subscribe()
	defer unsub()

	p := poller.New(api.NewClient("test-key", srv.URL, srv.Client()), s, h, 1.00, nil)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	go p.Run(ctx)

	select {
	case msg := <-ch:
		var payload struct {
			Positions []api.Position `json:"positions"`
		}
		if err := json.Unmarshal(msg, &payload); err != nil {
			t.Fatalf("unmarshal broadcast: %v", err)
		}
		if len(payload.Positions) != 1 {
			t.Fatalf("broadcast should contain 1 filtered position, got %d", len(payload.Positions))
		}
		if payload.Positions[0].Ticker != "AAPL_US_EQ" {
			t.Errorf("expected AAPL, got %q", payload.Positions[0].Ticker)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestPoller_StopsOnContextCancel(t *testing.T) {
	var callCount atomic.Int32
	srv := makeServer(t, []api.Position{}, &callCount)
	defer srv.Close()

	s := store.New()
	h := hub.New()
	p := poller.New(api.NewClient("test-key", srv.URL, srv.Client()), s, h, 1.00, nil)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		p.Run(ctx)
		close(done)
	}()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("poller did not stop after context cancel")
	}
}
