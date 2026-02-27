# T212 Portfolio Dashboard — Design Document

**Date:** 2026-02-27
**Status:** Approved

---

## Overview

A Go service that polls the Trading 212 live API every second, filters positions where profit-per-share exceeds £1, and presents results via a browser-based Web UI and a terminal TUI. Sends Signal notifications when positions cross the threshold. Runs as a systemd service on a Raspberry Pi 5 running DietPi.

---

## Requirements

| Concern | Decision |
|---|---|
| Language | Go |
| Filter | `currentPrice − averagePrice > £1.00` per share |
| UI | Web UI (WebSocket-powered) + Terminal TUI (bubbletea) — simultaneous |
| Hosting | Raspberry Pi 5, DietPi, systemd service |
| T212 environment | Live account (`https://live.trading212.com/api/v0`) |
| Polling rate | 1 req/s (T212 API maximum for `/equity/positions`) |
| Notifications | Signal via signal-cli (linked device — Pi registered on user's own Signal account) |
| Transport security | TLS 1.3 for all outbound connections |
| Local transport | HTTP on LAN (HTTPS/Let's Encrypt deferred to future iteration) |

---

## Architecture

### Binary structure

Single `linux/arm64` binary with two subcommands:

- `t212 serve` — runs the poller, web server, WebSocket hub, and Signal notifier (managed by systemd)
- `t212 tui` — connects to the running `serve` instance via WebSocket, renders bubbletea TUI (run manually over SSH)

### Component diagram

```
T212 API (1 req/s)
    │
    ▼
poller goroutine
    │  writes
    ▼
store (sync.RWMutex, in-memory only)
    │  reads + applies filter
    ▼
hub.Broadcast()
    ├──► WebSocket client(s) (browser Web UI)
    └──► WebSocket client(s) (t212 tui over SSH)
         │
         also triggers
         ▼
notifier: Signal alert on threshold edge (enter/exit)
```

### Project layout

```
t212/
├── cmd/t212/main.go
├── internal/
│   ├── api/          client.go, client_test.go
│   ├── poller/       poller.go, poller_test.go
│   ├── store/        store.go, store_test.go
│   ├── filter/       filter.go, filter_test.go
│   ├── hub/          hub.go, hub_test.go
│   ├── server/       server.go, server_test.go
│   ├── tui/          tui.go, tui_test.go
│   └── notifier/     notifier.go, notifier_test.go
├── web/
│   ├── index.html
│   ├── style.css
│   └── app.js
├── deploy/
│   ├── t212.service
│   └── config.env.example
├── .github/workflows/
│   ├── ci.yml
│   └── signal-cli-update.yml
├── Makefile
└── go.mod
```

---

## Data Model

```go
// Position represents a single open position from the T212 API.
type Position struct {
    Ticker         string  `json:"ticker"`
    Quantity       float64 `json:"quantity"`
    AveragePrice   float64 `json:"averagePrice"`   // avg buy price in account currency
    CurrentPrice   float64 `json:"currentPrice"`
    ProfitPerShare float64 `json:"profitPerShare"` // computed: currentPrice − averagePrice
    MarketValue    float64 `json:"marketValue"`    // computed: quantity × currentPrice
}

// BroadcastMessage is the WebSocket payload sent to all subscribers on each poll.
type BroadcastMessage struct {
    Timestamp time.Time  `json:"timestamp"`
    Positions []Position `json:"positions"` // only positions passing the filter
}
```

**Filter rule:** `position.CurrentPrice − position.AveragePrice > 1.00`

---

## API Integration

### Endpoint

`GET https://live.trading212.com/api/v0/equity/positions`
Rate limit: **1 request per second**

### Authentication

API key passed as HTTP header:
```
Authorization: <api_key>
```
Key loaded from environment variable `T212_API_KEY` at startup. Never logged, never written to disk after initial load.

### Rate limit handling

- Read `x-ratelimit-remaining` on every response
- If `0`: sleep until `x-ratelimit-reset` Unix timestamp
- Network/5xx errors: exponential backoff (1s → 2s → 4s → … → 30s max), resets on success

---

## Security

### API key

- Stored in `/etc/t212/config.env`, permissions `0600`, owner `root:root`
- Loaded via systemd `EnvironmentFile=` — never passed as CLI argument
- Never logged or included in any serialised struct
- Zeroed from memory on shutdown

### Transport

- All outbound connections (T212 API, Signal servers via signal-cli): TLS 1.3 minimum (`tls.VersionTLS13`)
- `InsecureSkipVerify` never set
- Local web server: HTTP on LAN (HTTPS deferred — see future work)

### Web server hardening

- Secure response headers on every request:
  - `Content-Security-Policy: default-src 'self'`
  - `X-Frame-Options: DENY`
  - `X-Content-Type-Options: nosniff`
  - `Referrer-Policy: no-referrer`
- WebSocket `/ws` endpoint: origin validation, max 5 concurrent connections
- No financial data persisted to disk at any point

### Audit logging

Structured logging via Go `slog` to systemd journal:
- Fields: timestamp, endpoint, HTTP status, latency
- API key and response bodies never logged

### systemd hardening

```ini
NoNewPrivileges=yes
ProtectSystem=strict
PrivateTmp=yes
```

---

## Signal Notifications

### Setup (one-time)

The Pi is registered as a **linked device** on the user's existing Signal account:

```bash
make setup-signal   # runs signal-cli addDevice, prints QR code
# scan QR with phone: Signal → Settings → Linked Devices
```

### Config

```
SIGNAL_NUMBER=+447700000000   # sender = recipient = user's own number
```

### Notification logic

Edge-triggered (not every poll):
- Position **enters** threshold: `📈 AAPL crossed +£1/share profit (now +£9.30)`
- Position **exits** threshold: `📉 TSLA dropped below +£1/share profit (now +£0.42)`

### signal-cli updates

- No official Debian apt package
- `make update-signal-cli`: queries GitHub releases API, downloads tarball, verifies SHA256, replaces binary atomically
- Weekly GitHub Actions workflow (`signal-cli-update.yml`) checks for new releases and opens a PR with version bump

---

## Testing Strategy

### Unit tests

All packages have `*_test.go` files with table-driven tests:

| Package | Key test cases |
|---|---|
| `filter/` | exactly £1 (boundary), just above, just below, zero profit, negative profit, float rounding |
| `api/` | auth header format, rate limit backoff, 429 handling, network error retry, response parsing |
| `store/` | concurrent reads/writes (`go test -race`), empty store, update idempotency |
| `notifier/` | mock exec.Command, correct signal-cli args, edge-trigger (enter/exit), no duplicate alerts |
| `hub/` | fan-out to multiple subscribers, subscriber disconnect mid-broadcast |
| `poller/` | backoff on error, respects rate limit headers, graceful shutdown via context |

### Integration tests

Tagged `//go:build integration`, run with `go test -tags integration`:
- Full pipeline: mock T212 server → poller → store → hub → WebSocket client
- Uses T212 **demo** API key from CI secret (never live account in CI)

### CI pipeline (GitHub Actions)

Every push and PR:

```yaml
jobs:
  test:     go test -race -coverprofile=coverage.out ./...
  lint:     golangci-lint run (errcheck, gosec, staticcheck, govet)
  build:    GOARCH=arm64 GOOS=linux go build ./cmd/t212
  security: govulncheck ./...
```

---

## Deployment

### Makefile targets

```
make build              # cross-compile linux/arm64 binary
make test               # go test -race -cover ./...
make deploy             # scp binary + service to Pi, reload systemd
make setup-signal       # run signal-cli addDevice on Pi, print QR
make update-signal-cli  # fetch + SHA256-verify latest signal-cli release
make logs               # ssh pi 'journalctl -u t212 -f'
```

### systemd unit (`deploy/t212.service`)

```ini
[Unit]
Description=T212 Portfolio Dashboard
After=network-online.target
Wants=network-online.target

[Service]
EnvironmentFile=/etc/t212/config.env
ExecStart=/usr/local/bin/t212 serve
Restart=always
RestartSec=5
NoNewPrivileges=yes
ProtectSystem=strict
PrivateTmp=yes
User=t212
Group=t212

[Install]
WantedBy=multi-user.target
```

### Config file (`/etc/t212/config.env`)

```
# chmod 0600, chown root:root
T212_API_KEY=<your_live_api_key>
SIGNAL_NUMBER=+447700000000
T212_PORT=8080
T212_FILTER_THRESHOLD=1.00
```

---

## Future Work

- HTTPS via Let's Encrypt (DNS-01 challenge, certbot on DietPi)
- Configurable threshold via web UI
- Historical profit chart (in-memory ring buffer, no DB needed)
- Mobile-responsive web UI improvements
