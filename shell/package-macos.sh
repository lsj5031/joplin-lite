#!/usr/bin/env bash
# Packages the portable macOS app for a release. Run this on macOS: build.sh
# targets the host OS and bundle.sh ad-hoc signs with codesign, so a Windows
# machine can only produce an unsigned cross-compiled binary.
set -euo pipefail
cd "$(dirname "$0")/.."

VERSION="${VERSION:-0.1.0}"
NAME="Joplin-Lite-$VERSION-macos-arm64"
ZIP="tmp/$NAME.zip"

rm -f "$ZIP"
bash shell/bundle.sh

# Archive without macOS AppleDouble (._) entries; they can break signature
# verification for people who download and unpack the bundle.
(cd tmp && COPYFILE_DISABLE=1 zip -qr "$NAME.zip" "Joplin Lite.app")

echo "packaged $ZIP"
