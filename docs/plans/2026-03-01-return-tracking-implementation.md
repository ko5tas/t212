# Return Tracking Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Show realised returns (sells + dividends) per position with RETURN, RETURN %, and NET ROI % columns, fetched from T212 history API.

**Architecture:** New history types + paginated API client methods + history store. Poller fetches history at startup and hourly, merges with positions before broadcast. TUI/Web send refresh requests upstream via WebSocket.

**Tech Stack:** Go 1.22+, gorilla/websocket, charmbracelet/bubbletea, T212 REST API v0.

---

### Task 1: History Wire Types and ReturnInfo Model

**Files:**
- Create: `internal/api/history.go`
- Test: `internal/api/history_test.go`

**Step 1: Write the test for ReturnInfo JSON serialisation**

```go
// internal/api/history_test.go
package api_test

import (
	"encoding/json"
	"testing"

	"github.com/ko5tas/t212/internal/api"
)

func TestReturnInfo_JSON(t *testing.T) {
	ri := api.ReturnInfo{
		TotalBought:    100.00,
		TotalSold:      35.00,
		TotalDividends: 7.30,
		Return:         42.30,
		ReturnPct:      42.30,
		NetROIPct:      65.08,
	}
	b, err := json.Marshal(ri)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got api.ReturnInfo
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.Return != 42.30 {
		t.Errorf("Return: got %v, want 42.30", got.Return)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestReturnInfo_JSON -v`
Expected: FAIL — `api.ReturnInfo` undefined.

**Step 3: Write the history types**

```go
// internal/api/history.go
package api

import "time"

// ReturnInfo holds realised return data for a single position.
type ReturnInfo struct {
	TotalBought    float64 `json:"totalBought"`
	TotalSold      float64 `json:"totalSold"`
	TotalDividends float64 `json:"totalDividends"`
	Return         float64 `json:"return"`
	ReturnPct      float64 `json:"returnPct"`
	NetROIPct      float64 `json:"netRoiPct"`
}

// HistoricalOrder is one item from GET /api/v0/equity/history/orders.
type HistoricalOrder struct {
	Fill struct {
		Price    float64   `json:"price"`
		Quantity float64   `json:"quantity"`
		FilledAt time.Time `json:"filledAt"`
		Impact   struct {
			NetValue float64 `json:"netValue"`
			Currency string  `json:"currency"`
		} `json:"walletImpact"`
	} `json:"fill"`
	Order struct {
		Ticker string `json:"ticker"`
		Side   string `json:"side"`
		Status string `json:"status"`
	} `json:"order"`
}

// DividendItem is one item from GET /api/v0/equity/history/dividends.
type DividendItem struct {
	Amount float64   `json:"amount"`
	Ticker string    `json:"ticker"`
	PaidOn time.Time `json:"paidOn"`
}

// PaginatedResponse wraps the T212 paginated list format.
type PaginatedResponse[T any] struct {
	Items        []T     `json:"items"`
	NextPagePath *string `json:"nextPagePath"`
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestReturnInfo_JSON -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/api/history.go internal/api/history_test.go
git commit -m "feat: add history wire types and ReturnInfo model"
```

---

### Task 2: Add Returns Field to Position

**Files:**
- Modify: `internal/api/position.go`
- Modify: `internal/api/position_test.go`

**Step 1: Write the test**

Add to `internal/api/position_test.go`:

```go
func TestPosition_ReturnsNilByDefault(t *testing.T) {
	p := api.Position{Ticker: "AAPL_US_EQ"}
	if p.Returns != nil {
		t.Error("Returns should be nil by default")
	}
}

func TestPosition_ReturnsInJSON(t *testing.T) {
	ri := &api.ReturnInfo{Return: 42.30, ReturnPct: 42.30, NetROIPct: 65.08}
	p := api.Position{Ticker: "AAPL_US_EQ", Returns: ri}
	b, _ := json.Marshal(p)
	var got api.Position
	json.Unmarshal(b, &got)
	if got.Returns == nil {
		t.Fatal("Returns should not be nil after unmarshal")
	}
	if got.Returns.Return != 42.30 {
		t.Errorf("Return: got %v, want 42.30", got.Returns.Return)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestPosition_Returns -v`
Expected: FAIL — `p.Returns` undefined.

**Step 3: Add Returns field to Position**

In `internal/api/position.go`, add after the `MarketValue` field:

```go
	Returns        *ReturnInfo `json:"returns,omitempty"`
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestPosition_Returns -v`
Expected: PASS

**Step 5: Run all tests to check nothing broke**

Run: `go test ./...`
Expected: All PASS

**Step 6: Commit**

```
git add internal/api/position.go internal/api/position_test.go
git commit -m "feat: add Returns field to Position"
```

---

### Task 3: API Client — Paginated FetchOrderHistory

**Files:**
- Modify: `internal/api/client.go`
- Modify: `internal/api/client_test.go`

