# Shared Rate Limiter Design

**Date:** 2026-03-07
**Problem:** History pagination consumes all T212 API rate limit (6 req/min), causing position polls to get 429'd every hour for ~3 minutes.

## Solution

Add a centralized `RateLimiter` to `api.Client` that coordinates all API calls. High-priority callers (position polls) preempt low-priority callers (history pagination) when rate budget is low.

## Components

### `internal/api/ratelimit.go` (new file)

- `Priority` type: `PriorityHigh` (polls), `PriorityLow` (history)
- `RateLimiter` struct: mutex, `remaining` int, `resetAt` time.Time
- `Update(resp *http.Response)` — updates state from `x-ratelimit-remaining` and `x-ratelimit-reset` headers
- `Wait(ctx context.Context, pri Priority) error` — blocks until a slot is available:
  - `remaining > 2`: all priorities proceed immediately
  - `remaining <= 2`: low-priority waits until reset; high-priority proceeds
  - Respects context cancellation
  - Caps max wait at 15s (safety valve)

### Changes to `api.Client`

- Add `rl *RateLimiter` field, initialized in constructor
- `fetchHistoryPage`: call `rl.Wait(ctx, PriorityLow)` before request, `rl.Update(resp)` after
- `FetchPositions`: call `rl.Wait(ctx, PriorityHigh)` before request, `rl.Update(resp)` after
- Remove `rateLimitWait()` and standalone `parseRateLimit()` — absorbed into RateLimiter

### Unchanged

- Poller structure, goroutine layout, intervals
- History pagination loop logic
- Existing tests (mock servers return no rate limit headers → limiter defaults to "proceed immediately")

## Verification

1. `go test ./...` — all existing tests pass
2. New unit tests for `RateLimiter` (wait behavior at various remaining levels, priority preemption)
3. Deploy and verify via `journalctl -u t212 -f`:
   - No more 429 errors on position polls
   - History refresh completes (look for "history fetched" log)
   - Position polls continue during history refresh
