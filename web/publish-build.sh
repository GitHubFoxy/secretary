#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")"
exec 9>"${TMPDIR:-/tmp}/secretary-web-build-$(id -u).lock"
flock 9
stage=$(mktemp -d "$PWD/.build-stage.XXXXXXXX")
trap 'rm -rf "$stage"' EXIT
./node_modules/.bin/vite build --outDir "$stage" --emptyOutDir
mkdir -p dist/assets
# Old hashed assets remain available to clients loading the previous index.
cp -a "$stage/assets/." dist/assets/
for file in "$stage"/*; do
  name=${file##*/}
  case "$name" in assets|index.html|build-version.json) continue;; esac
  if [ -f "$file" ]; then cp "$file" "dist/.${name}.new"; mv -f "dist/.${name}.new" "dist/$name"; fi
done
cp "$stage/index.html" dist/.index.html.new
mv -f dist/.index.html.new dist/index.html
# Publish the version last, after the index and its assets are in place.
cp "$stage/build-version.json" dist/.build-version.json.new
mv -f dist/.build-version.json.new dist/build-version.json
