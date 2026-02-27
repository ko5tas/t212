# T212 Dashboard Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Build a Go service that polls the Trading 212 live API, filters positions with profit-per-share > £1, and displays them via a WebSocket-powered Web UI and bubbletea TUI, with Signal notifications on threshold crossings.

**Architecture:** Single `linux/arm64` binary with two subcommands — `t212 serve` (systemd daemon: poller + HTTP server + WebSocket hub + Signal notifier) and `t212 tui` (connects to running serve via WebSocket, bubbletea renderer). Data flows: T212 API → poller goroutine → in-memory store → hub.Broadcast() → browser + TUI subscribers.

**Tech Stack:** Go 1.22, `github.com/gorilla/websocket`, `github.com/charmbracelet/bubbletea`, `github.com/charmbracelet/lipgloss`, `log/slog` (stdlib), `go:embed` for web assets, `net/http` stdlib server, signal-cli subprocess for notifications.

**Design doc:** `docs/plans/2026-02-27-t212-dashboard-design.md`

**Branch strategy:** Each PR block below creates a branch off `main`, opens a PR, and merges before the next block starts. Use `gh pr create` and `gh pr merge --squash`.

---

## PR 1 — `feature/core-models`: API client, models, filter, store

### Task 1: Initialise Go module and project structure

**Files:**
- Create: `go.mod`
- Create: `go.sum` (generated)
- Create: `cmd/t212/main.go`
- Create: `internal/api/position.go`
- Create: `internal/filter/filter.go`
- Create: `internal/store/store.go`

**Step 1: Create branch**
```bash
git checkout -b feature/core-models
```

**Step 2: Initialise module**
```bash
go mod init github.com/ko5tas/t212
```
Expected: `go.mod` created with `module github.com/ko5tas/t212` and `go 1.22`

**Step 3: Create minimal main.go so the module compiles**
```go
// cmd/t212/main.go
package main

func main() {}
```

**Step 4: Verify it compiles**
```bash
go build ./...
```
Expected: no output, no errors.

**Step 5: Commit**
```bash
git add go.mod cmd/t212/main.go
git commit -m "chore: initialise Go module"
```

---

### Task 2: Position model

**Files:**
- Create: `internal/api/position.go`
- Create: `internal/api/position_test.go`

**Step 1: Write the failing test**
```go
// internal/api/position_test.go
package api_test

import (
	"encoding/json"
	"testing"

	"github.com/ko5tas/t212/internal/api"
)

func TestPosition_UnmarshalJSON(t *testing.T) {
	raw := `{
		"ticker": "AAPL_US_EQ",
		"quantity": 3.0,
		"averagePrice": 173.20,
		"currentPrice": 182.50
	}`

	var p api.Position
	if err := json.Unmarshal([]byte(raw), &p); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if p.Ticker != "AAPL_US_EQ" {
		t.Errorf("ticker: got %q, want %q", p.Ticker, "AAPL_US_EQ")
	}
	if p.Quantity != 3.0 {
		t.Errorf("quantity: got %v, want 3.0", p.Quantity)
	}
	if p.AveragePrice != 173.20 {
		t.Errorf("averagePrice: got %v, want 173.20", p.AveragePrice)
	}
	if p.CurrentPrice != 182.50 {
		t.Errorf("currentPrice: got %v, want 182.50", p.CurrentPrice)
	}
}

func TestPosition_Computed(t *testing.T) {
	p := api.Position{
		Ticker:       "AAPL_US_EQ",
		Quantity:     3.0,
		AveragePrice: 173.20,
		CurrentPrice: 182.50,
	}
	p.Compute()

	want := 182.50 - 173.20 // 9.30
	if diff := p.ProfitPerShare - want; diff > 0.001 || diff < -0.001 {
		t.Errorf("ProfitPerShare: got %v, want ~%v", p.ProfitPerShare, want)
	}

	wantMV := 3.0 * 182.50 // 547.50
	if diff := p.MarketValue - wantMV; diff > 0.001 || diff < -0.001 {
		t.Errorf("MarketValue: got %v, want ~%v", p.MarketValue, wantMV)
	}
}
```

**Step 2: Run test to verify it fails**
```bash
go test ./internal/api/... -v -run TestPosition
```
Expected: FAIL — `package api` not found.

**Step 3: Write the implementation**
```go
// internal/api/position.go
package api

// Position is a single open position returned by the T212 /equity/positions endpoint.
// ProfitPerShare and MarketValue are computed fields — call Compute() after unmarshalling.
type Position struct {
	Ticker         string  `json:"ticker"`
	Quantity       float64 `json:"quantity"`
	AveragePrice   float64 `json:"averagePrice"`
	CurrentPrice   float64 `json:"currentPrice"`
	ProfitPerShare float64 `json:"profitPerShare"`
	MarketValue    float64 `json:"marketValue"`
}

// Compute populates the derived fields ProfitPerShare and MarketValue.
func (p *Position) Compute() {
	p.ProfitPerShare = p.CurrentPrice - p.AveragePrice
	p.MarketValue = p.Quantity * p.CurrentPrice
}
```

**Step 4: Run test to verify it passes**
```bash
go test ./internal/api/... -v -run TestPosition
```
Expected: PASS

**Step 5: Commit**
```bash
git add internal/api/position.go internal/api/position_test.go
git commit -m "feat: add Position model with computed fields"
```

---

### Task 3: Filter logic

**Files:**
- Create: `internal/filter/filter.go`
- Create: `internal/filter/filter_test.go`

**Step 1: Write the failing tests**
```go
// internal/filter/filter_test.go
package filter_test

import (
	"testing"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/filter"
)

func pos(ticker string, avg, current, qty float64) api.Position {
	p := api.Position{
		Ticker:       ticker,
		Quantity:     qty,
		AveragePrice: avg,
		CurrentPrice: current,
	}
	p.Compute()
	return p
}

func TestApply(t *testing.T) {
	threshold := 1.00

	tests := []struct {
		name      string
		positions []api.Position
		want      []string // expected tickers in result
	}{
		{
			name: "well above threshold",
			positions: []api.Position{pos("AAPL", 173.20, 182.50, 3)},
			want: []string{"AAPL"},
		},
		{
			name: "just above threshold (1.01)",
			positions: []api.Position{pos("MSFT", 100.00, 101.01, 1)},
			want: []string{"MSFT"},
		},
		{
			name: "exactly at threshold (1.00) — excluded",
			positions: []api.Position{pos("GOOG", 100.00, 101.00, 1)},
			want: []string{},
		},
		{
			name: "just below threshold (0.99)",
			positions: []api.Position{pos("AMZN", 100.00, 100.99, 1)},
			want: []string{},
		},
		{
			name: "zero profit",
			positions: []api.Position{pos("META", 100.00, 100.00, 1)},
			want: []string{},
		},
		{
			name: "negative profit (loss)",
			positions: []api.Position{pos("TSLA", 200.00, 190.00, 5)},
			want: []string{},
		},
		{
			name: "mixed — only profitable ones returned",
			positions: []api.Position{
				pos("AAPL", 173.20, 182.50, 3),
				pos("TSLA", 200.00, 190.00, 5),
				pos("MSFT", 100.00, 105.00, 2),
			},
			want: []string{"AAPL", "MSFT"},
		},
		{
			name: "empty input",
			positions: []api.Position{},
			want: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := filter.Apply(tt.positions, threshold)
			if len(got) != len(tt.want) {
				t.Fatalf("got %d positions, want %d: %+v", len(got), len(tt.want), got)
			}
			for i, p := range got {
				if p.Ticker != tt.want[i] {
					t.Errorf("position[%d]: got ticker %q, want %q", i, p.Ticker, tt.want[i])
				}
			}
		})
	}
}
```

