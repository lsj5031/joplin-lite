#!/usr/bin/env bash
# Regenerates web/public/app-icon.ico and then shell/rsrc_windows_amd64.syso:
# the executable's icon (the same artwork the web preview and the macOS bundle
# use) and its version block.
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
ART="$PWD/web/public/android-chrome-512x512.png"
ICO="$PWD/web/public/app-icon.ico"
# Pinned so regenerating the checked-in .syso does not pick up a new tool.
WINRES="${WINRES:-go run github.com/tc-hib/go-winres@v0.3.3}"

if [ ! -f "$ART" ]; then
  echo "missing $ART" >&2
  exit 1
fi

# One image per size Windows asks for: the title bar and file list should not
# depend on Windows scaling a single 256 px image down.
go run ./tools/icongen -in "$ART" -out "$ICO" -sizes 16,32,48,256

cd shell
$WINRES simply \
  --arch amd64 \
  --out rsrc \
  --icon "$ICO" \
  --manifest none \
  --product-name "Joplin Lite" \
  --product-version "$VERSION" \
  --file-version "$VERSION" \
  --file-description "Joplin Lite native note app" \
  --original-filename gojoplin-desktop.exe \
  --copyright "AGPL-3.0-or-later; see LICENSE and NOTICE"

echo "wrote shell/rsrc_windows_amd64.syso ($VERSION)"
