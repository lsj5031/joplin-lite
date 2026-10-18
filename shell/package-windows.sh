#!/usr/bin/env bash
# Builds the portable Windows package: the GUI-subsystem executable (icon and
# version block embedded from rsrc_windows_amd64.syso) plus the AGPL license
# and notices. There is no installer yet; unzip the directory and run the
# executable. The same binary is what a future installer would wrap.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-0.1.0}"
NAME="Joplin-Lite-$VERSION-windows-amd64"
OUT="tmp/$NAME"

bash shell/build.sh

rm -rf "$OUT" "$OUT.zip"
mkdir -p "$OUT"
cp tmp/gojoplin-desktop.exe "$OUT/"
cp LICENSE NOTICE "$OUT/"

cat > "$OUT/README.txt" <<'TXT'
Joplin Lite (Windows, portable)

Joplin Lite is a native desktop client for a Joplin vault: notebooks, notes,
tags, search and sync, with the web clipper/Data API running in the same
process. There is no browser engine in the app.

Run: gojoplin-desktop.exe

The first launch opens the vault in %LOCALAPPDATA%\gojoplin (config from
%USERPROFILE%\.config\joplin-desktop\settings.json or -config). Settings in
the app choose the sync target (file system folder or Joplin Server), the E2EE
master password and the clipper API token.

Local-only: gojoplin-desktop.exe -no-sync

License: AGPL-3.0-or-later. This package includes LICENSE and NOTICE; the
source is https://github.com/lsj5031/joplin-lite.
TXT

# Compress-Archive ships with Windows; it needs Windows paths.
powershell -NoProfile -Command \
  "Compress-Archive -Path '$(cygpath -w "$OUT")\*' -DestinationPath '$(cygpath -w "$OUT.zip")' -Force"

echo "packaged $OUT.zip"
