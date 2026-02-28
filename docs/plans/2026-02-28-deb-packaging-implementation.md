# Debian Package & GitHub Releases — Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Produce a `t212_<version>_arm64.deb` package that installs the binary, systemd service, and config file in one command, and automatically publishes a GitHub Release with that package whenever a semver tag is pushed.

**Architecture:** `nfpm` reads `nfpm.yaml` to assemble the `.deb` from the cross-compiled arm64 binary and files in `deploy/`. Three lifecycle shell scripts handle user creation, service enable (not start), and purge cleanup. A new GitHub Actions workflow (`release.yml`) triggers on semver tags, re-runs tests, builds the `.deb`, and creates a GitHub Release with it as an artifact.

**Tech Stack:** nfpm v2 (`github.com/goreleaser/nfpm/v2`), GNU make, GitHub Actions (`gh` CLI built into ubuntu-latest runners), dpkg

---

### Task 1: deploy/postinst.sh — post-install lifecycle script

**Files:**
- Create: `deploy/postinst.sh`

This script runs after dpkg places all files. It creates the `t212` system user (idempotent), secures the config directory and file, enables the service on boot (does NOT start it), and prints next-step instructions to stdout.

**Step 1: Create the script**

```bash
#!/bin/sh
set -e

case "$1" in
    configure)
        # Create system user if not already present
        if ! id -u t212 >/dev/null 2>&1; then
            useradd --system --no-create-home --shell /usr/sbin/nologin t212
        fi

        # Secure the config directory
        chown root:root /etc/t212
        chmod 700 /etc/t212

        # Secure the config file (nfpm installs it; we set ownership and mode here)
        chown root:root /etc/t212/config.env
        chmod 0600 /etc/t212/config.env

        # Register and enable the service (does NOT start it)
        systemctl daemon-reload
        systemctl enable t212

        echo ""
        echo "================================================================"
        echo " t212 installed successfully."
        echo "================================================================"
        echo ""
        echo " Next steps:"
        echo ""
        echo "  1. Edit the config file and set your credentials:"
        echo "       sudo nano /etc/t212/config.env"
        echo ""
        echo "     Required:"
        echo "       T212_API_KEY=<your Trading 212 live API key>"
        echo ""
        echo "     Optional:"
        echo "       SIGNAL_NUMBER=+447700000000   (Signal notifications)"
        echo "       T212_PORT=8080                (web UI port, default: 8080)"
        echo ""
        echo "  2. Start the service:"
        echo "       sudo systemctl start t212"
        echo ""
        echo "  3. Check it is running:"
        echo "       sudo systemctl status t212"
        echo "       sudo journalctl -u t212 -f"
        echo ""
        echo "  4. Open the web UI at:  http://$(hostname):8080"
        echo ""
        echo "================================================================"
        echo ""
        ;;
esac
```

Save as `deploy/postinst.sh`.

**Step 2: Make executable**

```bash
chmod +x deploy/postinst.sh
```

**Step 3: Verify with shellcheck (install if needed: brew install shellcheck)**

```bash
shellcheck deploy/postinst.sh
```

Expected: no warnings or errors.

**Step 4: Commit**

```bash
git add deploy/postinst.sh
git commit -m "feat: add postinst lifecycle script for deb package"
```

---

### Task 2: deploy/prerm.sh and deploy/postrm.sh — remove/purge scripts

**Files:**
- Create: `deploy/prerm.sh`
- Create: `deploy/postrm.sh`

**Step 1: Create deploy/prerm.sh**

Runs before files are removed. Stops and disables the service so dpkg can safely delete the binary.

```bash
#!/bin/sh
set -e

case "$1" in
    remove|upgrade|deconfigure)
        systemctl stop t212 || true
        systemctl disable t212 || true
        ;;
esac
```

**Step 2: Create deploy/postrm.sh**

Runs after files are removed. On `purge` only: deletes the config directory and system user. On regular `remove`, config survives so it's preserved across reinstall.

```bash
#!/bin/sh
set -e

case "$1" in
    purge)
        rm -rf /etc/t212

        if id -u t212 >/dev/null 2>&1; then
            userdel t212 || true
        fi
        ;;
esac
```

**Step 3: Make both executable**

```bash
chmod +x deploy/prerm.sh deploy/postrm.sh
```

**Step 4: Verify with shellcheck**

```bash
shellcheck deploy/prerm.sh deploy/postrm.sh
```

Expected: no warnings or errors.

**Step 5: Commit**

```bash
git add deploy/prerm.sh deploy/postrm.sh
git commit -m "feat: add prerm and postrm lifecycle scripts for deb package"
```

---

### Task 3: nfpm.yaml — package definition

