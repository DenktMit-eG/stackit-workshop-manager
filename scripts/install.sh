#!/usr/bin/env bash
# Host setup for stackit-ws-manager on Debian/Ubuntu (tested on Ubuntu 26.04):
#   - apt packages: GNU parallel (batch script) and the shared libraries headless Chromium needs
#   - Go build of the binary
#   - Playwright driver + Chromium download (~/.cache/ms-playwright*)
#   - config.yaml from defaults if missing
# Needs sudo (apt). Re-running is harmless.
set -euo pipefail
cd "$(dirname "$0")/.."

# shared libraries for chrome-headless-shell (Playwright's own list for Ubuntu 24.04/26.04; the
# fallback below is used when `playwright install --with-deps` does not know the distro release)
CHROMIUM_DEPS="libasound2t64 libatk-bridge2.0-0t64 libatk1.0-0t64 libatspi2.0-0t64 libcairo2 libcups2t64
libdbus-1-3 libdrm2 libgbm1 libglib2.0-0t64 libnspr4 libnss3 libpango-1.0-0 libx11-6 libxcb1
libxcomposite1 libxdamage1 libxext6 libxfixes3 libxkbcommon0 libxrandr2 fonts-liberation"

if ! command -v go >/dev/null; then
  echo "Go is not installed (https://go.dev/dl/), aborting" >&2
  exit 1
fi

echo "== apt packages"
sudo apt-get update -qq
sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq parallel ca-certificates curl

echo "== build"
go build -o stackit-ws-manager .

echo "== playwright driver + chromium (+ system deps)"
if ! ./stackit-ws-manager browser install --with-deps; then
  echo "playwright --with-deps failed, installing the Chromium libraries explicitly"
  # shellcheck disable=SC2086
  sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq $CHROMIUM_DEPS
  ./stackit-ws-manager browser install
fi

echo "== smoke test: launch headless chromium"
./stackit-ws-manager browser check

if [ ! -f config.yaml ]; then
  echo "== config.yaml"
  ./stackit-ws-manager init
fi
echo "done. next: edit config.yaml, then e.g. scripts/create-accounts.sh customername 20"
