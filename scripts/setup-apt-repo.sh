#!/usr/bin/env bash
# Add the t212 APT repository to a Debian/Ubuntu system.
# Usage: curl -fsSL https://ko5tas.github.io/t212/apt/setup.sh | sudo bash
set -euo pipefail

REPO_URL="https://ko5tas.github.io/t212/apt"
KEYRING="/usr/share/keyrings/t212.gpg"
LIST="/etc/apt/sources.list.d/t212.list"

echo "Adding t212 APT repository..."

# Download and install GPG key
curl -fsSL "${REPO_URL}/gpg.key" | gpg --dearmor -o "${KEYRING}"

# Add repository
echo "deb [signed-by=${KEYRING} arch=arm64] ${REPO_URL} stable main" > "${LIST}"

# Update
apt-get update -o Dir::Etc::sourcelist="${LIST}" -o Dir::Etc::sourceparts="-"

echo "Done. Install with: sudo apt install t212"
echo "Updates will arrive via: sudo apt update && sudo apt upgrade"
