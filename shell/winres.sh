#!/usr/bin/env bash
# Regenerates shell/rsrc_windows_amd64.syso: the executable's icon (the same
# artwork the web preview and the macOS bundle use) and its version block.
# `go build` embeds the .syso automatically, so the icon ships with a plain
# build; run this after changing the artwork or bumping the version, then
# commit the result.
#
# There is deliberately no manifest here. The app calls
# SetProcessDpiAwarenessContext(per-monitor v2) itself, and a manifest that
# declares a DPI mode would win over that call and blur the window on scaled
# displays.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-0.1.0}"
ICON="$PWD/web/public/android-chrome-512x512.png"
# Pinned so regenerating the checked-in .syso does not pick up a new tool.
WINRES="${WINRES:-go run github.com/tc-hib/go-winres@v0.3.3}"

if [ ! -f "$ICON" ]; then
  echo "missing $ICON" >&2
  exit 1
fi

cd shell
$WINRES simply \
  --arch amd64 \
  --out rsrc \
  --icon "$ICON" \
  --manifest none \
  --product-name "Joplin Lite" \
  --product-version "$VERSION" \
  --file-version "$VERSION" \
  --file-description "Joplin Lite native note app" \
  --original-filename gojoplin-desktop.exe \
  --copyright "AGPL-3.0-or-later; see LICENSE and NOTICE"

echo "wrote shell/rsrc_windows_amd64.syso ($VERSION)"