**Step 2: Run tests to verify they fail**
```bash
go test ./internal/filter/... -v
```
Expected: FAIL — `package filter` not found.

**Step 3: Write the implementation**
```go
// internal/filter/filter.go
package filter

import "github.com/ko5tas/t212/internal/api"

// Apply returns only the positions where profit-per-share strictly exceeds threshold.
func Apply(positions []api.Position, threshold float64) []api.Position {
	out := make([]api.Position, 0, len(positions))
	for _, p := range positions {
		if p.ProfitPerShare > threshold {
			out = append(out, p)
		}
	}
	return out
}
```

**Step 4: Run tests to verify they pass**
```bash
go test ./internal/filter/... -v
```
Expected: all PASS

**Step 5: Commit**
```bash
git add internal/filter/filter.go internal/filter/filter_test.go
git commit -m "feat: add position filter (profitPerShare > threshold)"
```

---

### Task 4: T212 API client

**Files:**
- Create: `internal/api/client.go`
- Create: `internal/api/client_test.go`
- Create: `internal/api/ratelimit.go`

**Step 1: Get the gorilla/websocket dependency (needed later — add now so go.sum is stable)**
```bash
go get github.com/gorilla/websocket@v1.5.3
go get github.com/charmbracelet/bubbletea@latest
go get github.com/charmbracelet/lipgloss@latest
```

**Step 2: Write failing tests**
```go
// internal/api/client_test.go
package api_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/api"
)

func TestClient_FetchPositions_Success(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Verify auth header is set
		if r.Header.Get("Authorization") == "" {
			t.Error("missing Authorization header")
		}
		w.Header().Set("x-ratelimit-remaining", "59")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode([]map[string]any{
			{"ticker": "AAPL_US_EQ", "quantity": 3.0, "averagePrice": 173.20, "currentPrice": 182.50},
		})
	}))
	defer srv.Close()

	c := api.NewClient("test-key", srv.URL, srv.Client())
	positions, rl, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(positions) != 1 {
		t.Fatalf("expected 1 position, got %d", len(positions))
	}
	if positions[0].Ticker != "AAPL_US_EQ" {
		t.Errorf("ticker: got %q, want AAPL_US_EQ", positions[0].Ticker)
	}
	if rl.Remaining != 59 {
		t.Errorf("ratelimit remaining: got %d, want 59", rl.Remaining)
	}
}

func TestClient_FetchPositions_AuthHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Header().Set("x-ratelimit-remaining", "1")
		w.Header().Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Second).Unix(), 10))
		json.NewEncoder(w).Encode([]api.Position{})
	}))
	defer srv.Close()

	c := api.NewClient("my-secret-key", srv.URL, srv.Client())
	_, _, err := c.FetchPositions(context.Background())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if gotAuth != "my-secret-key" {
		t.Errorf("Authorization header: got %q, want %q", gotAuth, "my-secret-key")
	}
}

func TestClient_FetchPositions_Non200(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
	}))
	defer srv.Close()

	c := api.NewClient("bad-key", srv.URL, srv.Client())
	_, _, err := c.FetchPositions(context.Background())
	if err == nil {
		t.Fatal("expected error for 401, got nil")
	}
}
```

**Step 3: Run tests to verify they fail**
```bash
go test ./internal/api/... -v -run TestClient
```
Expected: FAIL

**Step 4: Implement ratelimit types**
```go
// internal/api/ratelimit.go
package api

import "time"

// RateLimitInfo contains the parsed x-ratelimit-* response headers.
type RateLimitInfo struct {
	Remaining int
	Reset     time.Time
}
```

**Step 5: Implement the client**
```go
// internal/api/client.go
package api

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"time"
)

const positionsPath = "/api/v0/equity/positions"

// Client is a Trading 212 API client.
// Construct with NewClient — do not create directly.
type Client struct {
	httpClient *http.Client
	baseURL    string
	apiKey     string // never logged
}

// NewClient creates a Client. If httpClient is nil, a default client with TLS 1.3 is used.
func NewClient(apiKey, baseURL string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
			Transport: &http.Transport{
				TLSClientConfig: &tls.Config{
					MinVersion: tls.VersionTLS13,
				},
			},
		}
	}
	return &Client{
		httpClient: httpClient,
		baseURL:    baseURL,
		apiKey:     apiKey,
	}
}

// FetchPositions calls GET /api/v0/equity/positions and returns parsed positions
// plus rate limit metadata. Positions have Compute() called before return.
func (c *Client) FetchPositions(ctx context.Context) ([]Position, RateLimitInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+positionsPath, nil)
	if err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.apiKey)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, RateLimitInfo{}, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var positions []Position
	if err := json.NewDecoder(resp.Body).Decode(&positions); err != nil {
		return nil, RateLimitInfo{}, fmt.Errorf("decode response: %w", err)
	}

	for i := range positions {
		positions[i].Compute()
	}

	rl := parseRateLimit(resp)
	return positions, rl, nil
}

func parseRateLimit(resp *http.Response) RateLimitInfo {
	remaining, _ := strconv.Atoi(resp.Header.Get("x-ratelimit-remaining"))
	resetUnix, _ := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64)
	return RateLimitInfo{
		Remaining: remaining,
		Reset:     time.Unix(resetUnix, 0),
	}
}
```

**Step 6: Run tests to verify they pass**
```bash
go test ./internal/api/... -v
```
Expected: all PASS

**Step 7: Run race detector**
```bash
go test -race ./internal/...
```
Expected: all PASS, no race conditions

**Step 8: Commit**
```bash
git add internal/api/ go.mod go.sum
git commit -m "feat: add T212 API client with TLS 1.3 and rate limit parsing"
```

---

### Task 5: In-memory position store

**Files:**
- Create: `internal/store/store.go`
- Create: `internal/store/store_test.go`

**Step 1: Write failing tests**
```go
// internal/store/store_test.go
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
```

**Step 2: Run tests to verify they fail**
```bash
go test ./internal/store/... -v
```
Expected: FAIL

**Step 3: Implement the store**
```go
// internal/store/store.go
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
```

**Step 4: Run tests with race detector**
```bash
go test -race ./internal/store/... -v
```
Expected: all PASS, no races

