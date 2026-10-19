#!/usr/bin/env bash
set -euo pipefail
umask 077
fail() { printf 'Secretary standalone Node: %s\n' "$*" >&2; exit 1; }
[[ "$(uname -s)" == Linux && "$(id -u)" -ne 0 ]] || fail "Run on Linux as an unprivileged user"
for command in curl python3 sha256sum tar mktemp readlink; do command -v "$command" >/dev/null || fail "Missing $command"; done
case "$(uname -m)" in x86_64) arch=amd64 ;; aarch64|arm64) arch=arm64 ;; *) fail "Unsupported architecture" ;; esac
state="$HOME/.local/share/secretary/standalone-node/node"
root="$HOME/.local/share/secretary/standalone-node"
releases="$root/releases"
current="$root/current"
unit="$HOME/.config/systemd/user/secretary-standalone-node.service"
service=false
harness=codex
harness_explicit=false
server=""
name=""
action=install
while (($#)); do
  case "$1" in
    install|repair|upgrade|start|stop|status|logs|doctor|enable-service|disable-service) action="$1" ;;
    --server|--name|--harness|--version)
      option="$1"; shift; (($#)) || fail "$option requires a value"
      case "$option" in --server) server="$1" ;; --name) name="$1" ;; --harness) harness="$1"; harness_explicit=true ;; --version) export SECRETARY_VERSION="$1" ;; esac ;;
    --enable-service) service=true ;;
    *) fail "Unsupported option: $1" ;;
  esac
  shift
done
[[ "$harness" == codex || "$harness" == claude_code ]] || fail "Choose codex or claude_code"
[[ ! -L "$root" && ! -L "$releases" && ! -L "$state" && ! -L "$state/data" && ! -L "$state/config.json" ]] || fail "Unsafe symlink in managed state"
config="$state/config.json"
credential="$state/enrollment.env"
[[ ! -L "$credential" ]] || fail "Unsafe credential symlink"
if [[ -f "$state/harness" ]]; then
  saved="$(cat "$state/harness")"
  [[ "$harness_explicit" != true || "$harness" == "$saved" ]] || fail "Installed harness conflicts with explicit selection"
  harness="$saved"
