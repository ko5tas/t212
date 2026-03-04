#!/usr/bin/env bash
# Add the t212 APT repository to a Debian/Ubuntu system.
# Usage: curl -fsSL https://ko5tas.github.io/t212/apt/setup.sh | sudo bash
set -euo pipefail

REPO_URL="https://ko5tas.github.io/t212/apt"
KEYRING="/usr/share/keyrings/t212.gpg"
SOURCES="/etc/apt/sources.list.d/t212.sources"

echo "Adding t212 APT repository..."

# Remove old .list format if present
rm -f /etc/apt/sources.list.d/t212.list

# Download and install GPG key
curl -fsSL "${REPO_URL}/gpg.key" | gpg --dearmor -o "${KEYRING}"

# Add repository (DEB822 format)
cat > "${SOURCES}" <<EOF
Types: deb
URIs: ${REPO_URL}
Suites: stable
Components: main
Architectures: arm64
Signed-By: ${KEYRING}
EOF

# Update
apt-get update -o Dir::Etc::sourcelist="${SOURCES}" -o Dir::Etc::sourceparts="-"

echo "Done. Install with: sudo apt install t212"
echo "Updates will arrive via: sudo apt update && sudo apt upgrade"
