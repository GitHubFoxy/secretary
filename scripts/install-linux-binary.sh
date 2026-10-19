#!/usr/bin/env bash
set -euo pipefail
umask 077
fail() { printf 'Secretary Linux install: %s\n' "$*" >&2; exit 1; }
[[ "$(uname -s)" == Linux ]] || fail "Only Linux is supported"
for utility in curl tar sha256sum python3 zsh openssl systemctl mktemp readlink cmp diff; do
  command -v "$utility" >/dev/null 2>&1 || fail "Missing required utility: $utility"
done
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail "Unsupported Linux architecture" ;; esac
[[ "$(id -u)" -ne 0 ]] || fail "Run as a standard user, not root"
[[ -n "${XDG_RUNTIME_DIR:-}" && -S "${XDG_RUNTIME_DIR}/bus" ]] || fail "No active systemd user manager; start a real user session"
systemctl --user show-environment >/dev/null 2>&1 || fail "systemd --user unavailable; no services installed"
stage="$(mktemp -d "${TMPDIR:-/tmp}/secretary-linux.XXXXXXXX")"
trap 'rm -rf "$stage"' EXIT
version="${SECRETARY_VERSION:-}"
if [[ -z "$version" ]]; then
  curl -fsSL --retry 2 -o "$stage/latest.json" https://api.github.com/repos/GitHubFoxy/secretary/releases/latest || fail "Cannot resolve latest release; set SECRETARY_VERSION"
  version="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["tag_name"])' "$stage/latest.json")"
  version="${version#v}"
fi
[[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid release version"
name="secretary-v$version-linux-$arch"
url="${SECRETARY_RELEASE_BASE:-https://github.com/GitHubFoxy/secretary/releases/download/v$version}"
curl -fsSL --retry 2 -o "$stage/manifest.json" "$url/$name.json" || fail "Missing Linux manifest"
python3 - "$stage/manifest.json" "$version" "$arch" "$name" > "$stage/digest" <<'PY'
import json, re, sys
with open(sys.argv[1], encoding="utf-8") as f:
    m = json.load(f)
version, arch, name = sys.argv[2:]
assert isinstance(m, dict)
assert m.get("schema") == 1 and type(m.get("schema")) is int
assert m.get("version") == version and m.get("os") == "linux" and m.get("arch") == arch
assert m.get("archive") == name + ".tar.gz"
assert re.fullmatch(r"[a-fA-F0-9]{40}", m.get("revision", "")) 
assert re.fullmatch(r"[a-fA-F0-9]{64}", m.get("sha256", ""))
assert type(m.get("bytes")) is int and m["bytes"] > 0
print(m["sha256"].lower(), m["bytes"], m["revision"])
PY
read -r digest bytes revision < "$stage/digest"
curl -fsSL --retry 2 -o "$stage/$name.tar.gz" "$url/$name.tar.gz" || fail "Release archive download failed"
[[ "$(sha256sum "$stage/$name.tar.gz" | awk '{print $1}')" == "$digest" ]] || fail "Checksum mismatch; installed release unchanged"
[[ "$(wc -c < "$stage/$name.tar.gz" | tr -d '[:space:]')" == "$bytes" ]] || fail "Archive length mismatch"
mkdir -p "$stage/unpacked"
python3 - "$stage/$name.tar.gz" "$name" <<'PY'
import sys, tarfile
with tarfile.open(sys.argv[1], "r:gz") as t:
    prefix = sys.argv[2]
    for m in t.getmembers():
        parts = m.name.split("/")
        if not (m.name == prefix or m.name.startswith(prefix + "/")) or any(p in (".", "..", "") for p in parts) or not (m.isdir() or m.isfile()):
            raise SystemExit("Unsafe release archive member: " + m.name)
PY
tar -xzf "$stage/$name.tar.gz" -C "$stage/unpacked"
payload="$stage/unpacked/$name"
for binary in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  [[ -x "$payload/bin/$binary" && ! -L "$payload/bin/$binary" ]] || fail "Missing release binary: $binary"
done
[[ -f "$payload/secretary" && -f "$payload/scripts/secretary-interactive.zsh" && -f "$payload/internal/config/defaults/config.toml" ]] || fail "Incomplete CLI package"
[[ "$(cat "$payload/VERSION")" == "$version" && "$(cat "$payload/REVISION")" == "$revision" ]] || fail "Release provenance mismatch"
root="$HOME/.local/share/secretary"
releases="$root/releases"
current="$root/current"
bin="$HOME/.local/bin"
units="$HOME/.config/systemd/user"
for directory in "$root" "$releases" "$bin" "$units"; do
  [[ ! -L "$directory" ]] || fail "Unsafe symlink installation directory"
done
mkdir -p "$releases" "$bin" "$units"
target="$releases/$name"
if [[ -e "$target" ]]; then
  [[ -d "$target" && ! -L "$target" ]] || fail "Release directory conflict"
  cmp -s "$stage/manifest.json" "$target/RELEASE-MANIFEST.json" || fail "Cached manifest differs"
  diff -qr -x RELEASE-MANIFEST.json "$payload" "$target" >/dev/null || fail "Cached release is altered"
else
  temp="$(mktemp -d "$releases/.release.XXXXXXXX")"
  trap 'rm -rf "$temp" "$stage"' EXIT
  cp -R "$payload" "$temp/$name"
  cp "$stage/manifest.json" "$temp/$name/RELEASE-MANIFEST.json"
  cmp -s "$stage/manifest.json" "$temp/$name/RELEASE-MANIFEST.json" || fail "Staging failed"
  mv "$temp/$name" "$target"
  rmdir "$temp"
  trap 'rm -rf "$stage"' EXIT
fi
previous=""
if [[ -e "$current" || -L "$current" ]]; then
  [[ -L "$current" ]] || fail "Unmanaged release pointer"
  previous="$(readlink "$current")"
  case "$previous" in "$releases/"*) ;; *) fail "Unmanaged release pointer" ;; esac
