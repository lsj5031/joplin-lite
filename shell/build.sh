#!/usr/bin/env bash
# Builds the native desktop app. There is no frontend build step and no
# embedded SPA: the window is drawn by mygo/ui, and the web build stays an
# optional browser preview (`cd web && npm run build`, then
# `gojoplin serve --web-dir web/dist/client`).
set -euo pipefail
cd "$(dirname "$0")/.."

cd shell
CGO_ENABLED=0 GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.1}" go build -trimpath \
  -ldflags="-s -w" -o "${OUT:-../tmp/gojoplin-desktop.exe}" .