fi
binary="$current/bin/secretary-node"
unit_digest="$state/service-unit.sha256"
verify_unit() {
  [[ -f "$unit" && ! -L "$unit" && -f "$unit_digest" ]] || fail "Unmanaged systemd unit"
  [[ "$(sha256sum "$unit" | cut -d' ' -f1)" == "$(cat "$unit_digest")" ]] || fail "Systemd unit modified outside installer"
}
healthy_service() {
  local expected="$1" active pid actual
  for attempt in 1 2 3 4 5; do
    active="$(systemctl --user show secretary-standalone-node.service -p ActiveState --value 2>/dev/null || true)"
    pid="$(systemctl --user show secretary-standalone-node.service -p MainPID --value 2>/dev/null || true)"
    if [[ "$active" == active && "$pid" =~ ^[1-9][0-9]*$ && -e "/proc/$pid/exe" ]]; then
      actual="$(readlink -f "/proc/$pid/exe" 2>/dev/null || true)"
      [[ "$actual" == "$expected/bin/secretary-node" ]] && return 0
    fi
    sleep 1
  done
  return 1
}
service_available() { command -v systemctl >/dev/null 2>&1 && [[ -n "${XDG_RUNTIME_DIR:-}" && -S "$XDG_RUNTIME_DIR/bus" ]] && systemctl --user show-environment >/dev/null 2>&1; }
identity() {
  if [[ -f "$state/data/identity.json" ]]; then printf 'paired\n'; else printf 'awaiting explicit owner enrollment\n'; fi
}
version_status() {
  [[ ! -f "$current/VERSION" ]] || printf 'installed: version=%s revision=%s\n' "$(cat "$current/VERSION")" "$(cat "$current/REVISION")"
  if service_available && [[ -f "$unit" ]]; then
    pid="$(systemctl --user show secretary-standalone-node.service -p MainPID --value 2>/dev/null || true)"
    if [[ "$pid" =~ ^[1-9][0-9]*$ && -e "/proc/$pid/exe" ]]; then
      actual="$(readlink "/proc/$pid/exe" || true)"
      printf 'running: pid=%s executable=%s\n' "$pid" "$actual"
    else printf 'running: stopped\n'; fi
  else printf 'service: disabled or unavailable\n'; fi
}
case "$action" in
  status) version_status; [[ ! -f "$config" ]] || identity; exit 0 ;;
  logs) service_available && [[ -f "$unit" ]] || fail "systemd user service unavailable"; verify_unit; exec journalctl --user -u secretary-standalone-node.service -f --no-pager ;;
  start)
    [[ -x "$binary" && -f "$config" ]] || fail "Node not installed"
    if [[ -f "$unit" ]]; then service_available || fail "systemd user manager unavailable"; verify_unit; exec systemctl --user start secretary-standalone-node.service; fi
    [[ ! -f "$credential" ]] || { set -a; source "$credential"; set +a; }
    exec "$binary" --config "$config" ;;
  stop) service_available && [[ -f "$unit" ]] || fail "No managed service to stop"; verify_unit; exec systemctl --user stop secretary-standalone-node.service ;;
  disable-service) service_available || fail "systemd user manager unavailable"; [[ ! -f "$unit" ]] || { verify_unit; systemctl --user disable --now secretary-standalone-node.service; rm "$unit" "$unit_digest"; systemctl --user daemon-reload; }; exit 0 ;;
  doctor)
    [[ -x "$binary" && -f "$config" ]] || fail "Node not installed"
    if [[ "$harness" == codex ]]; then
      "$binary" --codex-pair-action=show --codex-pair-scope=node || fail "Codex inventory unavailable"
      "$binary" --codex-pair-action=readiness --codex-pair-scope=node || fail "Codex readiness unavailable"
    else
      command -v claude >/dev/null 2>&1 || fail "missing: Claude Code"
      claude --version || fail "Claude Code executable unavailable"
      claude auth status >/dev/null 2>&1 || fail "Claude Code authorization unavailable"
    fi
    version_status; identity; exit 0 ;;
  enable-service)
    service=true
    action=service-only ;;