**Step 1: Write the test**

Add to `internal/api/client_test.go`:

```go
func TestClient_FetchOrderHistory(t *testing.T) {
	page := 0
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/equity/history/orders" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		page++
		if page == 1 {
			// First page: return 1 item + nextPagePath
			next := "/api/v0/equity/history/orders?cursor=123&limit=50"
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"fill": map[string]any{
							"price": 150.0, "quantity": 0.5, "filledAt": "2025-11-17T10:00:00Z",
							"walletImpact": map[string]any{"netValue": 75.0, "currency": "GBP"},
						},
						"order": map[string]any{"ticker": "GOOGL_US_EQ", "side": "BUY", "status": "FILLED"},
					},
				},
				"nextPagePath": next,
			})
		} else {
			// Second page: 1 more item, no next page
			json.NewEncoder(w).Encode(map[string]any{
				"items": []map[string]any{
					{
						"fill": map[string]any{
							"price": 155.0, "quantity": 0.2, "filledAt": "2025-12-01T10:00:00Z",
							"walletImpact": map[string]any{"netValue": 31.0, "currency": "GBP"},
						},
						"order": map[string]any{"ticker": "GOOGL_US_EQ", "side": "SELL", "status": "FILLED"},
					},
				},
				"nextPagePath": nil,
			})
		}
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	orders, err := c.FetchOrderHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(orders) != 2 {
		t.Fatalf("expected 2 orders, got %d", len(orders))
	}
	if orders[0].Order.Side != "BUY" {
		t.Errorf("order[0] side: got %q, want BUY", orders[0].Order.Side)
	}
	if orders[1].Fill.Impact.NetValue != 31.0 {
		t.Errorf("order[1] netValue: got %v, want 31.0", orders[1].Fill.Impact.NetValue)
	}
}

func TestClient_FetchOrderHistory_ByTicker(t *testing.T) {
	var gotTicker string
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotTicker = r.URL.Query().Get("ticker")
		json.NewEncoder(w).Encode(map[string]any{"items": []any{}, "nextPagePath": nil})
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	c.FetchOrderHistory(context.Background(), "AAPL_US_EQ")
	if gotTicker != "AAPL_US_EQ" {
		t.Errorf("ticker param: got %q, want AAPL_US_EQ", gotTicker)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestClient_FetchOrderHistory -v`
Expected: FAIL — method undefined.

**Step 3: Implement FetchOrderHistory**

Add to `internal/api/client.go`:

```go
const (
	orderHistoryPath    = "/api/v0/equity/history/orders"
	dividendHistoryPath = "/api/v0/equity/history/dividends"
	historyPageLimit    = 50
	historyRateDelay    = 11 * time.Second // stay under 6 req/min
)

// FetchOrderHistory fetches all order fills, paginating automatically.
// Pass "" for ticker to fetch all stocks.
func (c *Client) FetchOrderHistory(ctx context.Context, ticker string) ([]HistoricalOrder, error) {
	path := orderHistoryPath + fmt.Sprintf("?limit=%d", historyPageLimit)
	if ticker != "" {
		path += "&ticker=" + ticker
	}
	var all []HistoricalOrder
	for {
		page, nextPath, err := c.fetchHistoryPage[HistoricalOrder](ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetch order history: %w", err)
		}
		all = append(all, page...)
		if nextPath == nil {
			break
		}
		path = *nextPath
		// Rate limit: sleep between pages
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(historyRateDelay):
		}
	}
	return all, nil
}

func (c *Client) fetchHistoryPage[T any](ctx context.Context, path string) ([]T, *string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Authorization", c.authHeader)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, nil, fmt.Errorf("http request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}

	var page PaginatedResponse[T]
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, nil, fmt.Errorf("decode: %w", err)
	}
	return page.Items, page.NextPagePath, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestClient_FetchOrderHistory -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/api/client.go internal/api/client_test.go
git commit -m "feat: add paginated FetchOrderHistory to API client"
```

---

### Task 4: API Client — FetchDividendHistory

**Files:**
- Modify: `internal/api/client.go`
- Modify: `internal/api/client_test.go`

**Step 1: Write the test**

Add to `internal/api/client_test.go`:

```go
func TestClient_FetchDividendHistory(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v0/equity/history/dividends" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		json.NewEncoder(w).Encode(map[string]any{
			"items": []map[string]any{
				{"amount": 3.50, "ticker": "GOOGL_US_EQ", "paidOn": "2025-12-15T00:00:00Z"},
			},
			"nextPagePath": nil,
		})
	}))
	defer srv.Close()

	c := api.NewClient("k", "s", srv.URL, srv.Client())
	divs, err := c.FetchDividendHistory(context.Background(), "")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(divs) != 1 {
		t.Fatalf("expected 1 dividend, got %d", len(divs))
	}
	if divs[0].Amount != 3.50 {
		t.Errorf("amount: got %v, want 3.50", divs[0].Amount)
	}
	if divs[0].Ticker != "GOOGL_US_EQ" {
		t.Errorf("ticker: got %q, want GOOGL_US_EQ", divs[0].Ticker)
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/api/ -run TestClient_FetchDividendHistory -v`
Expected: FAIL — method undefined.

