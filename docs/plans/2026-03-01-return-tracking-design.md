# Return Tracking Design

## Problem

The dashboard shows unrealised profit per share (currentPrice − averagePrice) but not
the actual realised return from sells and dividends. A user who bought £100 of GOOGL,
made fractional sells, and received dividends cannot see their total return at a glance.

## Solution

Fetch order history and dividend history from the T212 API, compute per-ticker
realised returns, and display three new columns in the TUI and web UI.

## Data Model

```go
type ReturnInfo struct {
    TotalBought    float64 // sum of all BUY fill netValues (GBP)
    TotalSold      float64 // sum of all SELL fill netValues (GBP)
    TotalDividends float64 // sum of all dividend amounts (GBP)
    Return         float64 // TotalSold + TotalDividends
    ReturnPct      float64 // Return / TotalBought * 100
    NetROIPct      float64 // (Return + unrealised) / (TotalBought - TotalSold) * 100
}
```

`Position` gains a `Returns *ReturnInfo` field (nil until history loads).

The T212 `FillWalletImpact.NetValue` is already in account currency (GBP) after
FX conversion, so no manual currency conversion is needed for returns.

## API Endpoints

| Endpoint | Rate Limit | Pagination |
|---|---|---|
| `GET /api/v0/equity/history/orders` | 6 req/min | cursor + limit (max 50) |
| `GET /api/v0/equity/history/dividends` | 6 req/min | cursor + limit (max 50) |

Both support a `ticker` query parameter for per-stock filtering.

### Wire Format (Orders)

Each item contains:
- `fill.walletImpact.netValue` — fill value in account currency (GBP)
- `order.side` — `"BUY"` or `"SELL"`
- `order.ticker` — e.g. `"GOOGL_US_EQ"`
- `order.status` — only `"FILLED"` orders count

### Wire Format (Dividends)

Each item contains:
- `amount` — dividend payment in account currency
- `ticker` — e.g. `"GOOGL_US_EQ"`

## API Client

Two new methods on `*Client`:

- `FetchOrderHistory(ctx, ticker) ([]HistoricalOrder, error)` — paginated fetch
  of all order fills. Pass `""` for ticker to fetch all stocks.
- `FetchDividendHistory(ctx, ticker) ([]DividendItem, error)` — paginated fetch
  of all dividends. Pass `""` for ticker to fetch all.

Rate limiting: track last request time per endpoint group, sleep if < 10s since
last call (ensures we stay under 6 req/min).

## History Store

New `internal/history/` package:

- Thread-safe `map[string]ReturnInfo` keyed by ticker
- `SetAll(map[string]ReturnInfo)` — full refresh
- `Set(ticker, ReturnInfo)` — per-stock refresh
- `Get(ticker) *ReturnInfo` — returns nil if not loaded

`ComputeReturns(orders, dividends, unrealisedPPS, qty) ReturnInfo` — pure function
that aggregates fills and dividends into a ReturnInfo.

## Poller Integration

1. **Startup:** fetch all order + dividend history (paginated, ~1-3 min). Compute
   ReturnInfo per ticker and populate history store. Then start normal poll loop.
2. **Every hour:** re-fetch all history in background, update history store.
3. **Before broadcast:** attach `Returns` from history store to each Position.
4. **Per-stock refresh:** on request, fetch just that ticker's history and update.

## Refresh Requests

WebSocket protocol gains client→server messages:

```json
{"action": "refresh", "ticker": "GOOGL_US_EQ"}
{"action": "refresh_all"}
```

Server reads incoming WS messages and forwards to poller via a channel.
Poller triggers the appropriate fetch, updates history store, and the next
broadcast includes updated return data.

## UI: Three New Columns

After the TICKER column:

| Column | Source | Example |
|---|---|---|
| RETURN | `ReturnInfo.Return` | `£42.30` |
| RETURN % | `ReturnInfo.ReturnPct` | `42.3%` |
| NET ROI % | `ReturnInfo.NetROIPct` | `58.1%` |

While history is loading, show `--` placeholder.

### TUI

- `r` key: refresh selected stock's history
- `R` key: refresh all history
- Status line: "Loading history..." during fetch

### Web UI

- Per-row refresh button (small icon)
- "Refresh All History" button in header
- `--` placeholder while loading

## Rate Limit Budget

| Operation | Orders reqs | Dividends reqs | Total |
|---|---|---|---|
| Full refresh (200 orders, 20 divs) | ~4 pages | ~1 page | ~5 reqs |
| Per-stock refresh | 1 page | 1 page | 2 reqs |
| Hourly refresh | ~5 reqs | ~1 req | ~6 reqs |

With 10s spacing between requests, a full refresh takes ~50s. Comfortable within
the 6 req/min per-endpoint limit.