**Files:**
- Create: `nfpm.yaml`

nfpm reads this file to know what goes in the package. `config|noreplace` on the config file means dpkg lists it as a conffile: installed fresh on first install, never overwritten on upgrade if the user has modified it.

**Step 1: Create nfpm.yaml in the project root**

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
  Polls live positions every second, filters by the £1 profit-per-share
  threshold, serves a WebSocket-powered web UI, and sends Signal
  notifications on threshold crossings.
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
  preremove:   ./deploy/prerm.sh
  postremove:  ./deploy/postrm.sh
```

**Step 2: Install nfpm if not already installed**

```bash
go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.4
```

**Step 3: Build the arm64 binary**

```bash
make build-arm
```

Expected: `t212-arm64` appears in the project root.

**Step 4: Build the .deb (version will be 0.0.0-dev since no tag exists yet)**

Run from project root:

```bash
VERSION=0.0.0-dev nfpm package --packager deb --target .
```

Expected: `t212_0.0.0-dev_arm64.deb` is created.

**Step 5: Inspect the package contents**

```bash
dpkg --info t212_0.0.0-dev_arm64.deb
dpkg -c t212_0.0.0-dev_arm64.deb
```

Expected output of `dpkg --info`:
- Package: t212
- Architecture: arm64
- Section: utils

Expected output of `dpkg -c` — you should see all four paths:
```
./usr/local/bin/t212
./etc/systemd/system/t212.service
./etc/t212/config.env
```

**Step 6: Verify conffiles**

```bash
dpkg --info t212_0.0.0-dev_arm64.deb | grep -A5 "Conffiles"
# OR inspect the control archive directly:
ar p t212_0.0.0-dev_arm64.deb control.tar.gz | tar -tzf - | grep conffiles
```

`/etc/t212/config.env` should appear in the conffiles list.

**Step 7: Commit**

```bash
git add nfpm.yaml
git commit -m "feat: add nfpm.yaml package definition"
```

---

### Task 4: Makefile — add deb target

**Files:**
- Modify: `Makefile`

Add a `VERSION` variable at the top (reads from git tag, falls back to `0.0.0-dev`). Add a `deb` target that builds the arm64 binary then runs nfpm. Update `clean` to remove `*.deb`.

**Step 1: Add VERSION variable after the existing variables (after line 6, `PI_CFG_DIR := /etc/t212`)**

Insert after `PI_CFG_DIR := /etc/t212`:

```makefile
VERSION     ?= $(shell git describe --tags --abbrev=0 2>/dev/null | sed 's/^v//' || echo "0.0.0-dev")
```

**Step 2: Add `deb` to the .PHONY line**

Change:
```makefile
.PHONY: build build-arm test lint security deploy setup-signal update-signal-cli logs clean
```
To:
```makefile
.PHONY: build build-arm test lint security deb deploy setup-signal update-signal-cli logs clean
```

**Step 3: Add the deb target after the build-arm target (after line 16)**

Insert after the `build-arm` block:

```makefile
## deb: build .deb package for linux/arm64 (requires nfpm: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.4)
deb: build-arm
	@command -v nfpm >/dev/null 2>&1 || { echo "nfpm not found. Install: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.4"; exit 1; }
	VERSION=$(VERSION) nfpm package --packager deb --target .
	@echo "Built: t212_$(VERSION)_arm64.deb"
```

**Step 4: Update the clean target to remove .deb files**

Change:
```makefile
clean:
	rm -f $(BINARY) $(BINARY_ARM) coverage.out
```
To:
```makefile
clean:
	rm -f $(BINARY) $(BINARY_ARM) coverage.out *.deb
```

**Step 5: Verify the deb target works**

```bash
make deb
```

Expected:
```
GOARCH=arm64 GOOS=linux go build -o t212-arm64 ./cmd/t212
VERSION=0.0.0-dev nfpm package --packager deb --target .
Built: t212_0.0.0-dev_arm64.deb
```

**Step 6: Verify clean removes .deb**

```bash
make clean
ls *.deb 2>/dev/null || echo "clean: OK"
```

Expected: `clean: OK`

**Step 7: Commit**

```bash
git add Makefile
git commit -m "feat: add make deb target using nfpm"
```

---

### Task 5: .github/workflows/release.yml — tag-triggered release workflow

**Files:**
- Create: `.github/workflows/release.yml`

Triggers on semver tags (e.g. `1.0.0`). Re-runs tests as a safety gate, then builds the `.deb` and creates a GitHub Release with it as the attached artifact. Uses `--generate-notes` to auto-populate release notes from commits since the previous tag.

**Step 1: Create .github/workflows/release.yml**

```yaml
name: Release

