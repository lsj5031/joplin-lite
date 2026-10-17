#!/usr/bin/env bash
# Builds the web UI, stages it for embedding, then builds the desktop app.
# The MyGO CLI is deliberately not required: the shell uses embed.FS +
# SetFrontend-equivalent handling, so only Go 1.27+ is needed here.
set -euo pipefail
cd "$(dirname "$0")/.."

if [ ! -d web/node_modules ]; then
  (cd web && npm install)
fi
(cd web && npm run build)

rm -rf shell/webdist
mkdir -p shell/webdist
cp -R web/dist/client/. shell/webdist/
# Keep the embed pattern valid in a clean checkout.
touch shell/webdist/.gitkeep

cd shell
CGO_ENABLED=0 GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.1}" go build -trimpath -ldflags="-s -w" \
  -o "${OUT:-../tmp/gojoplin-desktop.exe}" .
