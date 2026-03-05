package poller_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/history"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/poller"
	"github.com/ko5tas/t212/internal/store"
)

// t212Wire converts a Position slice to the T212 API wire format for mock servers.
// The wire format nests the ticker inside an "instrument" object and uses "averagePricePaid".
// Currency is inferred from the ticker suffix by the client.
func t212Wire(positions []api.Position) []map[string]any {
	out := make([]map[string]any, len(positions))
	for i, p := range positions {
		out[i] = map[string]any{
			"instrument":       map[string]any{"ticker": p.Ticker, "name": p.Name},
			"quantity":         p.Quantity,
			"averagePricePaid": p.AveragePrice,
			"currentPrice":     p.CurrentPrice,
			"walletImpact":     map[string]any{"currentValue": p.CurrentValueGBP},
		}
	}
	return out
}

func makeServer(t *testing.T, positions []api.Position, callCount *atomic.Int32) *httptest.Server {
	t.Helper()
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		w.Header().Set("x-ratelimit-remaining", "10")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode(t212Wire(positions))
	}))
}

func TestPoller_PollsAndStores(t *testing.T) {
	positions := []api.Position{
		{Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3, AveragePrice: 173.20, CurrentPrice: 182.50},
	}
	var callCount atomic.Int32
	srv := makeServer(t, positions, &callCount)
	defer srv.Close()

	s := store.New()
	h := hub.New()
	p := poller.New(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, nil)

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

func TestPoller_BroadcastsAllPositions(t *testing.T) {
	positions := []api.Position{
		{Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3, AveragePrice: 173.20, CurrentPrice: 182.50}, // profit 9.30 > 1
		{Ticker: "TSLA_US_EQ", Currency: "USD", Quantity: 1, AveragePrice: 200.00, CurrentPrice: 199.00}, // loss
	}
	var callCount atomic.Int32
	srv := makeServer(t, positions, &callCount)
	defer srv.Close()

	s := store.New()
	h := hub.New()
	ch, unsub := h.Subscribe()
	defer unsub()

	p := poller.New(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, nil)
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
		if len(payload.Positions) != 2 {
			t.Fatalf("broadcast should contain all 2 positions, got %d", len(payload.Positions))
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
	p := poller.New(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, nil)

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

// ------------ sendNotifications edge-detection tests ------------

// mockNotifier captures Notify calls for assertion in tests.
type mockNotifier struct {
	mu    sync.Mutex
	calls []notifyCall
}

type notifyCall struct {
	ticker    string
	name      string
	entered   bool
	profit    float64
	returnPct float64
}

func (m *mockNotifier) Notify(ticker, name string, entered bool, profit, returnPct float64) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.calls = append(m.calls, notifyCall{ticker, name, entered, profit, returnPct})
}

func (m *mockNotifier) Calls() []notifyCall {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]notifyCall(nil), m.calls...)
}

// makeSequenceServer returns a TLS server that replies with responses[i] on the
// i-th request, clamping to the last element once all are exhausted.
func makeSequenceServer(t *testing.T, responses [][]api.Position) *httptest.Server {
	t.Helper()
	var idx atomic.Int32
	return httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := int(idx.Add(1)) - 1
		if i >= len(responses) {
			i = len(responses) - 1
		}
		w.Header().Set("x-ratelimit-remaining", "10")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode(t212Wire(responses[i]))
	}))
}

// drainBroadcasts reads n messages from ch. Because sendNotifications is called
// before broadcast in poll(), receiving a broadcast guarantees the corresponding
// notifications have already been dispatched.
func drainBroadcasts(t *testing.T, ch <-chan []byte, n int, timeout time.Duration) {
	t.Helper()
	for i := 0; i < n; i++ {
		select {
		case <-ch:
		case <-time.After(timeout):
			t.Fatalf("timeout waiting for broadcast %d/%d", i+1, n)
		}
	}
}

