# Shared Rate Limiter Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Prevent history pagination from exhausting the T212 API rate limit, which causes position polls to get 429'd.

**Architecture:** A centralized `RateLimiter` in the `api` package tracks rate limit state from response headers. All API calls go through it. High-priority callers (position polls) proceed when `remaining <= 2`; low-priority callers (history pagination) wait until reset.

**Tech Stack:** Go stdlib only (`sync`, `time`, `log/slog`, `net/http`)

---

### Task 1: Create RateLimiter with Wait and Update

**Files:**
- Create: `internal/api/ratelimiter.go`
- Create: `internal/api/ratelimiter_test.go`

**Step 1: Write the failing tests**

```go
// internal/api/ratelimiter_test.go
package api

import (
	"context"
	"net/http"
	"strconv"
	"testing"
	"time"
)

func TestRateLimiter_HighPriority_ProceedsWhenLow(t *testing.T) {
	rl := NewRateLimiter()
	// Simulate remaining=1, reset in 5s
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("x-ratelimit-remaining", "1")
	resp.Header.Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(5*time.Second).Unix(), 10))
	rl.Update(resp)

	start := time.Now()
	err := rl.Wait(context.Background(), PriorityHigh)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("high priority should not wait")
	}
}

func TestRateLimiter_LowPriority_WaitsWhenLow(t *testing.T) {
	rl := NewRateLimiter()
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("x-ratelimit-remaining", "1")
	resp.Header.Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(200*time.Millisecond).Unix(), 10))
	rl.Update(resp)

	start := time.Now()
	err := rl.Wait(context.Background(), PriorityLow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	// Should have waited ~200ms (until reset)
	if time.Since(start) < 100*time.Millisecond {
		t.Error("low priority should wait when remaining <= 2")
	}
}

func TestRateLimiter_LowPriority_ProceedsWhenPlenty(t *testing.T) {
	rl := NewRateLimiter()
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("x-ratelimit-remaining", "5")
	resp.Header.Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(time.Minute).Unix(), 10))
	rl.Update(resp)

	start := time.Now()
	err := rl.Wait(context.Background(), PriorityLow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("low priority should not wait when remaining > 2")
	}
}

func TestRateLimiter_Wait_RespectsContextCancel(t *testing.T) {
	rl := NewRateLimiter()
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("x-ratelimit-remaining", "1")
	resp.Header.Set("x-ratelimit-reset", strconv.FormatInt(time.Now().Add(10*time.Second).Unix(), 10))
	rl.Update(resp)

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err := rl.Wait(ctx, PriorityLow)
	if err != context.Canceled {
		t.Errorf("expected context.Canceled, got %v", err)
	}
}

func TestRateLimiter_NoHeaders_ProceedsImmediately(t *testing.T) {
	rl := NewRateLimiter()
	// No Update called — zero state
	start := time.Now()
	err := rl.Wait(context.Background(), PriorityLow)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if time.Since(start) > 100*time.Millisecond {
		t.Error("should proceed immediately with no rate limit data")
	}
}
```

**Step 2: Run tests to verify they fail**

Run: `go test ./internal/api/ -run TestRateLimiter -v`
Expected: FAIL — `NewRateLimiter`, `PriorityHigh`, `PriorityLow` undefined

**Step 3: Write the implementation**

```go
// internal/api/ratelimiter.go
package api

import (
	"context"
	"log/slog"
	"net/http"
	"strconv"
	"sync"
	"time"
)

// Priority determines how a caller behaves when rate limit is low.
type Priority int

const (
	PriorityHigh Priority = iota // position polls — proceed even when low
	PriorityLow                  // history pagination — wait when low

	rateLimitReserve = 2          // slots reserved for high-priority callers
	maxRateLimitWait = 15 * time.Second // safety cap on wait duration
)

// RateLimiter coordinates API calls to stay within T212 rate limits.
// High-priority callers proceed when remaining <= reserve; low-priority wait.
type RateLimiter struct {
	mu        sync.Mutex
	remaining int
	resetAt   time.Time
	hasData   bool // true after first Update
}

// NewRateLimiter creates a RateLimiter.
func NewRateLimiter() *RateLimiter {
	return &RateLimiter{}
}

// Update records rate limit state from API response headers.
func (r *RateLimiter) Update(resp *http.Response) {
	remaining, err1 := strconv.Atoi(resp.Header.Get("x-ratelimit-remaining"))
	resetUnix, err2 := strconv.ParseInt(resp.Header.Get("x-ratelimit-reset"), 10, 64)
	if err1 != nil || err2 != nil {
		return // no valid headers — don't update state
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.remaining = remaining
	r.resetAt = time.Unix(resetUnix, 0)
	r.hasData = true

	slog.Debug("rate limit updated", "remaining", remaining, "reset", r.resetAt)
}

// Wait blocks until a request is safe to make at the given priority.
func (r *RateLimiter) Wait(ctx context.Context, pri Priority) error {
	r.mu.Lock()
	if !r.hasData || r.remaining > rateLimitReserve {
		r.mu.Unlock()
		return nil
	}
	if pri == PriorityHigh {
		r.mu.Unlock()
		slog.Debug("rate limit low but high priority, proceeding", "remaining", r.remaining)
		return nil
	}
	// Low priority: wait until reset
	delay := time.Until(r.resetAt)
	r.mu.Unlock()

	if delay <= 0 {
		return nil
	}
	if delay > maxRateLimitWait {
		delay = maxRateLimitWait
	}
	slog.Debug("rate limit low, waiting", "delay", delay, "remaining", r.remaining)
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(delay):
		return nil
	}
}
```

**Step 4: Run tests to verify they pass**