**Step 3: Implement FetchDividendHistory**

Add to `internal/api/client.go`:

```go
// FetchDividendHistory fetches all dividend payments, paginating automatically.
// Pass "" for ticker to fetch all stocks.
func (c *Client) FetchDividendHistory(ctx context.Context, ticker string) ([]DividendItem, error) {
	path := dividendHistoryPath + fmt.Sprintf("?limit=%d", historyPageLimit)
	if ticker != "" {
		path += "&ticker=" + ticker
	}
	var all []DividendItem
	for {
		page, nextPath, err := c.fetchHistoryPage[DividendItem](ctx, path)
		if err != nil {
			return nil, fmt.Errorf("fetch dividend history: %w", err)
		}
		all = append(all, page...)
		if nextPath == nil {
			break
		}
		path = *nextPath
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(historyRateDelay):
		}
	}
	return all, nil
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/api/ -run TestClient_FetchDividendHistory -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/api/client.go internal/api/client_test.go
git commit -m "feat: add paginated FetchDividendHistory to API client"
```

---

### Task 5: History Store

**Files:**
- Create: `internal/history/store.go`
- Create: `internal/history/store_test.go`

**Step 1: Write the tests**

```go
// internal/history/store_test.go
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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/history/ -v`
Expected: FAIL — package not found.

**Step 3: Implement the store**

```go
// internal/history/store.go
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
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/history/ -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/history/store.go internal/history/store_test.go
git commit -m "feat: add history store for per-ticker return data"
```

---

### Task 6: ComputeReturns Pure Function

**Files:**
- Create: `internal/history/compute.go`
- Create: `internal/history/compute_test.go`

**Step 1: Write the tests**

```go
// internal/history/compute_test.go
package history_test

import (
	"math"
	"testing"

	"github.com/ko5tas/t212/internal/api"
	"github.com/ko5tas/t212/internal/history"
)

func approx(a, b float64) bool {
	return math.Abs(a-b) < 0.01
}

func TestComputeReturns_BuysAndSells(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0),
		makeOrder("SELL", 35.0),
	}
	divs := []api.DividendItem{{Amount: 7.30}}

	ri := history.ComputeReturns(orders, divs, 2.50, 3.0) // unrealised per share=2.50, qty=3.0

	if !approx(ri.TotalBought, 100.0) {
		t.Errorf("TotalBought: got %v, want 100.0", ri.TotalBought)
	}
	if !approx(ri.TotalSold, 35.0) {
		t.Errorf("TotalSold: got %v, want 35.0", ri.TotalSold)
	}
	if !approx(ri.TotalDividends, 7.30) {
		t.Errorf("TotalDividends: got %v, want 7.30", ri.TotalDividends)
	}
	if !approx(ri.Return, 42.30) {
		t.Errorf("Return: got %v, want 42.30", ri.Return)
	}
	if !approx(ri.ReturnPct, 42.30) {
		t.Errorf("ReturnPct: got %v, want 42.30", ri.ReturnPct)
	}
	// NetROIPct = (42.30 + 7.50) / (100.0 - 35.0) * 100 = 49.80 / 65.0 * 100 = 76.62
	if !approx(ri.NetROIPct, 76.62) {
		t.Errorf("NetROIPct: got %v, want ~76.62", ri.NetROIPct)
	}
}

func TestComputeReturns_NoBuys(t *testing.T) {
	ri := history.ComputeReturns(nil, nil, 0, 0)
	if ri.ReturnPct != 0 {
		t.Errorf("ReturnPct should be 0 with no buys, got %v", ri.ReturnPct)
	}
}

func TestComputeReturns_OnlyDividends(t *testing.T) {
	orders := []api.HistoricalOrder{makeOrder("BUY", 200.0)}
	divs := []api.DividendItem{{Amount: 10.0}}
	ri := history.ComputeReturns(orders, divs, 5.0, 2.0)
	if !approx(ri.Return, 10.0) {
		t.Errorf("Return: got %v, want 10.0", ri.Return)
	}
}

func TestComputeReturns_SkipsNonFilled(t *testing.T) {
	orders := []api.HistoricalOrder{
		makeOrder("BUY", 100.0),
		makeOrderWithStatus("SELL", 50.0, "CANCELLED"),
	}
	ri := history.ComputeReturns(orders, nil, 0, 0)
	if ri.TotalSold != 0 {
		t.Errorf("cancelled sell should be ignored, got TotalSold=%v", ri.TotalSold)
	}
}

func makeOrder(side string, netValue float64) api.HistoricalOrder {
	return makeOrderWithStatus(side, netValue, "FILLED")
}

func makeOrderWithStatus(side string, netValue float64, status string) api.HistoricalOrder {
	var o api.HistoricalOrder
	o.Order.Side = side
	o.Order.Status = status
	o.Fill.Impact.NetValue = netValue
	return o
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/history/ -run TestComputeReturns -v`
Expected: FAIL — `history.ComputeReturns` undefined.