esac
if [[ "$action" != service-only ]]; then
  [[ -z "$server" || "$server" =~ ^https://[^/[:space:]]+(/[^[:space:]]*)?$ ]] || fail "Remote Server must use private HTTPS"
  [[ -z "$name" || "$name" =~ ^[A-Za-z0-9._-]+$ ]] || fail "Invalid Node name"
  [[ -n "$server" || -f "$config" ]] || fail "First install requires --server private HTTPS URL"
  [[ -n "$name" || -f "$config" ]] || fail "First install requires --name"
  [[ -n "${SECRETARY_NODE_PAIRING_TOKEN:-}" || -f "$state/data/identity.json" ]] || fail "First enrollment requires owner-approved SECRETARY_NODE_PAIRING_TOKEN"
  temp="$(mktemp -d "${TMPDIR:-/tmp}/secretary-node.XXXXXXXX")"
  trap 'rm -rf "$temp"' EXIT
  version="${SECRETARY_VERSION:-}"
  if [[ -z "$version" ]]; then
    curl -fsSL --retry 2 -o "$temp/latest.json" https://api.github.com/repos/GitHubFoxy/secretary/releases/latest || fail "Cannot discover release"
    version="$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["tag_name"].removeprefix("v"))' "$temp/latest.json")" || fail "Invalid latest release"
  fi
  [[ "$version" =~ ^[0-9]+\.[0-9]+\.[0-9]+([.-][0-9A-Za-z.-]+)?$ ]] || fail "Invalid version"
  package="secretary-v$version-linux-$arch"
  url="${SECRETARY_RELEASE_BASE:-https://github.com/GitHubFoxy/secretary/releases/download/v$version}"
  curl -fsSL --retry 2 -o "$temp/manifest.json" "$url/$package.json" || fail "Missing manifest"
  [[ "${SECRETARY_MANIFEST_SHA256:-}" =~ ^[a-fA-F0-9]{64}$ ]] || fail "Set independently trusted SECRETARY_MANIFEST_SHA256"
  [[ "$(sha256sum "$temp/manifest.json" | cut -d' ' -f1)" == "${SECRETARY_MANIFEST_SHA256,,}" ]] || fail "Manifest trust anchor mismatch"
  curl -fsSL --retry 2 -o "$temp/$package.tar.gz" "$url/$package.tar.gz" || fail "Missing archive"
  python3 - "$temp/manifest.json" "$temp/$package.tar.gz" "$package" "$version" "$arch" "$temp" <<'PY'
import hashlib, json, pathlib, re, sys, tarfile
manifest, archive, package, version, arch, stage = sys.argv[1:]
m = json.loads(pathlib.Path(manifest).read_text())
if not (m.get("schema") == 1 and type(m.get("schema")) is int and m.get("version") == version and m.get("os") == "linux" and m.get("arch") == arch and m.get("archive") == package + ".tar.gz" and re.fullmatch("[a-fA-F0-9]{40}", m.get("revision", "")) and re.fullmatch("[a-fA-F0-9]{64}", m.get("sha256", "")) and type(m.get("bytes")) is int):
    sys.exit("Manifest mismatch")
data = pathlib.Path(archive).read_bytes()
if len(data) != m["bytes"] or hashlib.sha256(data).hexdigest() != m["sha256"].lower():
    sys.exit("Archive integrity mismatch")
with tarfile.open(archive, "r:gz") as tar:
    members = tar.getmembers()
    for member in members:
        path = pathlib.PurePosixPath(member.name)
        if not (member.name == package or member.name.startswith(package + "/")) or any(part in ("..", ".") for part in member.name.split("/")) or not (member.isfile() or member.isdir()):
            sys.exit("Unsafe archive member")
    tar.extractall(stage, filter="data")
payload = pathlib.Path(stage) / package
for filename in ["bin/secretary-node", "VERSION", "REVISION"]:
    if not (payload / filename).is_file():
        sys.exit("Incomplete Node artifact")
if (payload / "VERSION").read_text().strip() != version or (payload / "REVISION").read_text().strip() != m["revision"]:
    sys.exit("Provenance mismatch")
PY
  [[ -x "$temp/$package/bin/secretary-node" ]] || fail "Node binary is not executable"
  for path in "$root" "$releases"; do [[ ! -L "$path" ]] || fail "Managed directory is a symlink"; done
  mkdir -p "$releases" "$state/data"
  chmod 700 "$root" "$releases" "$state" "$state/data"
  target="$releases/$package"
  if [[ -e "$target" ]]; then
    [[ -d "$target" && ! -L "$target" ]] || fail "Release path conflict"
    cmp -s "$temp/manifest.json" "$target/RELEASE-MANIFEST.json" || fail "Installed manifest differs"
    cmp -s "$temp/$package/bin/secretary-node" "$target/bin/secretary-node" || fail "Installed binary differs"
  else
    staged="$(mktemp -d "$releases/.stage.XXXXXXXX")"
    mkdir -p "$staged/bin"
    cp "$temp/$package/bin/secretary-node" "$staged/bin/"
    cp "$temp/$package/VERSION" "$temp/$package/REVISION" "$staged/"
    cp "$temp/manifest.json" "$staged/RELEASE-MANIFEST.json"
    mv "$staged" "$target"
  fi
  if [[ ! -f "$config" ]]; then
    printf '%s\n' "$harness" > "$state/harness"
    chmod 600 "$state/harness"
    if [[ -n "${SECRETARY_NODE_PAIRING_TOKEN:-}" ]]; then
      [[ "$SECRETARY_NODE_PAIRING_TOKEN" =~ ^[A-Za-z0-9_-]+$ ]] || fail "Invalid enrollment token characters"
      printf 'SECRETARY_NODE_PAIRING_TOKEN=%s\n' "$SECRETARY_NODE_PAIRING_TOKEN" > "$credential"
      chmod 600 "$credential"
    fi
    python3 - "$config" "$server" "$name" "$state/data" <<'PYCONFIG'
import json, pathlib, sys
path, server, name, data = sys.argv[1:]
pathlib.Path(path).write_text(json.dumps({"server_url":server,"node":name,"data_dir":data,"capacity":1,"include_opencode":False,"standalone":True,"workspaces":[]})+"\n")
PYCONFIG
    "$target/bin/secretary-node" --edit-deployment-config --config "$config" --standalone || fail "Node configuration failed"
    chmod 600 "$config"
  elif [[ -n "$server" || -n "$name" ]]; then
    fail "Existing Node identity/config cannot be overwritten; preserve pairing and use explicit Node control"
  fi
  if [[ "$harness" == codex ]]; then
    "$target/bin/secretary-node" --codex-pair-action=install --codex-pair-scope=node || fail "Codex provisioning failed; current remains unchanged"
  elif ! command -v claude >/dev/null 2>&1; then
    printf 'missing: Claude Code; configure native harness before starting Node\n' >&2
  fi
  previous=""
  if [[ -e "$current" || -L "$current" ]]; then
    [[ -L "$current" ]] || fail "Unmanaged current pointer"
    previous="$(readlink "$current")"
    case "$previous" in "$releases/"*) ;; *) fail "Unexpected current target" ;; esac
  fi
  ln -s "$target" "$root/.current.$$"
  mv -Tf "$root/.current.$$" "$current"
  if service_available && [[ -f "$unit" ]]; then
    verify_unit
    if ! systemctl --user restart secretary-standalone-node.service || ! healthy_service "$target"; then
      rm -f "$current"; [[ -z "$previous" ]] || ln -s "$previous" "$current"
      if [[ -n "$previous" ]]; then systemctl --user restart secretary-standalone-node.service && healthy_service "$previous" || fail "Rollback health verification failed"; fi
      fail "Service restart failed, reverted version"
    fi
  fi
