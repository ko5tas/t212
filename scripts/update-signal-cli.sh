#!/usr/bin/env bash
# Update signal-cli on the Raspberry Pi to the latest GitHub release.
# Usage: ./scripts/update-signal-cli.sh [PI_HOST]
set -euo pipefail

PI_HOST="${1:-pi@raspberrypi.local}"
INSTALL_DIR="/usr/local/bin"
REPO="AsamK/signal-cli"

echo "Checking latest signal-cli release..."
LATEST=$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest" \
  | grep '"tag_name"' | sed 's/.*"tag_name": "\(.*\)".*/\1/')

echo "Latest: ${LATEST}"

TARBALL="signal-cli-${LATEST#v}-Linux-aarch64.tar.gz"
URL="https://github.com/${REPO}/releases/download/${LATEST}/${TARBALL}"
CHECKSUM_URL="${URL}.sha256sum"

TMPDIR=$(mktemp -d)
trap "rm -rf ${TMPDIR}" EXIT

echo "Downloading ${TARBALL}..."
curl -fsSL -o "${TMPDIR}/${TARBALL}" "${URL}"
curl -fsSL -o "${TMPDIR}/${TARBALL}.sha256sum" "${CHECKSUM_URL}"

echo "Verifying SHA256..."
(cd "${TMPDIR}" && sha256sum -c "${TARBALL}.sha256sum")

echo "Installing on ${PI_HOST}..."
tar -xzf "${TMPDIR}/${TARBALL}" -C "${TMPDIR}"
scp "${TMPDIR}/signal-cli-${LATEST#v}-Linux-aarch64/bin/signal-cli" \
    "${PI_HOST}:/tmp/signal-cli-new"
ssh "${PI_HOST}" "sudo mv /tmp/signal-cli-new ${INSTALL_DIR}/signal-cli && \
                  sudo chmod 755 ${INSTALL_DIR}/signal-cli"

echo "Done. signal-cli ${LATEST} installed on ${PI_HOST}."
echo "${LATEST}" > .signal-cli-version