**Step 3: Implement ComputeReturns**

```go
// internal/history/compute.go
package history

import "github.com/ko5tas/t212/internal/api"

// ComputeReturns aggregates order fills and dividends into a ReturnInfo.
// unrealisedPPS is the current ProfitPerShare and qty is the current quantity held.
func ComputeReturns(orders []api.HistoricalOrder, divs []api.DividendItem, unrealisedPPS, qty float64) api.ReturnInfo {
	var bought, sold float64
	for _, o := range orders {
		if o.Order.Status != "FILLED" {
			continue
		}
		v := o.Fill.Impact.NetValue
		if v < 0 {
			v = -v
		}
		switch o.Order.Side {
		case "BUY":
			bought += v
		case "SELL":
			sold += v
		}
	}

	var dividends float64
	for _, d := range divs {
		dividends += d.Amount
	}

	ret := sold + dividends
	var retPct, netROI float64
	if bought > 0 {
		retPct = ret / bought * 100
	}
	unrealised := unrealisedPPS * qty
	netInvested := bought - sold
	if netInvested > 0 {
		netROI = (ret + unrealised) / netInvested * 100
	}

	return api.ReturnInfo{
		TotalBought:    bought,
		TotalSold:      sold,
		TotalDividends: dividends,
		Return:         ret,
		ReturnPct:      retPct,
		NetROIPct:      netROI,
	}
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/history/ -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/history/compute.go internal/history/compute_test.go
git commit -m "feat: add ComputeReturns pure function"
```

---

### Task 7: Poller — History Fetch and Merge

This is the largest task. The poller gains:
- A history store reference
- Startup history fetch (before first poll)
- Hourly background refresh
- Per-stock refresh via a request channel
- Merge Returns into Position before broadcast

**Files:**
- Modify: `internal/poller/poller.go`
- Modify: `internal/poller/poller_test.go`

**Step 1: Write a test for history merge into broadcast**

Add to `internal/poller/poller_test.go`:

```go
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
	hs.Set("AAPL_US_EQ", api.ReturnInfo{Return: 42.30, ReturnPct: 42.30, NetROIPct: 65.0})

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
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/poller/ -run TestPoller_BroadcastIncludesReturns -v`
Expected: FAIL — `poller.WithHistoryStore` undefined.

**Step 3: Add history store to poller + merge logic**

Modify `internal/poller/poller.go`:

1. Add import: `"github.com/ko5tas/t212/internal/history"`

2. Add field to `Poller` struct:
```go
	historyStore *history.Store
	refreshCh    chan string // ticker or "" for all
```

3. Add option function:
```go
// Option configures a Poller.
type Option func(*Poller)

// WithHistoryStore attaches a history store for return data.
func WithHistoryStore(hs *history.Store) Option {
	return func(p *Poller) { p.historyStore = hs }
}

// WithRefreshChan attaches a channel for receiving refresh requests.
func WithRefreshChan(ch chan string) Option {
	return func(p *Poller) { p.refreshCh = ch }
}
```

4. Update `New` and `NewForTesting` to accept `...Option`:
```go
func New(client *api.Client, s *store.Store, h *hub.Hub, threshold float64, n Notifier, opts ...Option) *Poller {
	p := &Poller{
		client:    client,
		store:     s,
		hub:       h,
		threshold: threshold,
		notifier:  n,
		prevAbove: make(map[string]bool),
		interval:  pollInterval,
	}
	for _, o := range opts {
		o(p)
	}
	return p
}

func NewForTesting(client *api.Client, s *store.Store, h *hub.Hub, threshold float64, n Notifier, interval time.Duration, opts ...Option) *Poller {
	p := New(client, s, h, threshold, n, opts...)
	p.interval = interval
	return p
}
```

5. Add merge step in `broadcast`:
```go
func (p *Poller) attachReturns(positions []api.Position) {
	if p.historyStore == nil {
		return
	}
	for i := range positions {
		positions[i].Returns = p.historyStore.Get(positions[i].Ticker)
	}
}
```

6. Call `p.attachReturns(filtered)` in `poll()` right before `p.broadcast(filtered)`.

