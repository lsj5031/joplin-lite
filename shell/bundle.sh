#!/usr/bin/env bash
# Packages the native desktop binary as a macOS .app bundle so the Dock shows
# the product icon/name instead of the generic executable tile. The bundle is
# the package, not a wrapper around a webview: the binary already has no SPA
# to embed, and the icon now lives in the bundle rather than in Go code.
set -euo pipefail
cd "$(dirname "$0")/.."

APP="${APP:-$PWD/tmp/Joplin Lite.app}"
BIN="${OUT:-../tmp/Joplin Lite.exe}"
case "$BIN" in
  /* | [A-Za-z]:[\\/]*) ;;
  *) BIN="$PWD/shell/$BIN" ;;
esac
ICNS="$PWD/web/public/app-icon.icns"

bash shell/build.sh

if [ ! -f "$ICNS" ]; then
  echo "missing $ICNS" >&2
  exit 1
fi

rm -rf "$APP"
mkdir -p "$APP/Contents/MacOS" "$APP/Contents/Resources/licenses"
cp "$BIN" "$APP/Contents/MacOS/Joplin Lite"
cp "$ICNS" "$APP/Contents/Resources/app-icon.icns"
# AGPL: ship the license and the attributed notices with the app.
cp LICENSE NOTICE THIRD_PARTY_NOTICES.md "$APP/Contents/Resources/licenses/"
cp shell/fonts/OFL.txt "$APP/Contents/Resources/licenses/"
REVISION="$(git rev-parse HEAD)"
cat > "$APP/Contents/Resources/licenses/SOURCE.txt" <<TXT
Joplin Lite corresponding source and build files:
https://github.com/lsj5031/joplin-lite/tree/$REVISION

Dependency versions and source locations are recorded in go.mod, go.sum,
shell/go.mod, shell/go.sum and THIRD_PARTY_NOTICES.md.
A build with local modifications requires the corresponding modified source.
TXT

cat > "$APP/Contents/Info.plist" <<'PLIST'
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleDevelopmentRegion</key><string>en</string>
  <key>CFBundleDisplayName</key><string>Joplin Lite</string>
  <key>CFBundleExecutable</key><string>Joplin Lite</string>
  <key>CFBundleIdentifier</key><string>com.joplinlite.desktop</string>
  <key>CFBundleURLTypes</key>
  <array>
    <dict>
      <key>CFBundleURLName</key><string>com.joplinlite.desktop.joplin</string>
      <key>CFBundleURLSchemes</key><array><string>joplin</string></array>
    </dict>
  </array>
  <key>CFBundleIconFile</key><string>app-icon</string>
  <key>CFBundleInfoDictionaryVersion</key><string>6.0</string>
  <key>CFBundleName</key><string>Joplin Lite</string>
  <key>CFBundlePackageType</key><string>APPL</string>
  <key>CFBundleShortVersionString</key><string>0.1.0</string>
  <key>CFBundleVersion</key><string>1</string>
  <key>LSMinimumSystemVersion</key><string>11.0</string>
  <key>NSHighResolutionCapable</key><true/>
</dict>
</plist>
PLIST

# Ad-hoc signing gives the bundle a stable identity for LaunchServices.
codesign --force --deep --sign - "$APP" >/dev/null 2>&1 || \
  echo "warning: codesign unavailable; the bundle still runs unsigned" >&2

echo "built $APP"
