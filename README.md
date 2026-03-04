# t212

A self-hosted dashboard for Trading 212 that polls your live portfolio every second, filters positions where profit-per-share exceeds £1, and surfaces them via a browser Web UI, a terminal TUI, and Signal notifications.

Designed to run as a systemd service on a Raspberry Pi 5 (DietPi, `linux/arm64`).

---

## Features

- **Live position feed** — polls `GET /api/v0/equity/positions` at the T212 rate limit (1 req/s)
- **Profit filter** — shows only positions where `currentPrice − averagePrice > £1.00` per share
- **Browser Web UI** — auto-updating table pushed over WebSocket; no page refresh needed
- **Terminal TUI** — bubbletea-based table, run over SSH from any machine on the LAN
- **Signal notifications** — edge-triggered alerts when a position enters or exits the threshold
- **Zero persistence** — no database; all state is in-memory and lost on restart

---

## Architecture

```
T212 API (1 req/s, HTTPS/TLS 1.3)
    │
    ▼
poller goroutine
    │  writes all positions
    ▼
store (sync.RWMutex, in-memory)
    │  reads + applies £1 filter
    ▼
hub.Broadcast()
    ├──► WebSocket → browser (Web UI)
    └──► WebSocket → t212 tui (terminal TUI over SSH)

also on each poll:
sendNotifications → signal-cli subprocess → Signal message
```

### Single binary, two subcommands

| Subcommand | Purpose | Managed by |
|---|---|---|
| `t212 serve` | Poller + web server + hub + notifier | systemd |
| `t212 tui` | Terminal subscriber (reads from running serve) | Manual / SSH |

---

## Prerequisites

- Go 1.25+ (for building from source)
- A Trading 212 live account with an API key
- Raspberry Pi 5 running DietPi (or any `linux/arm64` system)
- `make`, `ssh`, `scp` on your build machine
- (Optional) `signal-cli` installed on the Pi for notifications

---

## Installation (DietPi / Raspberry Pi 5)

### Option A: APT repository (recommended)

Add the t212 APT repository for automatic updates:

```bash
curl -fsSL https://ko5tas.github.io/t212/apt/setup.sh | sudo bash
sudo apt install t212
```

Future updates arrive via `sudo apt update && sudo apt upgrade`.

Or from your build machine:

```bash
make setup-apt PI_HOST=pi@raspberrypi.local
```

### Option B: Manual download