7. Add hourly refresh + refresh channel handling in `Run`:
```go
func (p *Poller) Run(ctx context.Context) {
	p.poll(ctx)

	ticker := time.NewTicker(p.interval)
	defer ticker.Stop()

	var historyTicker *time.Ticker
	if p.historyStore != nil {
		historyTicker = time.NewTicker(time.Hour)
		defer historyTicker.Stop()
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			p.poll(ctx)
		case t := <-p.refreshChOrNil():
			p.refreshHistory(ctx, t)
			p.poll(ctx) // re-broadcast with updated returns
		case <-p.historyTickerOrNil(historyTicker):
			p.refreshHistory(ctx, "")
		}
	}
}

func (p *Poller) refreshChOrNil() <-chan string {
	if p.refreshCh == nil {
		return nil
	}
	return p.refreshCh
}

func (p *Poller) historyTickerOrNil(t *time.Ticker) <-chan time.Time {
	if t == nil {
		return nil
	}
	return t.C
}

func (p *Poller) refreshHistory(ctx context.Context, ticker string) {
	if p.historyStore == nil {
		return
	}
	slog.Info("refreshing history", "ticker", ticker)

	orders, err := p.client.FetchOrderHistory(ctx, ticker)
	if err != nil {
		slog.Error("fetch order history", "err", err)
		return
	}
	divs, err := p.client.FetchDividendHistory(ctx, ticker)
	if err != nil {
		slog.Error("fetch dividend history", "err", err)
		return
	}

	positions := p.store.Get()

	if ticker == "" {
		// Full refresh: group by ticker
		ordersByTicker := make(map[string][]api.HistoricalOrder)
		for _, o := range orders {
			ordersByTicker[o.Order.Ticker] = append(ordersByTicker[o.Order.Ticker], o)
		}
		divsByTicker := make(map[string][]api.DividendItem)
		for _, d := range divs {
			divsByTicker[d.Ticker] = append(divsByTicker[d.Ticker], d)
		}
		all := make(map[string]api.ReturnInfo)
		tickers := make(map[string]bool)
		for t := range ordersByTicker {
			tickers[t] = true
		}
		for t := range divsByTicker {
			tickers[t] = true
		}
		for t := range tickers {
			pps, qty := findPosition(positions, t)
			all[t] = history.ComputeReturns(ordersByTicker[t], divsByTicker[t], pps, qty)
		}
		p.historyStore.SetAll(all)
	} else {
		// Per-stock refresh
		pps, qty := findPosition(positions, ticker)
		ri := history.ComputeReturns(orders, divs, pps, qty)
		p.historyStore.Set(ticker, ri)
	}
}

func findPosition(positions []api.Position, ticker string) (profitPerShare, quantity float64) {
	for _, p := range positions {
		if p.Ticker == ticker {
			return p.ProfitPerShare, p.Quantity
		}
	}
	return 0, 0
}
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/poller/ -run TestPoller_BroadcastIncludesReturns -v`
Expected: PASS

**Step 5: Run all tests**

Run: `go test ./...`
Expected: All PASS. Note: existing `poller.New` and `NewForTesting` calls don't pass options, so they still work unchanged.

**Step 6: Commit**

```
git add internal/poller/poller.go internal/poller/poller_test.go
git commit -m "feat: integrate history store into poller with merge and refresh"
```

---

### Task 8: Server — Handle Upstream WebSocket Messages

**Files:**
- Modify: `internal/server/server.go`
- Modify: `internal/server/server_test.go`

**Step 1: Write the test**

Add to `internal/server/server_test.go`:

```go
func TestServer_WebSocketRefreshMessage(t *testing.T) {
	refreshCh := make(chan string, 8)
	h := hub.New()
	srv := server.New(h, ":0", server.WithRefreshChan(refreshCh))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond) // let handler start

	// Send refresh request
	conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"refresh","ticker":"AAPL_US_EQ"}`))

	select {
	case ticker := <-refreshCh:
		if ticker != "AAPL_US_EQ" {
			t.Errorf("got ticker %q, want AAPL_US_EQ", ticker)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for refresh request")
	}
}