fi
if [[ "$service" == true ]]; then
  service_available || fail "No systemd user manager; run as a logged-in user or start manually"
  [[ -x "$binary" && -f "$config" ]] || fail "Install Node before enabling service"
  mkdir -p "$(dirname "$unit")"
  [[ ! -L "$unit" ]] || fail "Unsafe systemd unit symlink"
  if [[ -e "$unit" ]]; then
    verify_unit
  else
    python3 - "$unit" "$binary" "$config" "$HOME/.local/bin:$PATH" "$credential" <<'PY'
import pathlib, sys
unit, binary, config, path, credential = sys.argv[1:]
def quoted(value):
    return '"' + value.replace("%", "%%").replace("\\", "\\\\").replace('"', '\\"') + '"'
path = path.replace("\n", "").replace("\r", "")
text = "[Unit]\nDescription=Secretary standalone Execution Node\nAfter=network-online.target\n\n[Service]\nType=simple\nEnvironmentFile=-" + quoted(credential) + "\nEnvironment=" + quoted("PATH=" + path) + "\nExecStart=" + quoted(binary) + " --config " + quoted(config) + "\nRestart=on-failure\nRestartSec=3\n\n[Install]\nWantedBy=default.target\n"
pathlib.Path(unit).write_text(text)
PY
    chmod 600 "$unit"
    sha256sum "$unit" | cut -d\  -f1 > "$unit_digest"
    chmod 600 "$unit_digest"
  fi
  systemctl --user daemon-reload
  systemctl --user enable --now secretary-standalone-node.service
fi
version_status
identity
printf 'Node configured without a local Secretary Server. Pairing is performed outbound against the owner-approved remote Server.\n'