var (
	// currentValueGBP 100.50, totalBought 100.00 → profit 0.50 (no notification)
	posNeutral = []api.Position{{Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 1, AveragePrice: 100.00, CurrentPrice: 100.50, CurrentValueGBP: 100.50}}
	// currentValueGBP 110.00, totalBought 100.00 → profit 10.00 > 1.00 (green)
	posAbove = []api.Position{{Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 1, AveragePrice: 100.00, CurrentPrice: 110.00, CurrentValueGBP: 110.00}}
	// currentValueGBP 89.00, totalBought 100.00 → negative return (red)
	posLoss = []api.Position{{Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 1, AveragePrice: 100.00, CurrentPrice: 89.00, CurrentValueGBP: 89.00}}
)

// notifyHistoryStore returns a history store pre-loaded with TotalBought for AAPL_US_EQ.
func notifyHistoryStore() *history.Store {
	hs := history.NewStore()
	hs.Set("AAPL_US_EQ", api.ReturnInfo{TotalBought: 100.00})
	return hs
}

func TestSendNotifications_EnterThreshold(t *testing.T) {
	// With history store, Run() fires a background goroutine that does an extra
	// poll after refreshHistory. Sequence: poll(below), bg-poll(below), tick-poll(above).
	srv := makeSequenceServer(t, [][]api.Position{posNeutral, posNeutral, posAbove})
	defer srv.Close()

	s := store.New()
	h := hub.New()
	broadcastCh, unsub := h.Subscribe()
	defer unsub()

	n := &mockNotifier{}
	p := poller.NewForTesting(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, n, 50*time.Millisecond,
		poller.WithHistoryStore(notifyHistoryStore()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)

	drainBroadcasts(t, broadcastCh, 3, 3*time.Second)
	cancel()

	calls := n.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 notification, got %d: %+v", len(calls), calls)
	}
	if !calls[0].entered || calls[0].ticker != "AAPL_US_EQ" {
		t.Errorf("expected enter notification for AAPL, got: %+v", calls[0])
	}
	if calls[0].profit != 10.00 {
		t.Errorf("expected profit 10.00, got %v", calls[0].profit)
	}
}

func TestSendNotifications_LossThreshold(t *testing.T) {
	// Sequence: poll(neutral), bg-poll(neutral), tick-poll(loss).
	// Expected: 1 red notification for negative return.
	srv := makeSequenceServer(t, [][]api.Position{posNeutral, posNeutral, posLoss})
	defer srv.Close()

	s := store.New()
	h := hub.New()
	broadcastCh, unsub := h.Subscribe()
	defer unsub()

	n := &mockNotifier{}
	p := poller.NewForTesting(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, n, 50*time.Millisecond,
		poller.WithHistoryStore(notifyHistoryStore()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)

	drainBroadcasts(t, broadcastCh, 3, 3*time.Second)
	cancel()

	calls := n.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 notification (loss), got %d: %+v", len(calls), calls)
	}
	if calls[0].entered {
		t.Errorf("call[0] should be loss (entered=false), got: %+v", calls[0])
	}
	if calls[0].ticker != "AAPL_US_EQ" {
		t.Errorf("loss notification ticker mismatch: %+v", calls[0])
	}
	if calls[0].profit != 11.00 {
		t.Errorf("expected loss 11.00, got %v", calls[0].profit)
	}
}