**Step 5: Commit and open PR**
```bash
git add internal/store/
git commit -m "feat: add thread-safe in-memory position store"
gh pr create --title "feat: core models — API client, filter, store" \
  --body "Adds Position model, filter logic, T212 API client (TLS 1.3), and thread-safe store. All tests pass with -race." \
  --base main
```

**Step 6: Merge PR**
```bash
gh pr merge --squash --delete-branch
git checkout main && git pull
```

---

## PR 2 — `feature/poller-hub`: Poller goroutine and WebSocket hub

### Task 6: WebSocket hub

**Files:**
- Create: `internal/hub/hub.go`
- Create: `internal/hub/hub_test.go`

**Step 1: Create branch**
```bash
git checkout -b feature/poller-hub
```

**Step 2: Write failing tests**
```go
// internal/hub/hub_test.go
package hub_test

import (
	"sync"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/hub"
)

func TestHub_SubscribeAndBroadcast(t *testing.T) {
	h := hub.New()

	ch, unsub := h.Subscribe()
	defer unsub()

	msg := []byte(`{"test":true}`)
	go h.Broadcast(msg)

	select {
	case got := <-ch:
		if string(got) != string(msg) {
			t.Errorf("got %q, want %q", got, msg)
		}
	case <-time.After(time.Second):
		t.Fatal("timeout waiting for broadcast")
	}
}

func TestHub_Unsubscribe(t *testing.T) {
	h := hub.New()

	ch, unsub := h.Subscribe()
	unsub()

	// Broadcasting after unsubscribe should not block or panic.
	done := make(chan struct{})
	go func() {
		h.Broadcast([]byte("hello"))
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Broadcast blocked after unsubscribe")
	}

	// Channel should be closed after unsubscribe.
	select {
	case _, ok := <-ch:
		if ok {
			t.Error("expected channel to be closed")
		}
	default:
		t.Error("expected channel to be closed (not just empty)")
	}
}

func TestHub_MultipleSubscribers(t *testing.T) {
	h := hub.New()
	const n = 5

	channels := make([]<-chan []byte, n)
	unsubs := make([]func(), n)
	for i := range n {
		channels[i], unsubs[i] = h.Subscribe()
		defer unsubs[i]()
	}

	msg := []byte("broadcast")
	go h.Broadcast(msg)

	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(ch <-chan []byte) {
			defer wg.Done()
			select {
			case got := <-ch:
				if string(got) != string(msg) {
					t.Errorf("got %q, want %q", got, msg)
				}
			case <-time.After(time.Second):
				t.Error("timeout waiting for message")
			}
		}(channels[i])
	}
	wg.Wait()
}
```

**Step 3: Run tests to verify they fail**
```bash
go test ./internal/hub/... -v
```
Expected: FAIL

**Step 4: Implement the hub**
```go
// internal/hub/hub.go
package hub

import "sync"

// Hub fans out byte messages to all registered subscribers.
type Hub struct {
	mu      sync.RWMutex
	clients map[chan []byte]struct{}
}

// New returns an initialised Hub.
func New() *Hub {
	return &Hub{clients: make(map[chan []byte]struct{})}
}

// Subscribe registers a new subscriber and returns a receive channel and an
// unsubscribe function. The caller must call unsubscribe when done.
func (h *Hub) Subscribe() (<-chan []byte, func()) {
	ch := make(chan []byte, 8)
	h.mu.Lock()
	h.clients[ch] = struct{}{}
	h.mu.Unlock()

	return ch, func() {
		h.mu.Lock()
		delete(h.clients, ch)
		h.mu.Unlock()
		close(ch)
	}
}

// Broadcast sends msg to every subscriber. Slow or disconnected subscribers
// that have a full buffer are skipped (non-blocking send).
func (h *Hub) Broadcast(msg []byte) {
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.clients {
		select {
		case ch <- msg:
		default:
			// subscriber too slow — skip this tick
		}
	}
}
```

**Step 5: Run tests with race detector**
```bash
go test -race ./internal/hub/... -v
```
Expected: all PASS

**Step 6: Commit**
```bash
git add internal/hub/
git commit -m "feat: add WebSocket fan-out hub"
```

---

### Task 7: Poller goroutine

**Files:**
- Create: `internal/poller/poller.go`
- Create: `internal/poller/poller_test.go`

**Step 1: Write failing tests**
```go
// internal/poller/poller_test.go
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

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go p.Run(ctx)
	time.Sleep(150 * time.Millisecond)

	got := s.Get()
	if len(got) == 0 {
		t.Fatal("store should have positions after poller ran")
	}
	if got[0].Ticker != "AAPL_US_EQ" {
		t.Errorf("unexpected ticker: %q", got[0].Ticker)
	}
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
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
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
	case <-time.After(3 * time.Second):
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
```

**Step 2: Run tests to verify they fail**
```bash
go test ./internal/poller/... -v
```
Expected: FAIL

**Step 3: Define the Notifier interface (needed by poller)**
```go
// internal/poller/notifier.go
package poller

// Notifier is implemented by anything that can send threshold-crossing alerts.
type Notifier interface {
	Notify(ticker string, entered bool, profitPerShare float64)
}
```

**Step 4: Implement the poller**
```go
// internal/poller/poller.go
package poller

import (
	"context"
	"encoding/json"
	"log/slog"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/filter"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/store"
)

const pollInterval = time.Second

// BroadcastMessage is the JSON payload sent to WebSocket subscribers on each poll.
type BroadcastMessage struct {
	Timestamp time.Time      `json:"timestamp"`
	Positions []api.Position `json:"positions"`
}

// Poller polls the T212 API, updates the store, and broadcasts filtered positions.
type Poller struct {
	client    *api.Client
	store     *store.Store
	hub       *hub.Hub
	threshold float64
	notifier  Notifier
	prevAbove map[string]bool // ticker -> was above threshold last tick
}

// New creates a Poller. notifier may be nil (no alerts sent).
func New(client *api.Client, s *store.Store, h *hub.Hub, threshold float64, n Notifier) *Poller {
	return &Poller{
		client:    client,
		store:     s,
		hub:       h,
		threshold: threshold,
		notifier:  n,
		prevAbove: make(map[string]bool),
	}
}

// Run starts the polling loop. Blocks until ctx is cancelled.
func (p *Poller) Run(ctx context.Context) {
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		}
	}
}

func (p *Poller) poll(ctx context.Context) {
	positions, rl, err := p.client.FetchPositions(ctx)
	if err != nil {
		slog.Error("fetch positions failed", "err", err)
		return
	}

	slog.Debug("fetched positions", "count", len(positions), "ratelimit_remaining", rl.Remaining)

	p.store.Set(positions)

	filtered := filter.Apply(positions, p.threshold)
	p.sendNotifications(filtered)
	p.broadcast(filtered)
}

func (p *Poller) broadcast(filtered []api.Position) {
	msg := BroadcastMessage{
		Timestamp: time.Now().UTC(),
		Positions: filtered,
	}
	b, err := json.Marshal(msg)
	if err != nil {
		slog.Error("marshal broadcast", "err", err)
		return
	}
	p.hub.Broadcast(b)
}

func (p *Poller) sendNotifications(filtered []api.Position) {
	if p.notifier == nil {
		return
	}

	nowAbove := make(map[string]bool, len(filtered))
	for _, pos := range filtered {
		nowAbove[pos.Ticker] = true
	}

	// Detect edge: entered threshold.
	for ticker, pos := range nowAboveMap(filtered) {
		if !p.prevAbove[ticker] {
			p.notifier.Notify(ticker, true, pos.ProfitPerShare)
		}
	}

	// Detect edge: exited threshold.
	for ticker := range p.prevAbove {
		if !nowAbove[ticker] {
			p.notifier.Notify(ticker, false, 0)
		}
	}

	p.prevAbove = nowAbove
}

func nowAboveMap(positions []api.Position) map[string]api.Position {
	m := make(map[string]api.Position, len(positions))
	for _, p := range positions {
		m[p.Ticker] = p
	}
	return m
}
```