on:
  push:
    tags:
      - '[0-9]+.[0-9]+.[0-9]+'

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
        run: go test -race ./...

  release:
    name: Build .deb and publish release
    needs: [test]
    runs-on: ubuntu-latest
    permissions:
      contents: write
    steps:
      - uses: actions/checkout@v4
        with:
          fetch-depth: 0
      - uses: actions/setup-go@v5
        with:
          go-version-file: go.mod
      - name: Cross-compile for linux/arm64
        run: GOARCH=arm64 GOOS=linux go build -o t212-arm64 ./cmd/t212
      - name: Install nfpm
        run: go install github.com/goreleaser/nfpm/v2/cmd/nfpm@v2.41.4
      - name: Build .deb
        run: VERSION=${{ github.ref_name }} nfpm package --packager deb --target .
      - name: Create GitHub Release
        run: |
          gh release create ${{ github.ref_name }} \
            --title "${{ github.ref_name }}" \
            --generate-notes \
            t212_${{ github.ref_name }}_arm64.deb
        env:
          GH_TOKEN: ${{ github.token }}
```

**Step 2: Verify YAML syntax**

```bash
python3 -c "import yaml, sys; yaml.safe_load(open('.github/workflows/release.yml'))" && echo "YAML valid"
```

Expected: `YAML valid`

**Step 3: Commit**

```bash
git add .github/workflows/release.yml
git commit -m "feat: add tag-triggered GitHub release workflow"
```

**Step 4: Smoke-test the release workflow by pushing a tag**

From main (after merging this PR):
```bash
git tag 0.1.0
git push origin 0.1.0
```

Then watch the Actions tab: `https://github.com/ko5tas/t212/actions`

Expected: the `Release` workflow runs, `Test` passes, `Build .deb and publish release` passes, and a release named `0.1.0` appears at `https://github.com/ko5tas/t212/releases` with `t212_0.1.0_arm64.deb` attached.

---

### Task 6: README — installation section and Makefile table update

**Files:**
- Modify: `README.md`

Add an **Installation** section (the primary way to install on DietPi) immediately after the **Prerequisites** section. Update the Makefile targets table. Update the project structure listing to include the new files.

**Step 1: Add Installation section**

After the `---` that closes the Prerequisites section and before `## Quick start`, insert:

```markdown
## Installation (DietPi / Raspberry Pi 5)

Download the latest `.deb` from the [Releases page](https://github.com/ko5tas/t212/releases/latest):

```bash
wget https://github.com/ko5tas/t212/releases/latest/download/t212_<version>_arm64.deb
sudo dpkg -i t212_<version>_arm64.deb
```

The installer prints the exact steps to configure and start the service. Summary:

```bash
# 1. Set your API key (and optionally SIGNAL_NUMBER, T212_PORT)
sudo nano /etc/t212/config.env

# 2. Start the service
sudo systemctl start t212

# 3. Verify
sudo systemctl status t212
sudo journalctl -u t212 -f
```

Open `http://raspberrypi.local:8080` in a browser on the same LAN.

**Upgrading:** re-download and `sudo dpkg -i t212_<new-version>_arm64.deb`. Your `/etc/t212/config.env` is preserved automatically.

**Removing:** `sudo dpkg -r t212` (config survives). `sudo dpkg --purge t212` (config deleted).

---
```

**Step 2: Add `make deb` to the Makefile targets table**

In the Makefile targets table, add a row after `make build-arm`:

```markdown
| `make deb` | Build `.deb` package for Raspberry Pi (requires `nfpm`) |
```

**Step 3: Update project structure listing**

In the project structure code block, update the `deploy/` and `.github/workflows/` sections:

```
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
```

**Step 4: Verify README renders correctly**

```bash
# Quick visual check — no broken markdown
grep -n "^##" README.md
```

Expected: all section headers appear in order with no duplicates.

**Step 5: Commit**

```bash
git add README.md
git commit -m "docs: add deb installation section and update Makefile targets"
```

---

## Summary of new files

| File | Purpose |
|---|---|
| `nfpm.yaml` | Package definition: contents, scripts, metadata |
| `deploy/postinst.sh` | Create user, set permissions, enable service, print instructions |
| `deploy/prerm.sh` | Stop and disable service before removal/upgrade |
| `deploy/postrm.sh` | Remove user and config directory on `dpkg --purge` |
| `.github/workflows/release.yml` | Semver-tag-triggered: test → build .deb → GitHub Release |

## Modified files

| File | Change |
|---|---|
| `Makefile` | `VERSION` variable, `deb` target, `clean` includes `*.deb` |
| `README.md` | Installation section, Makefile targets table, project structure |