func TestServer_WebSocketRefreshAll(t *testing.T) {
	refreshCh := make(chan string, 8)
	h := hub.New()
	srv := server.New(h, ":0", server.WithRefreshChan(refreshCh))
	ts := httptest.NewServer(srv.Handler())
	defer ts.Close()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
	conn, _, err := websocket.DefaultDialer.Dial(wsURL, nil)
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer conn.Close()

	time.Sleep(50 * time.Millisecond)

	conn.WriteMessage(websocket.TextMessage, []byte(`{"action":"refresh_all"}`))

	select {
	case ticker := <-refreshCh:
		if ticker != "" {
			t.Errorf("got ticker %q, want empty string for refresh_all", ticker)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("timeout waiting for refresh_all")
	}
}
```

**Step 2: Run test to verify it fails**

Run: `go test ./internal/server/ -run TestServer_WebSocketRefresh -v`
Expected: FAIL — `server.WithRefreshChan` undefined.

**Step 3: Implement upstream message handling**

Modify `internal/server/server.go`:

1. Add `refreshCh` field and option:
```go
type Server struct {
	hub         *hub.Hub
	addr        string
	activeConns atomic.Int32
	refreshCh   chan<- string
}

type Option func(*Server)

func WithRefreshChan(ch chan<- string) Option {
	return func(s *Server) { s.refreshCh = ch }
}

func New(h *hub.Hub, addr string, opts ...Option) *Server {
	s := &Server{hub: h, addr: addr}
	for _, o := range opts {
		o(s)
	}
	return s
}
```

2. Update `handleWS` — replace the drain goroutine with a reader that parses refresh messages:
```go
	// Read incoming messages (refresh requests).
	go func() {
		for {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				return
			}
			if s.refreshCh == nil {
				continue
			}
			var msg struct {
				Action string `json:"action"`
				Ticker string `json:"ticker"`
			}
			if json.Unmarshal(raw, &msg) != nil {
				continue
			}
			switch msg.Action {
			case "refresh":
				select {
				case s.refreshCh <- msg.Ticker:
				default:
				}
			case "refresh_all":
				select {
				case s.refreshCh <- "":
				default:
				}
			}
		}
	}()
```

3. Add `"encoding/json"` to imports.

**Step 4: Run test to verify it passes**

Run: `go test ./internal/server/ -run TestServer_WebSocketRefresh -v`
Expected: PASS

**Step 5: Fix existing server tests**

Existing calls to `server.New(h, addr)` need no change since `opts` is variadic. Run all server tests to confirm:

Run: `go test ./internal/server/ -v`
Expected: All PASS

**Step 6: Commit**

```
git add internal/server/server.go internal/server/server_test.go
git commit -m "feat: handle upstream WebSocket refresh messages"
```

---

### Task 9: TUI — Display Return Columns

**Files:**
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/tui_test.go`

**Step 1: Write the test**

Add to `internal/tui/tui_test.go`:

```go
func TestModel_ViewShowsReturnColumns(t *testing.T) {
	m := tui.NewModel()
	ri := &api.ReturnInfo{Return: 42.30, ReturnPct: 42.30, NetROIPct: 65.08}
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{
				Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3,
				AveragePrice: 173.20, CurrentPrice: 182.50,
				ProfitPerShare: 9.30, MarketValue: 547.50,
				Returns: ri,
			},
		},
	}
	b, _ := json.Marshal(payload)
	updated := m.ApplyMessage(b)
	view := updated.View()

	if !strings.Contains(view, "RETURN") {
		t.Error("view should contain RETURN header")
	}
	if !strings.Contains(view, "42.30") {
		t.Error("view should contain return value 42.30")
	}
	if !strings.Contains(view, "65.08") {
		t.Error("view should contain NetROI value 65.08")
	}
}

func TestModel_ViewShowsPlaceholderWhenNoReturns(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{
				Ticker: "AAPL_US_EQ", Currency: "USD", Quantity: 3,
				AveragePrice: 173.20, CurrentPrice: 182.50,
				ProfitPerShare: 9.30, MarketValue: 547.50,
			},
		},
	}
	b, _ := json.Marshal(payload)
	updated := m.ApplyMessage(b)
	view := updated.View()

	if !strings.Contains(view, "--") {
		t.Error("view should contain -- placeholder when Returns is nil")
	}
}
```

Add `"strings"` to the test file imports.

**Step 2: Run test to verify it fails**

Run: `go test ./internal/tui/ -run TestModel_ViewShows -v`
Expected: FAIL — no RETURN header in view.

**Step 3: Update View to show return columns**

In `internal/tui/tui.go`, update the `View()` method:

1. Update header row to add 3 new columns after TICKER:
```go
		out += fmt.Sprintf("%-20s %10s %10s %10s %10s %12s %13s %14s %14s\n",
			headerStyle.Render("TICKER"),
			headerStyle.Render("RETURN"),
			headerStyle.Render("RETURN %"),
			headerStyle.Render("NET ROI %"),
			headerStyle.Render("QTY"),
			headerStyle.Render("AVG PRICE"),
			headerStyle.Render("CURR PRICE"),
			headerStyle.Render("PROFIT/SHR"),
			headerStyle.Render("MKT VALUE"),
		)
```

2. Update the row rendering:
```go
		for _, p := range m.positions {
			sym := p.CurrencySymbol()
			retStr := fmt.Sprintf("%10s %10s %10s", "--", "--", "--")
			if p.Returns != nil {
				retStr = fmt.Sprintf("%10.2f %9.1f%% %9.1f%%",
					p.Returns.Return, p.Returns.ReturnPct, p.Returns.NetROIPct)
			}
			out += fmt.Sprintf("%-20s %s %10.4f %s%11.2f %s%12.2f %s %s%13.2f\n",
				p.Ticker,
				retStr,
				p.Quantity,
				sym, p.AveragePrice,
				sym, p.CurrentPrice,
				profitStyle.Render(fmt.Sprintf("%s%+12.2f", sym, p.ProfitPerShare)),
				sym, p.MarketValue,
			)
		}
```