Download the latest `.deb` from the [Releases page](https://github.com/ko5tas/t212/releases/latest):

```bash
wget https://github.com/ko5tas/t212/releases/latest/download/t212_<version>_arm64.deb
sudo dpkg -i t212_<version>_arm64.deb
```

### Post-install setup

The installer prints the exact steps to configure and start the service. In summary:

```bash
# 1. Set your API key (and optionally SIGNAL_NUMBER, T212_PORT)
sudo nano /etc/t212/config.env

# 2. Start the service
sudo systemctl start t212

# 3. Verify
sudo systemctl status t212
sudo journalctl -u t212 -f
```

Open `http://<raspberry-pi-ip>:8080` in a browser on the same LAN.

**Upgrading (APT):** `sudo apt update && sudo apt upgrade` — automatic.

**Upgrading (manual):** re-download and `sudo dpkg -i t212_<new-version>_arm64.deb`. Your `/etc/t212/config.env` is preserved automatically.

**Removing:** `sudo dpkg -r t212` (config survives). `sudo dpkg --purge t212` (config deleted).

---

## Quick start (local development)

```bash
git clone https://github.com/ko5tas/t212
cd t212

# Run tests
make test

# Build for current platform
make build

# Run the server locally
T212_API_KEY=<your_key> ./t212 serve

# In another terminal, run the TUI
./t212 tui
```

Open `http://localhost:8080` in your browser to see the Web UI.

---

## Configuration

All configuration is via environment variables (or `/etc/t212/config.env` in production).

| Variable | Required | Default | Description |
|---|---|---|---|
| `T212_API_KEY` | Yes | — | Trading 212 API key ID (shown when generating the key) |
| `T212_API_SECRET` | Yes | — | Trading 212 API secret key (shown once at generation time) |
| `T212_PORT` | No | `8080` | Port for the web server |
| `SIGNAL_NUMBER` | No | — | Your phone number in E.164 format (`+447700…`). Omit to disable Signal notifications. |
| `SIGNAL_CLI_PATH` | No | `/usr/local/bin/signal-cli` | Path to the `signal-cli` binary |
| `T212_HOST` | No | `localhost` | Host for `t212 tui` to connect to (TUI subcommand only) |

`T212_API_KEY` and `T212_API_SECRET` are combined as HTTP Basic auth, loaded once at startup, never logged, and never written to disk.

---

## Deployment to Raspberry Pi

### 1. Create the service user and config directory

```bash
ssh pi@raspberrypi.local
sudo useradd -r -s /usr/sbin/nologin t212
sudo mkdir -p /etc/t212
sudo chmod 700 /etc/t212
```

### 2. Create the config file

```bash
sudo cp deploy/config.env.example /etc/t212/config.env
sudo chmod 0600 /etc/t212/config.env
sudo chown root:root /etc/t212/config.env
sudo nano /etc/t212/config.env   # fill in T212_API_KEY and SIGNAL_NUMBER
```

### 3. Deploy

```bash
# From your build machine:
make deploy PI_HOST=pi@raspberrypi.local
```

This cross-compiles for `linux/arm64`, copies the binary and systemd unit to the Pi, and restarts the service.

### 4. Check it's running

```bash
make logs PI_HOST=pi@raspberrypi.local
```

Open `http://raspberrypi.local:8080` in a browser on the same LAN.

---

## Signal notifications (optional)

Signal alerts are sent as a **linked device** on your own Signal account — the Pi sends messages to you from itself.

### One-time setup

Install `signal-cli` on the Pi, then:

```bash
# On the Pi, print a QR code:
make setup-signal PI_HOST=pi@raspberrypi.local
# Scan the QR with your phone: Signal → Settings → Linked Devices → Link New Device
```

Set `SIGNAL_NUMBER` in `/etc/t212/config.env` to your number in E.164 format.

### Alert messages

| Event | Message |
|---|---|
| Position enters threshold | `📈 AAPL_US_EQ crossed +£1/share profit (now +£9.30)` |
| Position exits threshold | `📉 AAPL_US_EQ dropped below +£1/share profit` |

Alerts are **edge-triggered**: you receive one message when a position crosses the boundary, not one every second while it stays there.

### Updating signal-cli

```bash
make update-signal-cli PI_HOST=pi@raspberrypi.local
```

Downloads the latest release from GitHub, verifies the SHA256 checksum, and replaces the binary atomically.

---

## Makefile targets

| Target | Description |
|---|---|
| `make build` | Compile for current platform |
| `make build-arm` | Cross-compile for Raspberry Pi 5 (`linux/arm64`) |
| `make deb` | Build `.deb` package for Raspberry Pi (requires `nfpm`: `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.45.0`) |
| `make test` | Run all tests with race detector and coverage |
| `make lint` | Run `golangci-lint` |
| `make security` | Run `govulncheck` |
| `make deploy` | Build + deploy to Pi via SSH/SCP |
| `make setup-apt` | Add t212 APT repository on Pi for automatic updates |
| `make setup-signal` | Register Pi as Signal linked device |
| `make update-signal-cli` | Download and verify latest signal-cli release |
| `make logs` | Tail systemd journal from Pi |
| `make clean` | Remove build artifacts |

Override the Pi host: `make deploy PI_HOST=user@192.168.1.10`

---

## Design decisions

| Concern | Decision | Rationale |
|---|---|---|
| Filter rule | `currentPrice − averagePrice > £1.00` (strict greater-than) | Simple, predictable, no floating-point ambiguity at the boundary |
| Polling rate | 1 req/min | Conservative interval to avoid 429s from T212 |
| WebSocket fan-out | Buffered channels (size 8), slow subscribers skipped | Prevents one slow client from blocking the broadcast loop |
| No database | In-memory store only | Data is live prices; stale data on restart is fine |
| Signal transport | Linked device (not a separate Signal account) | Simpler setup; no secondary phone number needed |
| TLS | 1.3 minimum for all outbound connections | Enforced via `tls.Config{MinVersion: tls.VersionTLS13}` |
| Profit field | Computed from `currentPrice − averagePrice` after fetch | T212 API returns raw prices; `ProfitPerShare` and `MarketValue` are derived |
| Notifications | Edge-detected via `prevAbove map[string]bool` | One alert on crossing, not one per poll while the position stays above |
| Local transport | HTTP on LAN | HTTPS/Let's Encrypt deferred to a future iteration |

---

## Security

- **API key** stored in `/etc/t212/config.env` (`0600`, `root:root`), loaded via systemd `EnvironmentFile=`; never passed as a CLI argument or logged
- **TLS 1.3** enforced for all outbound connections; `InsecureSkipVerify` never set
- **Web server** sends security headers on every response: `Content-Security-Policy`, `X-Frame-Options`, `X-Content-Type-Options`, `Referrer-Policy`
- **WebSocket** endpoint validates origin and limits to 5 concurrent connections
- **systemd hardening**: `NoNewPrivileges`, `ProtectSystem=strict`, `PrivateTmp`, `ProtectHome`, `PrivateDevices`, `CapabilityBoundingSet=` (empty)
- **No financial data** persisted to disk at any point

---

## Project structure

```
t212/
├── cmd/t212/            # main, serve, tui_cmd
├── internal/
│   ├── api/             # T212 HTTP client, Position model, rate-limit parsing
│   ├── filter/          # profit-per-share threshold filter
│   ├── store/           # thread-safe in-memory position store
│   ├── hub/             # WebSocket fan-out hub
│   ├── poller/          # polling loop, edge-detection, notifier interface
│   ├── notifier/        # signal-cli subprocess notifier
│   ├── server/          # HTTP server, WebSocket upgrade, static assets
│   └── tui/             # bubbletea terminal UI
├── internal/server/web/ # index.html, style.css, app.js (embedded)
├── deploy/
│   ├── t212.service       # systemd unit
│   ├── config.env.example
│   ├── postinst.sh        # dpkg post-install: create user, enable service, print instructions
│   ├── prerm.sh           # dpkg pre-remove: stop and disable service
│   └── postrm.sh          # dpkg post-remove: purge user and config on --purge
├── .github/workflows/
│   ├── ci.yml             # test + build-arm + govulncheck (on push/PR to main)
│   └── release.yml        # test + build .deb + publish GitHub Release (on semver tags)
├── nfpm.yaml              # nfpm package definition
├── Makefile
└── go.mod
```

---

## Future work

- HTTPS via Let's Encrypt (DNS-01 challenge, certbot on DietPi)
- Configurable profit threshold via web UI (currently hardcoded at £1.00)
- Historical profit chart (in-memory ring buffer, no DB needed)
