#!/usr/bin/env bash
# Builds the native desktop app. There is no frontend build step and no
# embedded SPA: the window is drawn by mygo/ui, and the web build stays an
# optional browser preview (`cd web && npm run build`, then
# `gojoplin serve --web-dir web/dist/client`).
set -euo pipefail
cd "$(dirname "$0")/.."

cd shell

# A desktop app must not open a console window when it is double-clicked. The
# icon and version block come from rsrc_windows_amd64.syso, which go build
# embeds automatically; regenerate it with shell/winres.sh after changing the
# icon or the version.
LDFLAGS="-s -w"
if [ "$(go env GOOS)" = "windows" ]; then
  LDFLAGS="-H=windowsgui -s -w"
fi

CGO_ENABLED=0 GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.1}" go build -trimpath \
  -ldflags="$LDFLAGS" -o "${OUT:-../tmp/gojoplin-desktop.exe}" .