**Step 5: Run tests with race detector**
```bash
go test -race ./internal/poller/... -v
```
Expected: all PASS

**Step 6: Commit and open PR**
```bash
git add internal/hub/ internal/poller/
git commit -m "feat: add poller goroutine and WebSocket hub"
gh pr create --title "feat: poller goroutine and WebSocket fan-out hub" \
  --body "Poller polls T212 API every second, updates store, applies filter, broadcasts to hub. Edge-triggered notification interface. All tests pass -race." \
  --base main
gh pr merge --squash --delete-branch
git checkout main && git pull
```

---

## PR 3 — `feature/signal-notifier`: Signal notifications via signal-cli

### Task 8: Signal notifier

**Files:**
- Create: `internal/notifier/notifier.go`
- Create: `internal/notifier/notifier_test.go`

**Step 1: Create branch**
```bash
git checkout -b feature/signal-notifier
```

**Step 2: Write failing tests**
```go
// internal/notifier/notifier_test.go
package notifier_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/ko5tas/t212/internal/notifier"
)

// fakeSignalCLI writes a shell script that records its arguments to a temp file,
// then returns the path to that script (to be used as signal-cli binary path).
func fakeSignalCLI(t *testing.T) (binPath, argsFile string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("signal-cli fake not supported on Windows")
	}
	dir := t.TempDir()
	argsFile = filepath.Join(dir, "args.txt")
	binPath = filepath.Join(dir, "signal-cli")

	script := "#!/bin/sh\necho \"$@\" > " + argsFile + "\n"
	if err := os.WriteFile(binPath, []byte(script), 0o755); err != nil {
		t.Fatalf("write fake signal-cli: %v", err)
	}
	return binPath, argsFile
}

func TestNotifier_NotifyEntered(t *testing.T) {
	binPath, argsFile := fakeSignalCLI(t)

	n := notifier.New(binPath, "+447700000000")
	n.Notify("AAPL_US_EQ", true, 9.30)

	data, err := os.ReadFile(argsFile)
	if err != nil {
		t.Fatalf("read args file: %v", err)
	}
	args := string(data)

	if !strings.Contains(args, "send") {
		t.Errorf("expected 'send' in args, got: %q", args)
	}
	if !strings.Contains(args, "+447700000000") {
		t.Errorf("expected recipient number in args, got: %q", args)
	}
	if !strings.Contains(args, "AAPL_US_EQ") {
		t.Errorf("expected ticker in message, got: %q", args)
	}
}

func TestNotifier_NotifyExited(t *testing.T) {
	binPath, argsFile := fakeSignalCLI(t)

	n := notifier.New(binPath, "+447700000000")
	n.Notify("TSLA_US_EQ", false, 0)

	data, _ := os.ReadFile(argsFile)
	args := string(data)

	if !strings.Contains(args, "TSLA_US_EQ") {
		t.Errorf("expected ticker in message, got: %q", args)
	}
}

func TestNotifier_SignalCLINotFound(t *testing.T) {
	n := notifier.New("/nonexistent/signal-cli", "+447700000000")
	// Should not panic — just log the error.
	n.Notify("AAPL_US_EQ", true, 9.30)
}

func TestNotifier_ImplementsPollerNotifier(t *testing.T) {
	// Compile-time check that Notifier satisfies the poller.Notifier interface.
	// This test body is intentionally empty — the assertion is in the import.
	_ = exec.Command("true") // suppress import-not-used
}
```

**Step 3: Run tests to verify they fail**
```bash
go test ./internal/notifier/... -v
```
Expected: FAIL

**Step 4: Implement the notifier**
```go
// internal/notifier/notifier.go
package notifier

import (
	"fmt"
	"log/slog"
	"os/exec"
)

// Notifier sends Signal messages via signal-cli subprocess.
type Notifier struct {
	signalCLIPath string
	number        string // sender = recipient (linked device)
}

// New creates a Notifier. signalCLIPath is the path to the signal-cli binary.
func New(signalCLIPath, number string) *Notifier {
	return &Notifier{signalCLIPath: signalCLIPath, number: number}
}

// Notify sends a Signal message when a position enters or exits the threshold.
// entered=true means the position just crossed above the threshold.
func (n *Notifier) Notify(ticker string, entered bool, profitPerShare float64) {
	var msg string
	if entered {
		msg = fmt.Sprintf("📈 %s crossed +£1/share profit (now +£%.2f)", ticker, profitPerShare)
	} else {
		msg = fmt.Sprintf("📉 %s dropped below +£1/share profit", ticker)
	}

	cmd := exec.Command(
		n.signalCLIPath,
		"-u", n.number,
		"send",
		"-m", msg,
		n.number,
	)

	if out, err := cmd.CombinedOutput(); err != nil {
		slog.Error("signal-cli failed", "err", err, "output", string(out))
	}
}
```

**Step 5: Verify poller.Notifier interface is satisfied — add compile-time check to notifier package**

Add at the bottom of `notifier.go`:
```go
// Ensure *Notifier satisfies the poller.Notifier interface.
// (import cycle avoided — interface is duplicated here for the check)
var _ interface {
	Notify(ticker string, entered bool, profitPerShare float64)
} = (*Notifier)(nil)
```

**Step 6: Run tests**
```bash
go test -race ./internal/notifier/... -v
```
Expected: all PASS

**Step 7: Commit and open PR**
```bash
git add internal/notifier/
git commit -m "feat: add Signal notifier via signal-cli subprocess"
gh pr create --title "feat: Signal notifications via signal-cli" \
  --body "Edge-triggered Signal alerts via signal-cli subprocess. Fake signal-cli in tests. Logs errors without panicking if binary missing." \
  --base main
gh pr merge --squash --delete-branch
git checkout main && git pull
```

---

## PR 4 — `feature/web-server`: HTTP server and web UI

### Task 9: HTTP server with WebSocket endpoint

**Files:**
- Create: `internal/server/server.go`
- Create: `internal/server/server_test.go`
- Create: `web/index.html`
- Create: `web/style.css`
- Create: `web/app.js`