3. Update subtitle:
```go
	out += dimStyle.Render("Positions with profit > 1/share  [r: refresh stock | R: refresh all | q: quit]") + "\n\n"
```

**Step 4: Run test to verify it passes**

Run: `go test ./internal/tui/ -run TestModel_ViewShows -v`
Expected: PASS

**Step 5: Commit**

```
git add internal/tui/tui.go internal/tui/tui_test.go
git commit -m "feat: display RETURN, RETURN %, NET ROI % columns in TUI"
```

---

### Task 10: TUI — Refresh Keybindings

**Files:**
- Modify: `internal/tui/tui.go`
- Modify: `internal/tui/tui_test.go`

The TUI needs to send refresh requests back to the server via WebSocket. This requires the TUI `Run` function to have a write channel to the WS connection.

**Step 1: Add conn field to Model and write support**

In `internal/tui/tui.go`:

1. Add a `conn` field to Model for sending messages:
```go
type Model struct {
	positions []api.Position
	updated   time.Time
	err       error
	conn      *websocket.Conn // nil in tests
	cursor    int             // selected row index
}
```

2. Update `Update` to handle `r` and `R`:
```go
	case tea.KeyMsg:
		switch v.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "r":
			if m.conn != nil && len(m.positions) > 0 && m.cursor < len(m.positions) {
				ticker := m.positions[m.cursor].Ticker
				m.conn.WriteMessage(websocket.TextMessage,
					[]byte(`{"action":"refresh","ticker":"`+ticker+`"}`))
			}
		case "R":
			if m.conn != nil {
				m.conn.WriteMessage(websocket.TextMessage,
					[]byte(`{"action":"refresh_all"}`))
			}
		case "j", "down":
			if m.cursor < len(m.positions)-1 {
				m.cursor++
			}
		case "k", "up":
			if m.cursor > 0 {
				m.cursor--
			}
		}
```

3. Highlight selected row in View (add `>` marker or different style for selected row).

4. Set `conn` in `Run`:
```go
	m := NewModel()
	m.conn = conn
```

**Step 2: Write test for cursor movement**

```go
func TestModel_CursorMovement(t *testing.T) {
	m := tui.NewModel()
	payload := tui.WSMessage{
		Timestamp: time.Now(),
		Positions: []api.Position{
			{Ticker: "AAPL_US_EQ", ProfitPerShare: 5},
			{Ticker: "MSFT_US_EQ", ProfitPerShare: 3},
		},
	}
	b, _ := json.Marshal(payload)
	m = m.ApplyMessage(b)

	// Move down
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	model := m2.(tui.Model)
	if model.Cursor() != 1 {
		t.Errorf("cursor should be 1 after j, got %d", model.Cursor())
	}

	// Move up
	m3, _ := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	model2 := m3.(tui.Model)
	if model2.Cursor() != 0 {
		t.Errorf("cursor should be 0 after k, got %d", model2.Cursor())
	}
}
```

Add `Cursor() int` accessor to Model.

**Step 3: Run tests**

Run: `go test ./internal/tui/ -v`
Expected: PASS

**Step 4: Commit**

```
git add internal/tui/tui.go internal/tui/tui_test.go
git commit -m "feat: add r/R refresh keybindings and cursor navigation to TUI"
```

---

### Task 11: Web UI — Display Return Columns and Refresh Buttons

**Files:**
- Modify: `internal/server/web/index.html`
- Modify: `internal/server/web/app.js`
- Modify: `internal/server/web/style.css`

**Step 1: Update HTML table headers**

In `internal/server/web/index.html`, update the `<thead>`:

```html
        <tr>
          <th>Ticker</th>
          <th></th>
          <th>Return</th>
          <th>Return %</th>
          <th>Net ROI %</th>
          <th>Quantity</th>
          <th>Avg Price</th>
          <th>Current Price</th>
          <th>Profit/Share</th>
          <th>Market Value</th>
        </tr>
```

Add a "Refresh All" button in the header:

```html
  <header>
    <h1>T212 Dashboard</h1>
    <button id="refresh-all" class="btn-refresh" title="Refresh all history">&#x21bb; Refresh History</button>
    <span id="status" class="status disconnected">Connecting…</span>
    <span id="updated"></span>
  </header>
```

**Step 2: Update app.js**

Store the WebSocket connection globally so we can send messages:

```javascript
  var ws = null;

  function sendRefresh(ticker) {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ action: 'refresh', ticker: ticker }));
    }
  }

  function sendRefreshAll() {
    if (ws && ws.readyState === WebSocket.OPEN) {
      ws.send(JSON.stringify({ action: 'refresh_all' }));
    }
  }

  document.getElementById('refresh-all').addEventListener('click', sendRefreshAll);
```

