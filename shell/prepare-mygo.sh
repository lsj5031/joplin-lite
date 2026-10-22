#!/usr/bin/env bash
# Materialize the pinned dependency and apply our tracked upstream patches.
# Never edit the shared Go module cache or overwrite an existing local copy.
set -euo pipefail
cd "$(dirname "$0")/.."
root="$PWD"
version="v0.5.5"
patches=(
  "$root/shell/patches/mygo-large-layouts.patch"
  "$root/shell/patches/mygo-inline-transforms.patch"
  "$root/shell/patches/mygo-list-estimate.patch"
)
dest="$root/tmp/mygo-patched"
stamp="$version:$(git hash-object "${patches[@]}")"
export GOTOOLCHAIN="${GOTOOLCHAIN:-go1.27.1}"

if [ -e "$dest" ]; then
  if [ -f "$dest/.joplin-patch" ] &&
     [ "$(cat "$dest/.joplin-patch")" = "$stamp" ] &&
     git -C "$dest" apply --reverse --check "${patches[@]}"; then
    exit 0
  fi
  echo "MyGO copy is stale or modified: $dest" >&2
  echo "Move it aside, then rerun bash shell/prepare-mygo.sh. It was not overwritten." >&2
  exit 1
fi

go mod download "github.com/egoist/mygo@$version"
source="$(go list -m -f '{{.Dir}}' "github.com/egoist/mygo@$version")"
if [ -z "$source" ]; then
  echo "Cannot locate MyGO $version in the module cache." >&2
  exit 1
fi
mkdir -p "$root/tmp"
stage="$(mktemp -d "$root/tmp/mygo-prepare.XXXXXX")"
trap 'if [ -n "$stage" ]; then rm -rf -- "$stage"; fi' EXIT
cp -R "$source/." "$stage/"
chmod -R u+w "$stage"
git -C "$stage" init -q
git -C "$stage" apply --check "${patches[@]}"
git -C "$stage" apply "${patches[@]}"
printf '%s\n' "$stamp" > "$stage/.joplin-patch"
mv "$stage" "$dest"
stage=""
echo "Prepared MyGO $version with large-layout cache and paint fixes."