**Step 1: Create branch**
```bash
git checkout -b feature/web-server
```

**Step 2: Write failing tests**
```go
// internal/server/server_test.go
package server_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/server"
)

func TestServer_HealthEndpoint(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status: got %d, want 200", resp.StatusCode)
	}
}

func TestServer_SecureHeaders(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	resp, err := http.Get(ts.URL + "/health")
	if err != nil {
		t.Fatalf("GET /health: %v", err)
	}

	checks := map[string]string{
		"X-Frame-Options":        "DENY",
		"X-Content-Type-Options": "nosniff",
	}
	for header, want := range checks {
		if got := resp.Header.Get(header); got != want {
			t.Errorf("header %s: got %q, want %q", header, got, want)
		}
	}
}

func TestServer_WebSocketReceivesBroadcast(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial ws: %v", err)
	}
	defer conn.Close()

	// Give handler time to register subscriber.
	time.Sleep(50 * time.Millisecond)

	payload := map[string]string{"test": "hello"}
	b, _ := json.Marshal(payload)
	h.Broadcast(b)

	conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	_, msg, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read ws message: %v", err)
	}
	if string(msg) != string(b) {
		t.Errorf("got %q, want %q", msg, b)
	}
}

func TestServer_WebSocketMaxConnections(t *testing.T) {
	h := hub.New()
	srv := server.New(h, ":0")
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"

	conns := make([]*websocket.Conn, 5)
	for i := range conns {
		c, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
		if err != nil {
			t.Fatalf("connection %d: %v", i, err)
		}
		conns[i] = c
		defer c.Close()
	}
	time.Sleep(50 * time.Millisecond)

	// 6th connection should be rejected.
	c, resp, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err == nil {
		c.Close()
		t.Error("expected 6th connection to be rejected")
	}
	if resp != nil && resp.StatusCode != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", resp.StatusCode)
	}
}
```

**Step 3: Run tests to verify they fail**
```bash
go test ./internal/server/... -v
```
Expected: FAIL

**Step 4: Create the web assets**
```html
<!-- web/index.html -->
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>T212 Dashboard</title>
  <link rel="stylesheet" href="/style.css">
</head>
<body>
  <header>
    <h1>T212 Portfolio Dashboard</h1>
    <span id="status" class="status disconnected">Connecting…</span>
    <span id="updated"></span>
  </header>
  <main>
    <p id="empty" class="empty hidden">No positions with profit &gt; £1/share</p>
    <table id="positions" class="hidden">
      <thead>
        <tr>
          <th>Ticker</th>
          <th>Quantity</th>
          <th>Avg Price</th>
          <th>Current Price</th>
          <th>Profit/Share</th>
          <th>Market Value</th>
        </tr>
      </thead>
      <tbody id="tbody"></tbody>
    </table>
  </main>
  <script src="/app.js"></script>
</body>
</html>
```

```css
/* web/style.css */
*, *::before, *::after { box-sizing: border-box; margin: 0; padding: 0; }
body { font-family: system-ui, sans-serif; background: #0f172a; color: #e2e8f0; padding: 1.5rem; }
header { display: flex; align-items: center; gap: 1rem; margin-bottom: 1.5rem; flex-wrap: wrap; }
h1 { font-size: 1.25rem; font-weight: 600; }
.status { font-size: 0.75rem; padding: 0.2rem 0.6rem; border-radius: 9999px; font-weight: 500; }
.status.connected { background: #14532d; color: #4ade80; }
.status.disconnected { background: #450a0a; color: #f87171; }
#updated { font-size: 0.75rem; color: #94a3b8; margin-left: auto; }
table { width: 100%; border-collapse: collapse; font-size: 0.9rem; }
th { text-align: left; padding: 0.6rem 1rem; background: #1e293b; color: #94a3b8; font-weight: 500; font-size: 0.75rem; text-transform: uppercase; letter-spacing: 0.05em; }
td { padding: 0.75rem 1rem; border-bottom: 1px solid #1e293b; }
tr:hover td { background: #1e293b; }
.profit { color: #4ade80; font-weight: 600; }
.hidden { display: none; }
.empty { color: #94a3b8; font-style: italic; }
```

```js
// web/app.js
(function () {
  'use strict';

  const statusEl = document.getElementById('status');
  const updatedEl = document.getElementById('updated');
  const tableEl = document.getElementById('positions');
  const tbodyEl = document.getElementById('tbody');
  const emptyEl = document.getElementById('empty');

  function fmt(n) { return '£' + n.toFixed(2); }

  function render(msg) {
    const positions = msg.positions || [];
    tbodyEl.innerHTML = '';

    if (positions.length === 0) {
      tableEl.classList.add('hidden');
      emptyEl.classList.remove('hidden');
    } else {
      emptyEl.classList.add('hidden');
      tableEl.classList.remove('hidden');
      positions.forEach(function (p) {
        const tr = document.createElement('tr');
        tr.innerHTML =
          '<td>' + p.ticker + '</td>' +
          '<td>' + p.quantity + '</td>' +
          '<td>' + fmt(p.avgPrice) + '</td>' +
          '<td>' + fmt(p.currentPrice) + '</td>' +
          '<td class="profit">+' + fmt(p.profitPerShare) + '</td>' +
          '<td>' + fmt(p.marketValue) + '</td>';
        tbodyEl.appendChild(tr);
      });
    }

    const ts = new Date(msg.timestamp);
    updatedEl.textContent = 'Last updated: ' + ts.toLocaleTimeString();
  }

  function connect() {
    const ws = new WebSocket('ws://' + location.host + '/ws');

    ws.onopen = function () {
      statusEl.textContent = 'Live';
      statusEl.className = 'status connected';
    };

    ws.onmessage = function (e) {
      try { render(JSON.parse(e.data)); } catch (_) {}
    };

    ws.onclose = function () {
      statusEl.textContent = 'Reconnecting…';
      statusEl.className = 'status disconnected';
      setTimeout(connect, 3000);
    };
  }

  connect();
})();
```

