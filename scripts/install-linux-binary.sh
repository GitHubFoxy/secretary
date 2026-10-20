#!/usr/bin/env bash
set -euo pipefail
umask 077
fail() { printf 'Secretary Linux install: %s
' "$*" >&2; exit 1; }
[[ "$(uname -s)" == Linux ]] || fail "Only Linux is supported"
for utility in curl tar sha256sum python3 zsh openssl systemctl mktemp readlink cmp diff grep; do
  command -v "$utility" >/dev/null 2>&1 || fail "Missing required utility: $utility"
done
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail "Unsupported Linux architecture" ;; esac
[[ "$(id -u)" -ne 0 ]] || fail "Run as a standard user, not root"
# Let a clean user explicitly select an already working harness instead of
# automatically downloading the pinned native Codex pair on every host.
selected_harness="${SECRETARY_INSTALL_HARNESS:-}"
selected_codex_cli="${SECRETARY_INSTALL_CODEX_CLI:-}"
selected_codex_acp="${SECRETARY_INSTALL_CODEX_ACP:-}"
case "$selected_harness" in
  ""|codex|claude_code) ;;
  *) fail "Invalid SECRETARY_INSTALL_HARNESS (use codex or claude_code)" ;;
esac
if [[ -n "$selected_codex_cli" || -n "$selected_codex_acp" ]]; then
  [[ -n "$selected_codex_cli" && -n "$selected_codex_acp" ]] || fail "Specify both SECRETARY_INSTALL_CODEX_CLI and SECRETARY_INSTALL_CODEX_ACP"
  [[ "$selected_harness" != claude_code ]] || fail "Explicit Codex pair cannot be used with claude_code harness"
  [[ "$selected_codex_cli" == /* && "$selected_codex_acp" == /* ]] || fail "Codex pair paths must be absolute"
  [[ -f "$selected_codex_cli" && -x "$selected_codex_cli" && -f "$selected_codex_acp" && -x "$selected_codex_acp" ]] || fail "Selected Codex pair executables are missing"
  selected_harness=codex
fi
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
for tool in ffmpeg ffprobe; do
  [[ -x "$payload/bin/video-tools/$tool" && ! -L "$payload/bin/video-tools/$tool" ]] || fail "Missing Server video tool: $tool"
done
[[ -f "$payload/VIDEO-TOOLS.json" && ! -L "$payload/VIDEO-TOOLS.json" ]] || fail "Missing video tool manifest"
python3 - "$payload" <<'PY'
import hashlib, json, pathlib, subprocess, sys
root = pathlib.Path(sys.argv[1])
m = json.loads((root / "VIDEO-TOOLS.json").read_text())
assert m.get("origin"), "Missing video tool provenance"
versions = []
for tool in ("ffmpeg", "ffprobe"):
    path = root / "bin" / "video-tools" / tool
    assert hashlib.sha256(path.read_bytes()).hexdigest() == m[tool]["sha256"], tool + " checksum mismatch"
    words = subprocess.check_output([str(path), "-version"], timeout=10).decode().splitlines()[0].split()
    assert words[:2] == [tool, "version"] and words[2] == m[tool]["version"], tool + " version mismatch"
    versions.append(words[2])
assert versions[0] == versions[1], "Video pair incompatible"
PY
[[ -f "$payload/secretary" && -f "$payload/scripts/secretary-interactive.zsh" && -f "$payload/internal/config/defaults/config.toml" ]] || fail "Incomplete CLI package"
[[ "$(cat "$payload/VERSION")" == "$version" && "$(cat "$payload/REVISION")" == "$revision" ]] || fail "Release provenance mismatch"
root="$HOME/.local/share/secretary"
releases="$root/releases"
current="$root/current"
public_bin="$HOME/.local/bin"
bin="$public_bin"
units="$HOME/.config/systemd/user"
for directory in "$root" "$releases" "$public_bin" "$units"; do
  [[ ! -L "$directory" ]] || fail "Unsafe symlink installation directory"
done
mkdir -p "$releases" "$public_bin" "$units"
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
  path="$public_bin/$executable"
  expected="$current/bin/$executable"
  [[ "$executable" != secretary ]] || expected="$current/secretary"
  if [[ -e "$path" || -L "$path" ]]; then
    if [[ ! -L "$path" || "$(readlink "$path")" != "$expected" ]]; then
      bin="$root/bin"
      break
    fi
  fi
done
if [[ "$bin" != "$public_bin" ]]; then
  [[ ! -L "$bin" ]] || fail "Unsafe private executable directory"
  mkdir -p "$bin"
fi
for executable in secretary secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  path="$bin/$executable"
  expected="$current/bin/$executable"
  [[ "$executable" != secretary ]] || expected="$current/secretary"
  if [[ -e "$path" || -L "$path" ]]; then
    [[ -L "$path" && "$(readlink "$path")" == "$expected" ]] || fail "Unmanaged release executable: $path"
  fi
done
service_path="$bin:$PATH"
unit_fields="$(SECRETARY_UNIT_CURRENT="$current" SECRETARY_UNIT_PATH="$service_path" SECRETARY_UNIT_BIN="$bin" SECRETARY_UNIT_PUBLIC_BIN="$public_bin" python3 - <<'PY'
import os
current = os.environ["SECRETARY_UNIT_CURRENT"]
path = os.environ["SECRETARY_UNIT_PATH"]
if any(c in current + path for c in "\r\n"):
  raise SystemExit("Unsafe systemd unit environment")
def spec(value):
  return value.replace("%", "%%")
def quote(value):
  return '"' + spec(value).replace("\\", "\\\\").replace('"', '\\"') + '"'
print("WorkingDirectory=" + spec(current).replace(" ", r"\x20"))
print("Environment=" + quote("PATH=" + path))
if os.environ["SECRETARY_UNIT_BIN"] != os.environ["SECRETARY_UNIT_PUBLIC_BIN"]:
  print("Environment=" + quote("SECRETARY_BINARY_STAGE_BIN=" + os.environ["SECRETARY_UNIT_BIN"]))
print("ExecStart=" + quote(current + "/secretary"))
PY
)" || fail "Cannot escape systemd unit values"
working_field="$(printf '%s\n' "$unit_fields" | grep '^WorkingDirectory=')"
path_field="$(printf '%s\n' "$unit_fields" | grep '^Environment="PATH=')"
bin_field="$(printf '%s\n' "$unit_fields" | grep '^Environment="SECRETARY_BINARY_STAGE_BIN=' || true)"
environment_fields="$path_field"
if [[ -n "$bin_field" ]]; then
  environment_fields="$(printf '%s\n%s' "$path_field" "$bin_field")"
fi
executable_field="$(printf '%s\n' "$unit_fields" | grep '^ExecStart=')"
for unit in secretary.service secretary-node.service; do
  path="$units/$unit"
  [[ ! -L "$path" ]] || fail "Unmanaged systemd unit symlink: $unit"
  if [[ -e "$path" ]]; then
    if [[ "$unit" == secretary.service ]]; then
      description="Secretary Server"
      exec_command="$executable_field serve"
    else
      description="Secretary co-located Execution Node"
      exec_command="$executable_field node serve"
    fi
    grep -Fqx "Description=$description" "$path" &&
      grep -Fqx "$working_field" "$path" &&
      grep -Fqx "ExecStart=$exec_command" "$path" ||
      fail "Conflicting systemd unit: $unit; remove or migrate the source-managed service before binary installation"
    if [[ -n "$bin_field" ]]; then
      grep -Fqx "$bin_field" "$path" || fail "Conflicting systemd release bin: $unit"
    fi
  fi
done
mkdir -p "$stage/setup-bin"
for executable in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  ln -s "$target/bin/$executable" "$stage/setup-bin/$executable"
done
setup_args=(setup --non-interactive)
[[ -z "$selected_harness" ]] || setup_args+=(--harness "$selected_harness")
if ! SECRETARY_BINARY_PREINSTALLED_ONLY=1 \
    SECRETARY_BINARY_STAGE_BIN="$stage/setup-bin" \
    SECRETARY_CODEX_PAIR_CLI="$selected_codex_cli" \
    SECRETARY_CODEX_PAIR_ACP="$selected_codex_acp" \
    "$target/secretary" "${setup_args[@]}"; then
  fail "Setup failed before release activation. See the native Codex/ACP readiness error above. A model-cache schema failure, ACP timeout or protocol error is not proof of missing authorization. No release was activated."
fi
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
    if [[ -z "$previous" ]]; then
      systemctl --user disable --now secretary-node.service secretary.service >/dev/null 2>&1 || true
    fi
    for path in "${unit_created[@]}"; do rm -f "$path"; done
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
      exec_command="$executable_field serve"
      description="Secretary Server"
      after="network-online.target"
    else
      exec_command="$executable_field node serve"
      description="Secretary co-located Execution Node"
      after="network-online.target secretary.service"
    fi
    cat > "$units/$unit" <<EOF
[Unit]
Description=$description
After=$after

[Service]
Type=simple
${working_field}
${environment_fields}
ExecStart=${exec_command}
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
  for attempt in 1 2 3 4 5 6 7 8 9 10; do
    ready=true
    for service in secretary.service secretary-node.service; do
      state="$(systemctl --user show "$service" -p ActiveState --value)" || fail "Cannot inspect $service ActiveState"
      pid="$(systemctl --user show "$service" -p MainPID --value)" || fail "Cannot inspect $service MainPID"
      if [[ "$state" != active || ! "$pid" =~ ^[1-9][0-9]*$ ]] || ! kill -0 "$pid" 2>/dev/null; then
        ready=false
      fi
    done
    health="$(curl -sS --max-time 2 -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/v1/web/session 2>/dev/null)" || health=000
    if [[ "$ready" == true && ( "$health" == 200 || "$health" == 401 ) ]]; then break; fi
    [[ "$attempt" != 10 ]] || fail "Server/Node failed service health verification after activation"
    sleep 1
  done
fi
activated=true
printf 'Secretary %s (%s) installed for linux/%s. Inspect: systemctl --user status secretary.service secretary-node.service; secretary status
' "$version" "$revision" "$arch"
\n'"$bin_field"
executable_field="$(printf '%s\n' "$unit_fields" | grep '^ExecStart=')"
for unit in secretary.service secretary-node.service; do
  path="$units/$unit"
  [[ ! -L "$path" ]] || fail "Unmanaged systemd unit symlink: $unit"
  if [[ -e "$path" ]]; then
    if [[ "$unit" == secretary.service ]]; then
      description="Secretary Server"
      exec_command="$executable_field serve"
    else
      description="Secretary co-located Execution Node"
      exec_command="$executable_field node serve"
    fi
    grep -Fqx "Description=$description" "$path" &&
      grep -Fqx "$working_field" "$path" &&
      grep -Fqx "ExecStart=$exec_command" "$path" ||
      fail "Conflicting systemd unit: $unit; remove or migrate the source-managed service before binary installation"
  fi
done
mkdir -p "$stage/setup-bin"
for executable in secretaryd secretaryctl secretary-mcp secretary-migrate secretary-node; do
  ln -s "$target/bin/$executable" "$stage/setup-bin/$executable"
done
setup_args=(setup --non-interactive)
[[ -z "$selected_harness" ]] || setup_args+=(--harness "$selected_harness")
if ! SECRETARY_BINARY_PREINSTALLED_ONLY=1 \
    SECRETARY_BINARY_STAGE_BIN="$stage/setup-bin" \
    SECRETARY_CODEX_PAIR_CLI="$selected_codex_cli" \
    SECRETARY_CODEX_PAIR_ACP="$selected_codex_acp" \
    "$target/secretary" "${setup_args[@]}"; then
  fail "Setup failed before release activation. See the native Codex/ACP readiness error above. A model-cache schema failure, ACP timeout or protocol error is not proof of missing authorization. No release was activated."
fi
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
    if [[ -z "$previous" ]]; then
      systemctl --user disable --now secretary-node.service secretary.service >/dev/null 2>&1 || true
    fi
    for path in "${unit_created[@]}"; do rm -f "$path"; done
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
      exec_command="$executable_field serve"
      description="Secretary Server"
      after="network-online.target"
    else
      exec_command="$executable_field node serve"
      description="Secretary co-located Execution Node"
      after="network-online.target secretary.service"
    fi
    cat > "$units/$unit" <<EOF
[Unit]
Description=$description
After=$after

[Service]
Type=simple
${working_field}
${path_field}
ExecStart=${exec_command}
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
  for attempt in 1 2 3 4 5 6 7 8 9 10; do
    ready=true
    for service in secretary.service secretary-node.service; do
      state="$(systemctl --user show "$service" -p ActiveState --value)" || fail "Cannot inspect $service ActiveState"
      pid="$(systemctl --user show "$service" -p MainPID --value)" || fail "Cannot inspect $service MainPID"
      if [[ "$state" != active || ! "$pid" =~ ^[1-9][0-9]*$ ]] || ! kill -0 "$pid" 2>/dev/null; then
        ready=false
      fi
    done
    health="$(curl -sS --max-time 2 -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/v1/web/session 2>/dev/null)" || health=000
    if [[ "$ready" == true && ( "$health" == 200 || "$health" == 401 ) ]]; then break; fi
    [[ "$attempt" != 10 ]] || fail "Server/Node failed service health verification after activation"
    sleep 1
  done
fi
activated=true
printf 'Secretary %s (%s) installed for linux/%s. Inspect: systemctl --user status secretary.service secretary-node.service\n' "$version" "$revision" "$arch"
if [[ "$bin" != "$public_bin" ]]; then
  printf 'Existing executables in %s preserved. Release CLI: SECRETARY_BINARY_STAGE_BIN=%q %q status\n' "$public_bin" "$bin" "$current/secretary"
else
  printf 'Release CLI: secretary status\n'
fi