Run: `go test ./internal/api/ -run TestRateLimiter -v`
Expected: PASS (all 5 tests)

**Step 5: Commit**

```bash
git add internal/api/ratelimiter.go internal/api/ratelimiter_test.go
git commit -m "feat: add shared rate limiter with priority support"
```

---

### Task 2: Wire RateLimiter into Client

**Files:**
- Modify: `internal/api/client.go` (struct, constructor, FetchPositions, fetchHistoryPage, FetchOrderHistory, FetchDividendHistory)
- Modify: `internal/api/ratelimit.go` (remove standalone `parseRateLimit` — logic now in `RateLimiter.Update`)

**Step 1: Add `rl` field to Client and initialise in constructor**

In `internal/api/client.go`, add field to struct:

```go
type Client struct {
	httpClient  *http.Client
	baseURL     string
	authHeader  string
	instruments map[string]InstrumentMeta
	exchanges   map[int]string
	rl          *RateLimiter
}
```

In `NewClient`, add initialisation before the return:

```go
return &Client{
	httpClient: httpClient,
	baseURL:    baseURL,
	authHeader: "Basic " + auth,
	rl:         NewRateLimiter(),
}
```

**Step 2: Update FetchPositions to use RateLimiter**

Add `c.rl.Wait(ctx, PriorityHigh)` before the HTTP call, and `c.rl.Update(resp)` after getting the response (before status check). Remove the standalone `parseRateLimit(resp)` call at the end — use `c.rl` state instead.

Replace the `FetchPositions` rate limit section (around lines 58-132):

Before the HTTP request (after building `req`):
```go
if err := c.rl.Wait(ctx, PriorityHigh); err != nil {
	return nil, RateLimitInfo{}, err
}
```

After `resp.Body.Close()` defer, before status check:
```go
c.rl.Update(resp)
```

At the return, replace `rl := parseRateLimit(resp)` with reading from the limiter. Since `FetchPositions` still returns `RateLimitInfo` for the poller's debug logging, keep `parseRateLimit` but just for the return value:
```go
rl := parseRateLimit(resp)
return positions, rl, nil
```

**Step 3: Update fetchHistoryPage to use RateLimiter**

Add `c.rl.Wait(ctx, PriorityLow)` before the HTTP request. Replace the existing `rl := parseRateLimit(resp)` + debug log with `c.rl.Update(resp)` and keep the debug log using the response directly.

```go
func fetchHistoryPage[T any](c *Client, ctx context.Context, path string) ([]T, *string, RateLimitInfo, error) {
	if err := c.rl.Wait(ctx, PriorityLow); err != nil {
		return nil, nil, RateLimitInfo{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	// ... (unchanged until after resp.Body.Close() defer)

	c.rl.Update(resp)
	rl := parseRateLimit(resp)
	slog.Debug("history page", "path", path, "status", resp.StatusCode, "ratelimit_remaining", rl.Remaining, "ratelimit_reset", rl.Reset)
	// ... rest unchanged
```

**Step 4: Remove old `rateLimitWait` function and `historyRateDelay` constant**

Delete the `rateLimitWait` function (lines 336-360 of client.go) and the `historyRateDelay` constant (line 23).

Update `FetchOrderHistory` and `FetchDividendHistory` — remove the `rateLimitWait(ctx, rl)` calls in the pagination loops. The rate limiting is now handled by `fetchHistoryPage` via `c.rl.Wait`.

In `FetchOrderHistory`, the loop becomes:
```go
for {
	page, nextPath, _, err := fetchHistoryPage[HistoricalOrder](c, ctx, path)
	if err != nil {
		return nil, fmt.Errorf("fetch order history: %w", err)
	}
	all = append(all, page...)
	if nextPath == nil {
		break
	}
	path = *nextPath
}
```

Same pattern for `FetchDividendHistory`.

**Step 5: Run all tests**

Run: `go test ./internal/api/ -v`
Expected: PASS — existing tests still pass (mock servers return rate limit headers with high remaining, so limiter always proceeds)

Run: `go test ./... -v`
Expected: PASS — all project tests pass

**Step 6: Commit**

```bash
git add internal/api/client.go internal/api/ratelimit.go
git commit -m "refactor: wire shared rate limiter into API client"
```

---

### Task 3: Clean up unused code

**Files:**
- Modify: `internal/api/client.go` — remove `historyRateDelay` const if not already removed

**Step 1: Verify `historyRateDelay` and `rateLimitWait` are gone**

Search for any remaining references:
```bash
grep -r "rateLimitWait\|historyRateDelay" internal/
```
Expected: no matches

**Step 2: Run full test suite**

Run: `go vet ./... && go test ./...`
Expected: clean vet, all tests pass

**Step 3: Commit (if any cleanup needed)**

```bash
git add -A && git commit -m "chore: remove unused rate limit helpers"
```

---

### Task 4: Build, tag, release, verify

**Step 1: Final build and test**

Run: `go build ./... && go test ./...`
Expected: clean build, all tests pass

**Step 2: Tag and push**

```bash
git tag 1.6.7
git push origin main --tags
```

**Step 3: Create GitHub release**

```bash
gh release create 1.6.7 --title "1.6.7" --notes "Shared rate limiter: position polls no longer get 429'd during history pagination."
```

**Step 4: Deploy and verify on Pi**

After release workflow completes:
```bash
sudo apt update && sudo apt upgrade -y
sudo journalctl -u t212 -f
```

Expected in logs:
- `rate limit updated` debug messages during history pagination
- `rate limit low, waiting` when history yields for polls
- No more `fetch positions failed: unexpected status 429` errors
- `history fetched` INFO message appearing after pagination completes