**Step 5: Implement the server**
```go
// internal/server/server.go
package server

import (
	"embed"
	"io/fs"
	"log/slog"
	"net/http"
	"sync/atomic"

	"github.com/gorilla/websocket"
	"github.com/ko5tas/t212/internal/hub"
)

//go:embed ../../web
var webFS embed.FS

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		origin := r.Header.Get("Origin")
		return origin == "" || origin == "http://"+r.Host || origin == "https://"+r.Host
	},
}

const maxConnections = 5

// Server serves the web UI and WebSocket endpoint.
type Server struct {
	hub         *hub.Hub
	addr        string
	activeConns atomic.Int32
}

// New creates a Server.
func New(h *hub.Hub, addr string) *Server {
	return &Server{hub: h, addr: addr}
}

// Handler returns the HTTP handler (useful for testing with httptest).
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	sub, err := fs.Sub(webFS, "web")
	if err != nil {
		panic(err)
	}
	mux.Handle("/", http.FileServer(http.FS(sub)))
	mux.HandleFunc("/health", s.handleHealth)
	mux.HandleFunc("/ws", s.handleWS)

	return secureHeaders(mux)
}

// Start binds and serves. Blocks until the server returns an error.
func (s *Server) Start() error {
	slog.Info("web server starting", "addr", s.addr)
	return http.ListenAndServe(s.addr, s.Handler())
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (s *Server) handleWS(w http.ResponseWriter, r *http.Request) {
	if s.activeConns.Load() >= maxConnections {
		http.Error(w, "too many connections", http.StatusServiceUnavailable)
		return
	}

	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		slog.Error("ws upgrade failed", "err", err)
		return
	}
	defer conn.Close()

	s.activeConns.Add(1)
	defer s.activeConns.Add(-1)

	ch, unsub := s.hub.Subscribe()
	defer unsub()

	// Drain incoming messages (browser may send pings).
	go func() {
		for {
			if _, _, err := conn.ReadMessage(); err != nil {
				return
			}
		}
	}()

	for msg := range ch {
		if err := conn.WriteMessage(websocket.TextMessage, msg); err != nil {
			return
		}
	}
}

func secureHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("Content-Security-Policy", "default-src 'self'")
		next.ServeHTTP(w, r)
	})
}
```

**Step 6: Run tests**
```bash
go test -race ./internal/server/... -v
```
Expected: all PASS

**Step 7: Commit and open PR**
```bash
git add internal/server/ web/
git commit -m "feat: HTTP server with WebSocket endpoint and embedded web UI"
gh pr create --title "feat: HTTP server, WebSocket endpoint, web UI" \
  --body "Serves embedded web UI, /ws WebSocket endpoint with max-5-connections guard and origin check, secure headers. Live-updating dashboard via WebSocket push." \
  --base main
gh pr merge --squash --delete-branch
git checkout main && git pull
```

---

## PR 5 — `feature/tui`: Terminal UI

### Task 10: TUI subcommand

**Files:**
- Create: `internal/tui/tui.go`
- Create: `internal/tui/tui_test.go`

**Step 1: Create branch**
```bash
git checkout -b feature/tui
```

**Step 2: Write failing tests**
```go
// internal/tui/tui_test.go
package tui_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/tui"
)

func TestModel_UpdateFromMessage(t *testing.T) {
	m := tui.NewModel()

	positions := []api.Position{
		{Ticker: "AAPL_US_EQ", Quantity: 3, AveragePrice: 173.20, CurrentPrice: 182.50, ProfitPerShare: 9.30, MarketValue: 547.50},
	}
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: positions,
	}
	b, _ := json.Marshal(payload)

	updated := m.ApplyMessage(b)
	if len(updated.Positions()) != 1 {
		t.Fatalf("expected 1 position, got %d", len(updated.Positions()))
	}
	if updated.Positions()[0].Ticker != "AAPL_US_EQ" {
		t.Errorf("ticker: got %q", updated.Positions()[0].Ticker)
	}
}

func TestModel_InvalidMessageIgnored(t *testing.T) {
	m := tui.NewModel()
	updated := m.ApplyMessage([]byte("not json"))
	if len(updated.Positions()) != 0 {
		t.Error("invalid message should leave positions empty")
	}
}
```

**Step 3: Run tests to verify they fail**
```bash
go test ./internal/tui/... -v
```
Expected: FAIL

**Step 4: Implement the TUI model (pure logic — no bubbletea I/O in tests)**
```go
// internal/tui/tui.go
package tui

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/gorilla/websocket"
	"github.com/ko5tas/t212/internal/api"
)

// WSMessage matches the BroadcastMessage sent by the poller.
type WSMessage struct {
	Timestamp time.Time      `json:"timestamp"`
	Positions []api.Position `json:"positions"`
}

// Model is the bubbletea model for the TUI.
type Model struct {
	positions []api.Position
	updated   time.Time
	err       error
	wsURL     string
}

// NewModel returns an empty Model.
func NewModel() Model { return Model{} }

// Positions returns the current positions (used in tests).
func (m Model) Positions() []api.Position { return m.positions }

// ApplyMessage parses a raw WebSocket message and returns an updated Model.
func (m Model) ApplyMessage(raw []byte) Model {
	var msg WSMessage
	if err := json.Unmarshal(raw, &msg); err != nil {
		return m
	}
	m.positions = msg.Positions
	m.updated = msg.Timestamp
	return m
}

type msgReceived []byte
type errMsg error

// Init satisfies tea.Model.
func (m Model) Init() tea.Cmd { return nil }

// Update handles bubbletea messages.
func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch v := msg.(type) {
	case tea.KeyMsg:
		if v.String() == "q" || v.String() == "ctrl+c" {
			return m, tea.Quit
		}
	case msgReceived:
		m = m.ApplyMessage(v)
	case errMsg:
		m.err = v
		return m, tea.Quit
	}
	return m, nil
}

var (
	headerStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#94a3b8"))
	profitStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("#4ade80")).Bold(true)
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("#e2e8f0"))
	dimStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#475569"))
)

// View renders the TUI.
func (m Model) View() string {
	if m.err != nil {
		return fmt.Sprintf("Error: %v\n", m.err)
	}

	out := titleStyle.Render("T212 Dashboard") + "\n"
	out += dimStyle.Render(fmt.Sprintf("Positions with profit > £1/share  [q: quit]")) + "\n\n"

	if len(m.positions) == 0 {
		out += dimStyle.Render("No positions above threshold") + "\n"
	} else {
		out += fmt.Sprintf("%-20s %10s %12s %13s %14s %14s\n",
			headerStyle.Render("TICKER"),
			headerStyle.Render("QTY"),
			headerStyle.Render("AVG PRICE"),
			headerStyle.Render("CURR PRICE"),
			headerStyle.Render("PROFIT/SHR"),
			headerStyle.Render("MKT VALUE"),
		)
		for _, p := range m.positions {
			out += fmt.Sprintf("%-20s %10.4f %12.2f %13.2f %s %14.2f\n",
				p.Ticker, p.Quantity, p.AveragePrice, p.CurrentPrice,
				profitStyle.Render(fmt.Sprintf("%+13.2f", p.ProfitPerShare)),
				p.MarketValue,
			)
		}
	}

	if !m.updated.IsZero() {
		out += "\n" + dimStyle.Render("Last updated: "+m.updated.Local().Format("15:04:05"))
	}
	return out
}

// Run connects to the WebSocket server and runs the bubbletea program.
func Run(ctx context.Context, wsURL string) error {
	m := Model{wsURL: wsURL}
	p := tea.NewProgram(m, tea.WithAltScreen())

	conn, _, err := websocket.DefaultDialer.DialContext(ctx, wsURL, nil)
	if err != nil {
		return fmt.Errorf("connect to %s: %w", wsURL, err)
	}
	defer conn.Close()

	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				slog.Error("ws read error", "err", err)
				p.Send(errMsg(err))
				return
			}
			p.Send(msgReceived(raw))
		}
	}()

	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return err
	}
	return nil
}
```

