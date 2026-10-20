#!/usr/bin/env bash
set -euo pipefail
umask 077

fail() { printf 'Secretary install: %s\n' "$*" >&2; exit 1; }

case "$(uname -s)/$(uname -m)" in
  Linux/x86_64) platform=linux; arch=amd64 ;;
  Linux/aarch64|Linux/arm64) platform=linux; arch=arm64 ;;
  Darwin/arm64) platform=darwin; arch=arm64 ;;
  Darwin/x86_64) platform=darwin; arch=amd64 ;;
  *) fail "Unsupported host $(uname -s)/$(uname -m); Linux and macOS are supported" ;;
esac

for tool in curl mktemp; do
  command -v "$tool" >/dev/null 2>&1 || fail "Required tool missing: $tool"
done
if [[ "$platform" == linux ]]; then
  command -v python3 >/dev/null 2>&1 || fail "Python 3 is required by the Linux binary installer"
else
  command -v plutil >/dev/null 2>&1 || fail "plutil is required by the macOS binary installer"
fi

ref="${SECRETARY_INSTALLER_REF:-main}"
[[ "$ref" =~ ^[A-Za-z0-9][A-Za-z0-9._/-]*$ && "$ref" != *..* && "$ref" != *//* && "$ref" != */ ]] || fail "Invalid SECRETARY_INSTALLER_REF"
stage="$(mktemp -d "${TMPDIR:-/tmp}/secretary-entry.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT

version="${SECRETARY_VERSION:-}"
if [[ -z "$version" ]]; then
  curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors -o "$stage/releases.json" "https://api.github.com/repos/GitHubFoxy/secretary/releases?per_page=30" || fail "Cannot list published releases"
  if [[ "$platform" == linux ]]; then
    version="$(python3 - "$stage/releases.json" "$platform" "$arch" <<'PY'
import json, re, sys
with open(sys.argv[1], encoding="utf-8") as f:
    releases = json.load(f)
platform, arch = sys.argv[2:]
for release in releases:
    tag = release.get("tag_name", "")
    if release.get("draft") or not release.get("published_at") or not re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+(?:[.-][0-9A-Za-z.-]+)?", tag):
        continue
    name = f"secretary-{tag}-{platform}-{arch}"
    assets = {asset.get("name") for asset in release.get("assets", []) if isinstance(asset, dict)}
    if {name + ".json", name + ".tar.gz"} <= assets:
        print(tag[1:])
        break
PY
)" || fail "Cannot parse published releases"
  else
    for ((index=0; index<30; index++)); do
      tag="$(plutil -extract "$index.tag_name" raw -o - "$stage/releases.json" 2>/dev/null)" || break
      [[ "$tag" =~ ^v[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || continue
      draft="$(plutil -extract "$index.draft" raw -o - "$stage/releases.json" 2>/dev/null)" || continue
      [[ "$draft" == false ]] || continue
      assets="$(plutil -extract "$index.assets" json -o - "$stage/releases.json" 2>/dev/null)" || continue
      name="secretary-$tag-$platform-$arch"
      if [[ "$assets" == *"$name.json"* && "$assets" == *"$name.tar.gz"* ]]; then
        version="${tag#v}"
        break
      fi
    done
  fi
  [[ -n "$version" ]] || fail "No published release with both $platform/$arch manifest and archive; set SECRETARY_VERSION"
fi

[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid SECRETARY_VERSION"
export SECRETARY_VERSION="$version"
case "$platform" in
  linux) installer=install-linux-binary.sh ;;
  darwin) installer=install-macos-binary.sh ;;
esac
url="https://raw.githubusercontent.com/GitHubFoxy/secretary/$ref/scripts/$installer"
curl -fsSL --retry 5 --retry-delay 2 --retry-all-errors -o "$stage/$installer" "$url" || fail "Cannot download $installer from $ref"
bash "$stage/$installer"
