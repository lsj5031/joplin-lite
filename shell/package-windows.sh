#!/usr/bin/env bash
# Builds the portable Windows package: the GUI-subsystem executable (icon and
# version block embedded from rsrc_windows_amd64.syso) plus the AGPL license
# and notices. The app is portable by decision: unzip the directory and run the
# executable. There is no installer, no registry entry and no uninstaller.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-0.1.0}"
NAME="Joplin-Lite-$VERSION-windows-amd64"
PACKAGE_DIR="tmp/$NAME"
BIN="${OUT:-../tmp/Joplin Lite.exe}"
case "$BIN" in
  /* | [A-Za-z]:[\\/]*) ;;
  *) BIN="$PWD/shell/$BIN" ;;
esac

bash shell/build.sh

rm -rf "$PACKAGE_DIR" "$PACKAGE_DIR.zip"
mkdir -p "$PACKAGE_DIR"
cp "$BIN" "$PACKAGE_DIR/Joplin Lite.exe"
cp LICENSE NOTICE THIRD_PARTY_NOTICES.md "$PACKAGE_DIR/"
cp shell/fonts/OFL.txt "$PACKAGE_DIR/"
REVISION="$(git rev-parse HEAD)"
cat > "$PACKAGE_DIR/SOURCE.txt" <<TXT
Joplin Lite corresponding source and build files:
https://github.com/lsj5031/joplin-lite/tree/$REVISION

Dependency versions and source locations are recorded in go.mod, go.sum,
shell/go.mod, shell/go.sum and THIRD_PARTY_NOTICES.md.
Distribute release binaries from a verified commit. A build with local
modifications requires the corresponding modified source as well.
TXT

cat > "$PACKAGE_DIR/README.txt" <<'TXT'
Joplin Lite (Windows, portable)

Joplin Lite is a native desktop client for a Joplin vault: notebooks, notes,
tags, search and sync, with the web clipper/Data API running in the same
process. There is no browser engine in the app.

Run: double-click Joplin Lite.exe
PowerShell: & ".\Joplin Lite.exe"

The first launch opens the vault in %USERPROFILE%\.local\share\gojoplin.
Without an explicit config, remembered launch selection, GOJOPLIN_CONFIG_PATH,
or an existing %USERPROFILE%\.config\go-joplin\config.yaml, the app creates
its own empty config at %AppData%\joplin-lite\config.json. It does not
implicitly import official Joplin settings.json; use -config to select that
file explicitly. Existing YAML, environment and remembered launch selections
still apply. Settings in the app choose the sync target (file system folder
or Joplin Server), the E2EE master password and the clipper API token.

Local-only (PowerShell): & ".\Joplin Lite.exe" -no-sync

License: AGPL-3.0-or-later. This package includes LICENSE, NOTICE,
THIRD_PARTY_NOTICES.md, OFL.txt and SOURCE.txt.
TXT

# Compress-Archive ships with Windows; zip supports cross-packaging on macOS.
if command -v cygpath >/dev/null 2>&1; then
  powershell -NoProfile -Command \
    "Compress-Archive -Path '$(cygpath -w "$PACKAGE_DIR")\*' -DestinationPath '$(cygpath -w "$PACKAGE_DIR.zip")' -Force"
else
  (cd "$PACKAGE_DIR" && zip -qr "../$NAME.zip" .)
fi

echo "packaged $PACKAGE_DIR.zip"