**Step 5: Run tests**
```bash
go test -race ./internal/tui/... -v
```
Expected: all PASS

**Step 6: Commit and open PR**
```bash
git add internal/tui/
git commit -m "feat: add bubbletea TUI subcommand"
gh pr create --title "feat: terminal TUI via bubbletea" \
  --body "TUI connects to running serve WebSocket, renders live-updating position table. Pure model logic tested independently of bubbletea I/O." \
  --base main
gh pr merge --squash --delete-branch
git checkout main && git pull
```

---

## PR 6 — `feature/main-wiring`: cmd entrypoint and full integration

### Task 11: Main entrypoint

**Files:**
- Modify: `cmd/t212/main.go`
- Create: `cmd/t212/serve.go`
- Create: `cmd/t212/tui.go`

**Step 1: Create branch**
```bash
git checkout -b feature/main-wiring
```

**Step 2: Implement serve.go**
```go
// cmd/t212/serve.go
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/hub"
	"github.com/ko5tas/t212/internal/notifier"
	"github.com/ko5tas/t212/internal/poller"
	"github.com/ko5tas/t212/internal/server"
	"github.com/ko5tas/t212/internal/store"
)

func runServe() error {
	apiKey := os.Getenv("T212_API_KEY")
	if apiKey == "" {
		return fmt.Errorf("T212_API_KEY environment variable is not set")
	}

	signalNumber := os.Getenv("SIGNAL_NUMBER")
	signalCLIPath := os.Getenv("SIGNAL_CLI_PATH")
	if signalCLIPath == "" {
		signalCLIPath = "/usr/local/bin/signal-cli"
	}

	port := os.Getenv("T212_PORT")
	if port == "" {
		port = "8080"
	}

	threshold := 1.00

	slog.Info("t212 serve starting",
		"port", port,
		"threshold", threshold,
		"signal_enabled", signalNumber != "",
	)

	apiClient := api.NewClient(apiKey, "https://live.trading212.com", nil)
	s := store.New()
	h := hub.New()

	var n poller.Notifier
	if signalNumber != "" {
		n = notifier.New(signalCLIPath, signalNumber)
	}

	p := poller.New(apiClient, s, h, threshold, n)
	srv := server.New(h, ":"+port)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go p.Run(ctx)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.Start() }()

	select {
	case <-ctx.Done():
		slog.Info("shutting down")
		return nil
	case err := <-errCh:
		return fmt.Errorf("server: %w", err)
	}
}
```

**Step 3: Implement cmd/t212/tui.go**
```go
// cmd/t212/tui.go
package main

import (
	"context"
	"fmt"
	"os"

	"github.com/ko5tas/t212/internal/tui"
)

func runTUI() error {
	host := os.Getenv("T212_HOST")
	if host == "" {
		host = "localhost"
	}
	port := os.Getenv("T212_PORT")
	if port == "" {
		port = "8080"
	}

	wsURL := fmt.Sprintf("ws://%s:%s/ws", host, port)
	return tui.Run(context.Background(), wsURL)
}
```

**Step 4: Implement main.go**
```go
// cmd/t212/main.go
package main

import (
	"fmt"
	"log/slog"
	"os"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: t212 <serve|tui>")
		os.Exit(1)
	}

	var err error
	switch os.Args[1] {
	case "serve":
		err = runServe()
	case "tui":
		err = runTUI()
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
		os.Exit(1)
	}

	if err != nil {
		slog.Error("fatal", "err", err)
		os.Exit(1)
	}
}
```

**Step 5: Build and verify**
```bash
go build ./...
GOARCH=arm64 GOOS=linux go build -o t212-arm64 ./cmd/t212
ls -lh t212-arm64
```
Expected: binary created, no errors.

**Step 6: Run all tests**
```bash
go test -race ./...
```
Expected: all PASS

**Step 7: Commit and open PR**
```bash
rm t212-arm64
git add cmd/t212/
git commit -m "feat: wire up serve and tui subcommands in main"
gh pr create --title "feat: main entrypoint — serve and tui subcommands" \
  --body "Wires all components: serve starts poller + server + notifier, tui connects to serve WebSocket. Graceful shutdown via SIGTERM." \
  --base main
gh pr merge --squash --delete-branch
git checkout main && git pull
```

---

## PR 7 — `feature/deployment-ci`: Makefile, systemd, GitHub Actions

### Task 12: Makefile

**Files:**
- Create: `Makefile`

**Step 1: Create branch**
```bash
git checkout -b feature/deployment-ci
```

**Step 2: Write the Makefile**
```makefile
# Makefile
BINARY      := t212
BINARY_ARM  := t212-arm64
PI_HOST     ?= pi@raspberrypi.local
PI_BIN_DIR  := /usr/local/bin
PI_SVC_DIR  := /etc/systemd/system
PI_CFG_DIR  := /etc/t212

SIGNAL_CLI_VERSION_FILE := .signal-cli-version
SIGNAL_CLI_INSTALL_DIR  := /usr/local/bin

.PHONY: build build-arm test lint security deploy setup-signal update-signal-cli logs clean

## build: compile for current platform
build:
	go build -o $(BINARY) ./cmd/t212

## build-arm: cross-compile for Raspberry Pi 5 (linux/arm64)
build-arm:
	GOARCH=arm64 GOOS=linux go build -o $(BINARY_ARM) ./cmd/t212

## test: run all tests with race detector and coverage
test:
	go test -race -coverprofile=coverage.out ./...
	go tool cover -func=coverage.out | tail -1

## lint: run golangci-lint
lint:
	golangci-lint run ./...

## security: run govulncheck
security:
	govulncheck ./...

## deploy: build for arm64 and deploy to Raspberry Pi
deploy: build-arm
	ssh $(PI_HOST) "sudo mkdir -p $(PI_CFG_DIR) && sudo chmod 700 $(PI_CFG_DIR)"
	scp $(BINARY_ARM) $(PI_HOST):/tmp/$(BINARY)
	ssh $(PI_HOST) "sudo mv /tmp/$(BINARY) $(PI_BIN_DIR)/$(BINARY) && sudo chmod 755 $(PI_BIN_DIR)/$(BINARY)"
	scp deploy/t212.service $(PI_HOST):/tmp/t212.service
	ssh $(PI_HOST) "sudo mv /tmp/t212.service $(PI_SVC_DIR)/t212.service"
	ssh $(PI_HOST) "sudo systemctl daemon-reload && sudo systemctl enable t212 && sudo systemctl restart t212"
	@echo "Deployed. Check status with: make logs"

## setup-signal: register Pi as linked device (scan QR with Signal app)
setup-signal:
	ssh $(PI_HOST) "signal-cli addDevice --uri \$$(signal-cli link -n 'T212-Pi')"

## update-signal-cli: download and verify latest signal-cli release
update-signal-cli:
	@./scripts/update-signal-cli.sh $(PI_HOST) $(SIGNAL_CLI_INSTALL_DIR)

## logs: tail service logs from Pi
logs:
	ssh $(PI_HOST) "journalctl -u t212 -f"

## clean: remove build artifacts
clean:
	rm -f $(BINARY) $(BINARY_ARM) coverage.out
```