fi
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$bin/$executable"
  if [[ -e "$path" || -L "$path" ]]; then
    [[ -L "$path" ]] || fail "Unmanaged executable: $path"
    case "$(readlink "$path")" in "$current/"*) ;; *) fail "Source-managed executable exists: $path" ;; esac
  fi
done
for unit in secretary.service secretary-node.service; do
  if [[ -e "$units/$unit" ]]; then
    grep -Fqx "WorkingDirectory=$current" "$units/$unit" || fail "Unmanaged systemd unit: $unit"
  fi
done
mkdir -p "$stage/setup-bin"
for executable in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  ln -s "$target/bin/$executable" "$stage/setup-bin/$executable"
done
SECRETARY_BINARY_PREINSTALLED_ONLY=1 SECRETARY_BINARY_STAGE_BIN="$stage/setup-bin" "$target/secretary" setup --non-interactive || fail "Setup failed before release activation"
created=()
unit_created=()
activated=false
rollback() {
  if [[ "$activated" != true ]]; then
    if [[ -L "$current" && "$(readlink "$current")" == "$target" ]]; then
      rm -f "$current"
      [[ -z "$previous" ]] || ln -s "$previous" "$current"
    fi
    for path in "${created[@]}"; do rm -f "$path"; done
    if [[ -z "$previous" ]]; then\n      systemctl --user disable --now secretary-node.service secretary.service >/dev/null 2>&1 || true\n    fi\n    for path in "${unit_created[@]}"; do rm -f "$path"; done
    systemctl --user daemon-reload >/dev/null 2>&1 || true
  fi
}
trap 'rollback; rm -rf "$stage"' EXIT
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$bin/$executable"
  if [[ ! -L "$path" ]]; then
    relative="bin/$executable"
    [[ "$executable" != secretary ]] || relative=secretary
    ln -s "$current/$relative" "$path"
    created+=("$path")
  fi
done
pointer="$root/.current.$$.new"
trap 'rm -f "$pointer"; rollback; rm -rf "$stage"' EXIT
ln -s "$target" "$pointer"
mv -Tf "$pointer" "$current"
if [[ -z "$previous" ]]; then
  for unit in secretary.service secretary-node.service; do
    [[ ! -e "$units/$unit" ]] || continue
    if [[ "$unit" == secretary.service ]]; then
      exec_command="$current/secretary serve"
      description="Secretary Server"
      after="network-online.target"
    else
      exec_command="$current/secretary node serve"
      description="Secretary co-located Execution Node"
      after="network-online.target secretary.service"
    fi
    cat > "$units/$unit" <<EOF
[Unit]
Description=$description
After=$after

[Service]
Type=simple
WorkingDirectory=$current
ExecStart=$exec_command
Restart=on-failure
RestartSec=3

[Install]
WantedBy=default.target
EOF
    chmod 600 "$units/$unit"
    unit_created+=("$units/$unit")
  done
  systemctl --user daemon-reload || fail "Cannot register systemd units"
  systemctl --user enable --now secretary.service || fail "Cannot activate Server service"
  systemctl --user enable --now secretary-node.service || fail "Cannot activate Node service"
fi
activated=true
printf 'Secretary %s (%s) installed for linux/%s. Inspect: systemctl --user status secretary.service secretary-node.service; secretary status\n' "$version" "$revision" "$arch"