func TestSendNotifications_NoDoubleNotifyOnStay(t *testing.T) {
	// Sequence: poll(above), bg-poll(above), tick-poll(above), tick-poll(above).
	srv := makeSequenceServer(t, [][]api.Position{posAbove, posAbove, posAbove, posAbove})
	defer srv.Close()

	s := store.New()
	h := hub.New()
	broadcastCh, unsub := h.Subscribe()
	defer unsub()

	n := &mockNotifier{}
	p := poller.NewForTesting(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, n, 50*time.Millisecond,
		poller.WithHistoryStore(notifyHistoryStore()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	go p.Run(ctx)

	drainBroadcasts(t, broadcastCh, 4, 3*time.Second)
	cancel()

	calls := n.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected exactly 1 notification (entry only), got %d: %+v", len(calls), calls)
	}
	if !calls[0].entered {
		t.Errorf("sole notification should be enter, got: %+v", calls[0])
	}
}

func TestPoller_BroadcastIncludesReturns(t *testing.T) {
	positions := []api.Position{
		{Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3, AveragePrice: 173.20, CurrentPrice: 182.50},
	}
	var callCount atomic.Int32
	srv := makeServer(t, positions, &callCount)
	defer srv.Close()

	s := store.New()
	h := hub.New()
	ch, unsub := h.Subscribe()
	defer unsub()

	hs := history.NewStore()
	hs.Set("AAPL_US_EQ", api.ReturnInfo{Return: 42.30, ReturnPct: 42.30})

	p := poller.NewForTesting(
		api.NewClient("k", "s", srv.URL, srv.Client()),
		s, h, 1.00, nil, 50*time.Millisecond,
		poller.WithHistoryStore(hs),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)

	select {
	case msg := <-ch:
		var payload struct {
			Positions []api.Position `json:"positions"`
		}
		if err := json.Unmarshal(msg, &payload); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(payload.Positions) != 1 {
			t.Fatalf("expected 1 position, got %d", len(payload.Positions))
		}
		if payload.Positions[0].Returns == nil {
			t.Fatal("Returns should be attached from history store")
		}
		if payload.Positions[0].Returns.Return != 42.30 {
			t.Errorf("Return: got %v, want 42.30", payload.Positions[0].Returns.Return)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestSendNotifications_ProfitToNeutralNoRedAlert(t *testing.T) {
	// Sequence: poll(above), bg-poll(above), tick-poll(neutral).
	// Dropping from profit to neutral should NOT trigger a red alert (return still positive).
	srv := makeSequenceServer(t, [][]api.Position{posAbove, posAbove, posNeutral})
	defer srv.Close()

	s := store.New()
	h := hub.New()
	broadcastCh, unsub := h.Subscribe()
	defer unsub()

	n := &mockNotifier{}
	p := poller.NewForTesting(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, n, 50*time.Millisecond,
		poller.WithHistoryStore(notifyHistoryStore()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)

	drainBroadcasts(t, broadcastCh, 3, 3*time.Second)
	cancel()

	calls := n.Calls()
	if len(calls) != 1 {
		t.Fatalf("expected 1 notification (enter only, no red alert), got %d: %+v", len(calls), calls)
	}
	if !calls[0].entered {
		t.Errorf("sole notification should be enter, got: %+v", calls[0])
	}
}

func TestSendNotifications_ProfitThenLoss(t *testing.T) {
	// Sequence: poll(above), bg-poll(above), tick-poll(loss).
	// Expected: green enter + red loss = 2 notifications.
	srv := makeSequenceServer(t, [][]api.Position{posAbove, posAbove, posLoss})
	defer srv.Close()

	s := store.New()
	h := hub.New()
	broadcastCh, unsub := h.Subscribe()
	defer unsub()

	n := &mockNotifier{}
	p := poller.NewForTesting(api.NewClient("test-key", "test-secret", srv.URL, srv.Client()), s, h, 1.00, n, 50*time.Millisecond,
		poller.WithHistoryStore(notifyHistoryStore()),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	go p.Run(ctx)

	drainBroadcasts(t, broadcastCh, 3, 3*time.Second)
	cancel()

	calls := n.Calls()
	if len(calls) != 2 {
		t.Fatalf("expected 2 notifications (enter+loss), got %d: %+v", len(calls), calls)
	}
	if !calls[0].entered {
		t.Errorf("call[0] should be enter (green), got: %+v", calls[0])
	}
	if calls[1].entered {
		t.Errorf("call[1] should be loss (red), got: %+v", calls[1])
	}
}
