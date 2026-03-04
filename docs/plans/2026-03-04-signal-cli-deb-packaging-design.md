# signal-cli .deb Packaging & Auto-Update Design

## Goal

Package signal-cli as a standalone `.deb` (`signal-cli`) in our APT repository so it can be installed and updated via `apt upgrade`, replacing the manual SSH-based update workflow.

## Context

signal-cli is currently installed manually on the Pi. The existing `signal-cli-update.yml` workflow checks for new releases weekly and creates a PR, but the user still has to run `make update-signal-cli` to install it via SSH. This design automates the entire lifecycle through APT.

## Design Decisions

### Package Design

- **Package name:** `signal-cli`
- **Arch:** `arm64` (matches our repo; Pi is the only target)
- **Install location:** `/opt/signal-cli/` (libs/jars) with symlink `/usr/local/bin/signal-cli` → `/opt/signal-cli/bin/signal-cli`
- **Version:** Matches upstream signal-cli version (e.g., `0.13.12`)
- **postinst:** Checks for Java at install time; prints warning if missing (`"WARNING: Java JRE not found. signal-cli requires Java 17+. Install via: dietpi-software install 196"`) but does NOT fail the install

### t212 Dependency

- Add `Recommends: signal-cli` to `nfpm.yaml` (not `Depends` — Signal notifications are optional)
- Users who don't want Signal notifications are not forced to install it

### Cron Workflow

Replace the existing `signal-cli-update.yml` with a workflow that:

1. Runs weekly (Monday 09:00 UTC) + manual `workflow_dispatch`
2. Fetches latest release tag from `AsamK/signal-cli` GitHub API
3. Compares against `.signal-cli-version` file — skips if no update
4. Downloads the signal-cli Linux tarball from the release
5. Builds a `signal-cli` `.deb` using nfpm
6. Publishes the `.deb` to the APT repo on `gh-pages` (reuses GPG signing + dpkg-scanpackages flow)
7. Updates `.signal-cli-version` file and commits to main

### Systemd Fix

- Keep `ProtectHome=yes` in `t212.service` (security hardening)
- Add `SIGNAL_CLI_CONFIG=/var/lib/t212/signal-cli` to the service environment
- t212 `postinst.sh` creates `/var/lib/t212/signal-cli` directory with `t212:t212` ownership
- signal-cli stores its linked device keys there instead of `~/.local/share/signal-cli/`

### Cleanup

Remove files that are replaced by the APT-based workflow:

- `scripts/update-signal-cli.sh` — replaced by `apt upgrade`
- Makefile `update-signal-cli` target — same reason
- Keep Makefile `setup-signal` target — still needed for one-time QR code linking
- Update README signal-cli section to reference `apt upgrade`

## Files to Modify/Create

| File | Change |
|------|--------|
| `.github/workflows/signal-cli-update.yml` | Replace: new workflow that builds .deb and publishes to APT |
| `deploy/signal-cli-postinst.sh` | New: Java check + warning |
| `deploy/signal-cli-nfpm.yaml` | New: nfpm config for signal-cli .deb |
| `deploy/t212.service` | Add `Environment=SIGNAL_CLI_CONFIG=/var/lib/t212/signal-cli` |
| `deploy/postinst.sh` | Create `/var/lib/t212/signal-cli` directory with t212 ownership |
| `nfpm.yaml` | Add `Recommends: signal-cli` |
| `scripts/update-signal-cli.sh` | Delete |
| `Makefile` | Remove `update-signal-cli` target, update docs table |
| `README.md` | Update signal-cli section |