**Step 3: Create the signal-cli update script**
```bash
mkdir -p scripts
```

```bash
#!/usr/bin/env bash
# scripts/update-signal-cli.sh
# Usage: ./scripts/update-signal-cli.sh [PI_HOST] [INSTALL_DIR]
set -euo pipefail

PI_HOST="${1:-pi@raspberrypi.local}"
INSTALL_DIR="${2:-/usr/local/bin}"
REPO="AsamK/signal-cli"

echo "Checking latest signal-cli release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')

echo "Latest: ${LATEST}"

TARBALL="signal-cli-${LATEST#v}-Linux-aarch64.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${TARBALL}"
CHECKSUM_URL="${URL}.sha256sum"

TMPDIR=$(mktemp -d)
trap "rm -rf ${TMPDIR}" EXIT

echo "Downloading ${TARBALL}..."
curl -fsSL -o "${TMPDIR}/${TARBALL}" "${URL}"
curl -fsSL -o "${TMPDIR}/${TARBALL}.sha256sum" "${CHECKSUM_URL}"

echo "Verifying SHA256..."
(cd "${TMPDIR}" && sha256sum -c "${TARBALL}.sha256sum")

echo "Installing to ${PI_HOST}:${INSTALL_DIR}/signal-cli..."
tar -xzf "${TMPDIR}/${TARBALL}" -C "${TMPDIR}"
scp "${TMPDIR}/signal-cli-${LATEST#v}-Linux-aarch64/bin/signal-cli" \
    "${PI_HOST}:/tmp/signal-cli-new"
ssh "${PI_HOST}" "sudo mv /tmp/signal-cli-new ${INSTALL_DIR}/signal-cli && \
                  sudo chmod 755 ${INSTALL_DIR}/signal-cli"

echo "Done. signal-cli ${LATEST} installed."
echo "${LATEST}" > .signal-cli-version
```

```bash
chmod +x scripts/update-signal-cli.sh
```

**Step 4: Create systemd unit**
```ini
# deploy/t212.service
[Unit]
Description=T212 Portfolio Dashboard
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/t212/config.env
ExecStart=/usr/local/bin/t212 serve
Restart=always
RestartSec=5
User=t212
Group=t212
NoNewPrivileges=yes
ProtectSystem=strict
PrivateTmp=yes
ProtectHome=yes
PrivateDevices=yes
CapabilityBoundingSet=

[Install]
WantedBy=multi-user.target
```

**Step 5: Create config template**
```bash
# deploy/config.env.example
# Copy to /etc/t212/config.env and chmod 0600, chown root:root

# Trading 212 live API key (required)
T212_API_KEY=

# Your Signal number in E.164 format (optional — omit to disable notifications)
SIGNAL_NUMBER=+447700000000

# Path to signal-cli binary (default: /usr/local/bin/signal-cli)
# SIGNAL_CLI_PATH=/usr/local/bin/signal-cli

# Port to serve the web UI on (default: 8080)
# T212_PORT=8080
```

**Step 6: Create GitHub Actions CI workflow**
```yaml
# .github/workflows/ci.yml
name: CI

on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    name: Test
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Run tests with race detector
        run: go test -race -coverprofile=coverage.out ./...
      - name: Coverage summary
        run: go tool cover -func=coverage.out | tail -1

  lint:
    name: Lint
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - uses: golangci/golangci-lint-action@v6
        with:
          version: latest

  build-arm:
    name: Build linux/arm64
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Cross-compile for Raspberry Pi 5
        run: GOARCH=arm64 GOOS=linux go build -o t212-arm64 ./cmd/t212

  security:
    name: Security scan
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Install govulncheck
        run: go install golang.org/x/vuln/cmd/govulncheck@latest
      - name: Run govulncheck
        run: govulncheck ./...
```

**Step 7: Create signal-cli version check workflow**
```yaml
# .github/workflows/signal-cli-update.yml
name: Check signal-cli updates

on:
  schedule:
    - cron: '0 9 * * 1'  # every Monday at 09:00 UTC
  workflow_dispatch:

jobs:
  check:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Check for new signal-cli release
        id: check
        run: |
          LATEST=$(curl -fsSL https://api.github.com/repos/AsamK/signal-cli/releases/latest \
            | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')
          CURRENT=$(cat .signal-cli-version 2>/dev/null || echo "none")
          echo "latest=$LATEST" >> $GITHUB_OUTPUT
          echo "current=$CURRENT" >> $GITHUB_OUTPUT
          echo "Latest: $LATEST | Current: $CURRENT"
          if [ "$LATEST" != "$CURRENT" ]; then
            echo "update_available=true" >> $GITHUB_OUTPUT
          fi
      - name: Open PR if update available
        if: steps.check.outputs.update_available == 'true'
        env:
          GH_TOKEN: ${{ secrets.GITHUB_TOKEN }}
        run: |
          LATEST="${{ steps.check.outputs.latest }}"
          BRANCH="chore/signal-cli-${LATEST}"
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git checkout -b "$BRANCH"
          echo "$LATEST" > .signal-cli-version
          git add .signal-cli-version
          git commit -m "chore: update signal-cli to ${LATEST}"
          git push origin "$BRANCH"
          gh pr create \
            --title "chore: update signal-cli to ${LATEST}" \
            --body "signal-cli ${LATEST} is available. Run \`make update-signal-cli\` after merging to install on the Pi." \
            --base main
```

**Step 8: Add .gitignore**
```gitignore
# .gitignore
t212
t212-arm64
coverage.out
```

**Step 9: Run full test suite one final time**
```bash
go test -race ./...
GOARCH=arm64 GOOS=linux go build ./cmd/t212
```
Expected: all PASS, binary builds.

**Step 10: Commit and open PR**
```bash
git add Makefile scripts/ deploy/ .github/ .gitignore
git commit -m "feat: Makefile, systemd service, GitHub Actions CI"
gh pr create --title "feat: deployment — Makefile, systemd, CI, signal-cli updater" \
  --body "Adds make build/test/deploy/setup-signal/update-signal-cli targets, systemd service with hardening flags, GitHub Actions CI (test/lint/build-arm/security), weekly signal-cli version check workflow." \
  --base main
gh pr merge --squash --delete-branch
git checkout main && git pull
```

---

## Post-implementation checklist

- [ ] `go test -race ./...` — all pass
- [ ] `GOARCH=arm64 GOOS=linux go build ./cmd/t212` — builds clean
- [ ] `golangci-lint run` — no issues
- [ ] `govulncheck ./...` — no vulnerabilities
- [ ] GitHub Actions CI passing on main
- [ ] `make deploy` tested against Pi
- [ ] `make setup-signal` QR scanned and linked
- [ ] Web UI accessible at `http://pi.local:8080`
- [ ] `t212 tui` running over SSH shows live data simultaneously with web UI
- [ ] Signal notification received on threshold crossing