Update the row rendering to include return columns and a per-row refresh button:

```javascript
      positions.forEach(function (p) {
        const c = p.currency || 'GBP';
        const r = p.returns;
        const retVal = r ? fmt(r.return, 'GBP') : '--';
        const retPct = r ? r.returnPct.toFixed(1) + '%' : '--';
        const netRoi = r ? r.netRoiPct.toFixed(1) + '%' : '--';
        const tr = document.createElement('tr');
        tr.innerHTML =
          '<td>' + p.ticker + '</td>' +
          '<td><button class="btn-refresh-row" title="Refresh ' + p.ticker + '">&#x21bb;</button></td>' +
          '<td>' + retVal + '</td>' +
          '<td>' + retPct + '</td>' +
          '<td>' + netRoi + '</td>' +
          '<td>' + p.quantity + '</td>' +
          '<td>' + fmt(p.averagePrice, c) + '</td>' +
          '<td>' + fmt(p.currentPrice, c) + '</td>' +
          '<td class="profit">+' + fmt(p.profitPerShare, c) + '</td>' +
          '<td>' + fmt(p.marketValue, c) + '</td>';
        tr.querySelector('.btn-refresh-row').addEventListener('click', function () {
          sendRefresh(p.ticker);
        });
        tbodyEl.appendChild(tr);
      });
```

Update `connect()` to use the global `ws` variable:

```javascript
  function connect() {
    ws = new WebSocket('ws://' + location.host + '/ws');
    ws.onopen = function () { ... };
    ws.onmessage = function (e) { ... };
    ws.onclose = function () { ws = null; ... };
  }
```

**Step 3: Update style.css**

Add styles for refresh buttons:

```css
.btn-refresh { background: #1e293b; color: #94a3b8; border: 1px solid #334155; border-radius: 4px; padding: 0.3rem 0.8rem; cursor: pointer; font-size: 0.75rem; }
.btn-refresh:hover { background: #334155; color: #e2e8f0; }
.btn-refresh-row { background: none; border: none; color: #475569; cursor: pointer; font-size: 0.9rem; padding: 0; }
.btn-refresh-row:hover { color: #e2e8f0; }
```

**Step 4: Verify build compiles (embedded files)**

Run: `go build ./...`
Expected: Success

**Step 5: Commit**

```
git add internal/server/web/index.html internal/server/web/app.js internal/server/web/style.css
git commit -m "feat: add return columns and refresh buttons to web UI"
```

---

### Task 12: Wire Everything in serve.go

**Files:**
- Modify: `cmd/t212/serve.go`

**Step 1: Update serve.go to create and wire history components**

```go
import (
	// ... existing imports ...
	"github.com/ko5tas/t212/internal/history"
)

// In runServe():

	hs := history.NewStore()
	refreshCh := make(chan string, 8)

	p := poller.New(apiClient, s, h, threshold, n,
		poller.WithHistoryStore(hs),
		poller.WithRefreshChan(refreshCh),
	)
	srv := server.New(h, ":"+port, server.WithRefreshChan(refreshCh))

	// Startup history fetch (blocking, before poll loop)
	slog.Info("fetching initial history data...")
	go func() {
		refreshCh <- "" // trigger full refresh on first poll
	}()
```

Wait — the initial history load should happen inside the poller's Run before the first poll. Better approach: add a `LoadHistory` method to poller that is called at startup:

Actually, the simplest: the poller's `Run` already does an immediate poll. We add the initial history fetch right before that first poll in `Run`:

```go
func (p *Poller) Run(ctx context.Context) {
	if p.historyStore != nil {
		p.refreshHistory(ctx, "")
	}
	p.poll(ctx)
	// ... rest of loop
}
```

This was already part of Task 7. So serve.go just needs:

```go
	hs := history.NewStore()
	refreshCh := make(chan string, 8)

	p := poller.New(apiClient, s, h, threshold, n,
		poller.WithHistoryStore(hs),
		poller.WithRefreshChan(refreshCh),
	)
	srv := server.New(h, ":"+port, server.WithRefreshChan(refreshCh))
```

**Step 2: Run build and all tests**

Run: `go build ./... && go test ./...`
Expected: All pass

**Step 3: Commit**

```
git add cmd/t212/serve.go
git commit -m "feat: wire history store and refresh channel in serve command"
```

---

### Task 13: Final Integration Test and Cleanup

**Step 1: Run the full test suite**

Run: `go test ./... -v`
Expected: All PASS

**Step 2: Run go vet and build**

Run: `go vet ./... && go build ./...`
Expected: Clean

**Step 3: Commit any remaining cleanup**

**Step 4: Tag release**

```
git tag 1.0.10
git push && git push origin 1.0.10
```
