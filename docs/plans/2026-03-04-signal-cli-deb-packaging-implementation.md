# signal-cli .deb Packaging & Auto-Update Implementation Plan

> **For Claude:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task.

**Goal:** Package signal-cli as a `.deb` in our APT repository with automated weekly builds so the Pi receives updates via `apt upgrade`.

**Architecture:** A new GitHub Actions workflow replaces the existing PR-based update check. It downloads the upstream signal-cli tarball, packages it as a `.deb` using nfpm, and publishes to our `gh-pages` APT repository. The t212 package declares `Recommends: signal-cli` and the systemd service is updated to use `/var/lib/t212/signal-cli` for signal-cli data (avoiding `ProtectHome=yes` conflicts).

**Tech Stack:** GitHub Actions, nfpm, dpkg-scanpackages, apt-ftparchive, GPG signing

---

### Task 1: Create signal-cli nfpm config and postinst script

**Files:**
- Create: `deploy/signal-cli-nfpm.yaml`
- Create: `deploy/signal-cli-postinst.sh`

**Step 1: Create the nfpm config for signal-cli**

Create `deploy/signal-cli-nfpm.yaml`:

```yaml
name: signal-cli
arch: arm64
platform: linux
version: "${SIGNAL_CLI_VERSION}"
section: utils
priority: optional
maintainer: "t212 <noreply@github.com>"
description: |
  signal-cli repackaged for the t212 APT repository.
  Command-line interface for the Signal messenger.
  Upstream: https://github.com/AsamK/signal-cli
homepage: https://github.com/AsamK/signal-cli

contents:
  - src: ./signal-cli-dist/
    dst: /opt/signal-cli/

  - dst: /usr/local/bin/signal-cli
    type: symlink
    src: /opt/signal-cli/bin/signal-cli

scripts:
  postinstall: ./deploy/signal-cli-postinst.sh
```

**Step 2: Create the postinst script**

Create `deploy/signal-cli-postinst.sh`:

```bash
#!/bin/sh
set -e

case "$1" in
    configure)
        # Check for Java runtime
        if ! command -v java >/dev/null 2>&1; then
            echo ""
            echo "================================================================"
            echo " WARNING: Java JRE not found."
            echo " signal-cli requires Java 17+."
            echo " Install via: dietpi-software install 196"
            echo " Or:          sudo apt install openjdk-17-jre-headless"
            echo "================================================================"
            echo ""
        fi
        ;;
esac
```

**Step 3: Make the postinst executable**

Run: `chmod +x deploy/signal-cli-postinst.sh`

**Step 4: Commit**

```bash
git add deploy/signal-cli-nfpm.yaml deploy/signal-cli-postinst.sh
git commit -m "feat: add signal-cli nfpm config and postinst script"
```

---

### Task 2: Replace signal-cli-update workflow

**Files:**
- Replace: `.github/workflows/signal-cli-update.yml`

**Step 1: Replace the workflow**

Replace the entire contents of `.github/workflows/signal-cli-update.yml` with:

```yaml
name: Update signal-cli package

on:
  schedule:
    - cron: '0 9 * * 1'  # every Monday 09:00 UTC
  workflow_dispatch:

permissions:
  contents: write

jobs:
  check:
    name: Check for updates
    runs-on: ubuntu-latest
    outputs:
      latest: ${{ steps.check.outputs.latest }}
      current: ${{ steps.check.outputs.current }}
      update_available: ${{ steps.check.outputs.update_available }}
    steps:
      - uses: actions/checkout@v4
      - name: Compare versions
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

  build-and-publish:
    name: Build and publish signal-cli .deb
    needs: [check]
    if: needs.check.outputs.update_available == 'true'
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4

      - name: Download signal-cli tarball
        env:
          VERSION: ${{ needs.check.outputs.latest }}
        run: |
          CLEAN_VERSION="${VERSION#v}"
          TARBALL="signal-cli-${CLEAN_VERSION}-Linux.tar.gz"
          URL="https://github.com/AsamK/signal-cli/releases/download/${VERSION}/${TARBALL}"
          echo "Downloading ${URL}..."
          curl -fsSL -o "${TARBALL}" "${URL}"
          mkdir -p signal-cli-dist
          tar -xzf "${TARBALL}" --strip-components=1 -C signal-cli-dist

      - name: Install nfpm
        run: |
          curl -fsSL https://github.com/goreleaser/nfpm/releases/download/v2.45.0/nfpm_2.45.0_linux_amd64.tar.gz \
            | tar -xz -C /usr/local/bin nfpm

      - name: Build .deb
        env:
          VERSION: ${{ needs.check.outputs.latest }}
        run: |
          CLEAN_VERSION="${VERSION#v}"
          SIGNAL_CLI_VERSION="${CLEAN_VERSION}" nfpm package \
            --config deploy/signal-cli-nfpm.yaml \
            --packager deb \
            --target .
          ls -la signal-cli_*.deb

      - name: Check out gh-pages
        uses: actions/checkout@v4
        with:
          ref: gh-pages
          path: gh-pages

      - name: Import GPG key
        run: |
          echo "${{ secrets.GPG_PRIVATE_KEY }}" | base64 -d | gpg --batch --import

      - name: Publish to APT repository
        run: |
          mkdir -p gh-pages/apt/pool/main gh-pages/apt/dists/stable/main/binary-arm64
          cp signal-cli_*.deb gh-pages/apt/pool/main/

          cd gh-pages/apt

          # Generate Packages index
          dpkg-scanpackages pool/main /dev/null > dists/stable/main/binary-arm64/Packages
          gzip -k -f dists/stable/main/binary-arm64/Packages

          # Generate Release file
          cd dists/stable
          apt-ftparchive release \
            -o APT::FTPArchive::Release::Suite=stable \
            -o APT::FTPArchive::Release::Codename=stable \
            -o APT::FTPArchive::Release::Architectures=arm64 \
            . > Release

          # Sign Release
          gpg --batch --yes --armor --detach-sign -o Release.gpg Release
          gpg --batch --yes --clearsign -o InRelease Release

      - name: Push to gh-pages
        run: |
          cd gh-pages
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add -A
          git commit -m "apt: publish signal-cli ${{ needs.check.outputs.latest }}"
          git push

      - name: Update version tracker
        env:
          VERSION: ${{ needs.check.outputs.latest }}
        run: |
          cd ${{ github.workspace }}
          echo "${VERSION}" > .signal-cli-version
          git config user.name "github-actions[bot]"
          git config user.email "github-actions[bot]@users.noreply.github.com"
          git add .signal-cli-version
          git commit -m "chore: update .signal-cli-version to ${VERSION}"
          git push
```

**Step 2: Commit**

```bash
git add .github/workflows/signal-cli-update.yml
git commit -m "feat: replace signal-cli PR workflow with auto-build and APT publish"
```

---

### Task 3: Update t212 systemd service and postinst

**Files:**
- Modify: `deploy/t212.service:7` — add Environment line
- Modify: `deploy/postinst.sh:17-18` — create signal-cli data directory

**Step 1: Add SIGNAL_CLI_CONFIG to the service file**

In `deploy/t212.service`, after line 7 (`EnvironmentFile=/etc/t212/config.env`), add:

```
Environment="SIGNAL_CLI_CONFIG=/var/lib/t212/signal-cli"
```

The `[Service]` section should now read:

```ini
[Service]
EnvironmentFile=/etc/t212/config.env
Environment="SIGNAL_CLI_CONFIG=/var/lib/t212/signal-cli"
ExecStart=/usr/local/bin/t212 serve
```

**Step 2: Create signal-cli data directory in postinst**

In `deploy/postinst.sh`, after line 17 (`chmod 0600 /etc/t212/config.env`), add:

```bash
        # Create signal-cli data directory (used via SIGNAL_CLI_CONFIG)
        mkdir -p /var/lib/t212/signal-cli
        chown t212:t212 /var/lib/t212/signal-cli
```

**Step 3: Verify the t212 tests still pass**

Run: `go test -race ./...`
Expected: all tests pass (these are deploy script changes, not Go code)

**Step 4: Commit**

```bash
git add deploy/t212.service deploy/postinst.sh
git commit -m "fix: configure signal-cli data dir under /var/lib/t212 for systemd sandboxing"
```

---

### Task 4: Add Recommends to t212 nfpm.yaml

**Files:**
- Modify: `nfpm.yaml:33` — add recommends section

**Step 1: Add recommends**

In `nfpm.yaml`, after line 33 (the last line, `postremove: ./deploy/postrm.sh`), add:

```yaml

recommends:
  - signal-cli
```

**Step 2: Verify nfpm config is valid**

Run: `VERSION=0.0.0-test nfpm package --packager deb --target /tmp/ 2>&1 | head -5`
Expected: should build (or show "nfpm not found" if not installed locally — that's fine, CI has it)

**Step 3: Commit**

```bash
git add nfpm.yaml
git commit -m "feat: add Recommends: signal-cli to t212 .deb package"
```

---

### Task 5: Clean up obsolete files

**Files:**
- Delete: `scripts/update-signal-cli.sh`
- Modify: `Makefile:10` — remove `update-signal-cli` from `.PHONY`
- Modify: `Makefile:57-59` — delete the `update-signal-cli` target

**Step 1: Delete the update script**

Run: `rm scripts/update-signal-cli.sh`

**Step 2: Update Makefile**

In `Makefile` line 10, change:

```makefile
.PHONY: build build-arm test lint security deb deploy setup-apt setup-signal update-signal-cli logs clean
```

to:

```makefile
.PHONY: build build-arm test lint security deb deploy setup-apt setup-signal logs clean
```

Then delete lines 57-59:

```makefile
## update-signal-cli: download and SHA256-verify latest signal-cli release on Pi
update-signal-cli:
	@./scripts/update-signal-cli.sh $(PI_HOST)
```

**Step 3: Verify tests still pass**

Run: `go test -race ./...`
Expected: all tests pass

**Step 4: Commit**

```bash
git add -A
git commit -m "chore: remove obsolete signal-cli manual update script and Makefile target"
```

---

### Task 6: Update README

**Files:**
- Modify: `README.md:55` — update prerequisites
- Modify: `README.md:192-224` — update Signal section
- Modify: `README.md:240` — remove from Makefile targets table

**Step 1: Update prerequisites**

Change line 55 from:

```markdown
- (Optional) `signal-cli` installed on the Pi for notifications
```

to:

```markdown
- (Optional) `signal-cli` for notifications — install via `sudo apt install signal-cli` after adding the APT repo
```

**Step 2: Update Signal notifications section**

Replace lines 196-223 (from `### One-time setup` through the end of `### Updating signal-cli`) with:

```markdown
### One-time setup

Install signal-cli from the t212 APT repo (if not already installed):

```bash
sudo apt install signal-cli
```

Then link the Pi as a Signal device:

```bash
# On the Pi, print a QR code:
make setup-signal PI_HOST=pi@raspberrypi.local
# Scan the QR with your phone: Signal → Settings → Linked Devices → Link New Device
```

Set `SIGNAL_NUMBER` in `/etc/t212/config.env` to your number in E.164 format.

signal-cli is updated automatically via the APT repository (`sudo apt update && sudo apt upgrade`).
```

**Step 3: Remove `update-signal-cli` from Makefile targets table**

Delete line 240:

```markdown
| `make update-signal-cli` | Download and verify latest signal-cli release |
```

**Step 4: Commit**

```bash
git add README.md
git commit -m "docs: update signal-cli instructions for APT-based install and updates"
```

---

### Task 7: Run full verification

**Step 1: Run all Go tests**

Run: `go test -race ./...`
Expected: all tests pass

**Step 2: Run vet**

Run: `go vet ./...`
Expected: clean

**Step 3: Build**

Run: `go build ./cmd/t212`
Expected: builds successfully

**Step 4: Verify no leftover references to update-signal-cli**

Run: `grep -r "update-signal-cli" --include="*.go" --include="*.yml" --include="*.yaml" --include="*.sh" --include="*.md" --include="Makefile" .`
Expected: no matches (all references removed)

**Step 5: Push branch**

```bash
git push origin feature/return-tracking
```

---

### Task 8: Trigger signal-cli workflow manually

After pushing, trigger the new workflow to build and publish the first signal-cli .deb:

```bash
gh workflow run signal-cli-update.yml
```

Watch for completion:

```bash
gh run list --workflow=signal-cli-update.yml --limit 1
gh run watch <run-id>
```

Expected: workflow downloads signal-cli, builds .deb, publishes to gh-pages APT repo, updates `.signal-cli-version`.

Verify on the Pi:

```bash
sudo apt update
apt-cache show signal-cli
sudo apt install signal-cli
signal-cli --version
```
