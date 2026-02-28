# Debian Package & GitHub Releases — Design Document

**Date:** 2026-02-28
**Status:** Approved

---

## Goal

Replace the manual multi-step Raspberry Pi deploy process with a single `dpkg -i t212_<version>_arm64.deb` command, and automatically produce that package as a GitHub Release artifact whenever a new semantic version tag is pushed.

---

## Tooling: nfpm

`nfpm` (goreleaser/nfpm) is a cross-platform Go tool that reads a YAML config and produces `.deb` packages. It works on macOS without any additional system tooling, integrates with the existing `go install` workflow, and handles `config|noreplace` correctly out of the box.

Install: `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.4`

---

## Package Contents

| Destination on Pi | Source | dpkg type | Mode |
|---|---|---|---|
| `/usr/local/bin/t212` | `t212-arm64` (cross-compiled binary) | regular | 0755 |
| `/etc/systemd/system/t212.service` | `deploy/t212.service` | regular | 0644 |
| `/etc/t212/config.env` | `deploy/config.env.example` | `config\|noreplace` | 0600 |

`config|noreplace` is the key flag: dpkg installs the example config on first install but **never overwrites it on upgrade** if the user has edited it. This protects the API key across upgrades.

---

## Lifecycle Scripts

### `deploy/postinst.sh` (runs after files are placed)

1. Create system user `t212` (no login shell, no home directory) if it doesn't already exist
2. `chown root:root /etc/t212` + `chmod 700 /etc/t212`
3. `chown root:root /etc/t212/config.env` + `chmod 0600 /etc/t212/config.env`
4. `systemctl daemon-reload`
5. `systemctl enable t212` — enables on boot but does **not** start the service
6. Print next-steps instructions (see below)

**Printed instructions (non-interactive, automation-safe):**
```
t212 installed successfully.

Next steps:
  1. Set your Trading 212 API key:
       sudo nano /etc/t212/config.env
     Required: T212_API_KEY
     Optional: SIGNAL_NUMBER (for Signal notifications), T212_PORT (default: 8080)

  2. Start the service:
       sudo systemctl start t212

  3. Verify it is running:
       sudo systemctl status t212
       sudo journalctl -u t212 -f

  4. Open the web UI in your browser:
       http://<this-host>:8080
```

### `deploy/prerm.sh` (runs before files are removed)

1. `systemctl stop t212 || true`
2. `systemctl disable t212 || true`

### `deploy/postrm.sh` (runs after removal)

- On `--purge` only: remove the `t212` system user and `/etc/t212/` directory
- On regular `remove`: do nothing (config survives for reinstall)

---

## Versioning

- Git tags use bare semantic versioning: `1.0.0`, `1.2.3` (no `v` prefix)
- Deb package version matches the tag exactly
- `make deb` locally: reads version from `git describe --tags --abbrev=0`, strips any accidental `v` prefix, falls back to `0.0.0-dev` if no tags exist
- Version is passed to nfpm via the `VERSION` environment variable

---

## nfpm.yaml

Placed in the project root. References `${VERSION}` and the arm64 binary path.

```yaml
name: t212
arch: arm64
platform: linux
version: "${VERSION}"
section: utils
priority: optional
maintainer: "t212 <noreply@github.com>"
description: |
  Trading 212 portfolio dashboard.
  Polls live positions every second, filters by £1 profit threshold,
  serves a WebSocket-powered web UI and sends Signal notifications.
homepage: https://github.com/ko5tas/t212
contents:
  - src: ./t212-arm64
    dst: /usr/local/bin/t212
    file_info:
      mode: 0755
  - src: ./deploy/t212.service
    dst: /etc/systemd/system/t212.service
  - src: ./deploy/config.env.example
    dst: /etc/t212/config.env
    type: config|noreplace
    file_info:
      mode: 0600
scripts:
  postinstall: ./deploy/postinst.sh
  preremove: ./deploy/prerm.sh
  postremove: ./deploy/postrm.sh
```

---

## Makefile Changes

Add `deb` target, update `clean`, add nfpm install hint:

```makefile
## deb: build .deb package for linux/arm64 (requires nfpm: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.4)
deb: build-arm
    $(eval VERSION := $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo "0.0.0-dev"))
    VERSION=$(VERSION) nfpm package --packager deb --target .
    @echo "Built: t212_$(VERSION)_arm64.deb"
```

Update `clean` to also remove `*.deb`.

---

## GitHub Actions: release.yml

New file: `.github/workflows/release.yml`

**Trigger:** push of a tag matching `[0-9]+.[0-9]+.[0-9]+`

**Jobs (sequential):**

```
test → build-deb → release
```

1. **test** — same as ci.yml (race detector, coverage) — safety net before publishing
2. **build-deb**:
   - `actions/checkout@v4` (with `fetch-depth: 0` for tag access)
   - `actions/setup-go@v5`
   - Cross-compile `linux/arm64` binary
   - `go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.4`
   - Extract version from tag: `VERSION=${GITHUB_REF_NAME}` (tag is already `1.2.3`)
   - `VERSION=$VERSION nfpm package --packager deb --target .`
   - Upload `t212_*.deb` as workflow artifact
3. **release**:
   - Download artifact from build-deb
   - `gh release create ${{ github.ref_name }} --title "${{ github.ref_name }}" --generate-notes t212_*.deb`

`--generate-notes` auto-populates release notes from commits since the previous tag.

The workflow uses `GITHUB_TOKEN` (built-in, no extra secrets needed) for the release.

---

## README Changes

Add an **Installation** section before the existing "Quick start" section:

```markdown
## Installation (DietPi / Raspberry Pi 5)

Download the latest release from the [Releases page](https://github.com/ko5tas/t212/releases):

    wget https://github.com/ko5tas/t212/releases/latest/download/t212_<version>_arm64.deb
    sudo dpkg -i t212_<version>_arm64.deb

The installer will print the exact steps to configure and start the service.
```

Also update the Makefile targets table to include `make deb`.

---

## New Files Summary

| File | Purpose |
|---|---|
| `nfpm.yaml` | nfpm package definition |
| `deploy/postinst.sh` | Post-install: create user, set permissions, enable service, print instructions |
| `deploy/prerm.sh` | Pre-remove: stop and disable service |
| `deploy/postrm.sh` | Post-remove: purge user and config directory |
| `.github/workflows/release.yml` | Tag-triggered: test → build deb → create GitHub release |

## Modified Files Summary

| File | Change |
|---|---|
| `Makefile` | Add `deb` target, update `clean` |
| `README.md` | Add Installation section, update Makefile targets table |
