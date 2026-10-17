#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

make_mise() {
  local directory="$1"
  cat > "$directory/mise" <<'STUB'
#!/usr/bin/env bash
set -euo pipefail
[[ "$1" == exec && "$2" == -- && "$3" == go && "$4" == build && "$5" == -o ]]
cat > "$6" <<'BINARY'
#!/usr/bin/env bash
set -euo pipefail
if [[ "${1:-}" == --check-codex-readiness ]]; then
  exit 0
fi
exit 0
BINARY
chmod 700 "$6"
STUB
  chmod +x "$directory/mise"
}

CODBIN="$TMP/codex-bin"
CLAUDEBIN="$TMP/claude-bin"
mkdir -p "$CODBIN" "$CLAUDEBIN"
make_mise "$CODBIN"
make_mise "$CLAUDEBIN"
cat > "$CODBIN/codex-acp" <<'STUB'
#!/usr/bin/env bash
exit 0
STUB
cat > "$CLAUDEBIN/claude" <<'STUB'
#!/usr/bin/env bash
if [[ "$*" == '--version' || "$*" == 'auth status' ]]; then exit 0; fi
exit 1
STUB
chmod +x "$CODBIN/codex-acp" "$CLAUDEBIN/claude"
cat > "$CODBIN/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$TEST_SYSTEMD_LOG"
exit 0
STUB
chmod +x "$CODBIN/systemctl"
cp "$CODBIN/systemctl" "$CLAUDEBIN/systemctl"

export HOME="$TMP/codex-home" PATH="$CODBIN:/usr/bin:/bin" TEST_SYSTEMD_LOG="$TMP/systemd.log"
mkdir -p "$HOME/.codex"
printf 'personal codex config\n' > "$HOME/.codex/config.toml"
"$ROOT/secretary" setup --non-interactive
STATE="$HOME/.local/share/secretary"
[[ -f "$STATE/environment" && -f "$STATE/node/config.json" ]] || fail "setup did not create private deployment"
grep -q '^harness = "codex"$' "$STATE/config.toml" || fail "Codex not selected"
grep -q '^default_harness = "codex"$' "$STATE/config.toml" || fail "Worker Codex default missing"
grep -q 'SECRETARY_NODE_PAIRING_TOKEN' "$STATE/environment" || fail "local Node pairing credential missing"
grep -q 'SECRETARY_NODE_ADMIN_TOKEN' "$STATE/environment" || fail "local Node admin credential missing"
[[ "$(stat -c '%a' "$STATE/environment" 2>/dev/null || stat -f '%Lp' "$STATE/environment")" == 600 ]] || fail "insecure private environment permissions"
[[ "$(cat "$HOME/.codex/config.toml")" == 'personal codex config' ]] || fail "native Codex config overwritten"
"$ROOT/secretary" doctor >/dev/null
"$ROOT/secretary" node doctor >/dev/null
before="$(cat "$STATE/environment")"
"$ROOT/secretary" setup --non-interactive >/dev/null
[[ "$(cat "$STATE/environment")" == "$before" ]] || fail "repeat setup rotated credentials"
if "$ROOT/secretary" config >"$TMP/config-log" 2>&1; then fail "config unexpectedly accepted without TTY"; fi
grep -q 'terminal' "$TMP/config-log" || fail "missing config TTY diagnostic"
"$ROOT/secretary" node setup --standalone --name test-node >/dev/null
grep -q '"standalone": true' "$STATE/node/config.json" || fail "standalone config lost"

"$ROOT/secretary" install-service
[[ -f "$HOME/.config/systemd/user/secretary.service" ]] || fail "Linux service unit missing"
grep -q 'enable --now secretary.service' "$TEST_SYSTEMD_LOG" || fail "systemd service not enabled"
"$ROOT/secretary" status | grep -q 'systemd' || fail "service status missing"
"$ROOT/secretary" restart
grep -q 'restart secretary.service' "$TEST_SYSTEMD_LOG" || fail "systemd restart not invoked"
mkdir -p "$STATE/node/data"
printf '{"node":"test-node"}\n' > "$STATE/node/data/identity.json"
"$ROOT/secretary" node install-service
[[ -f "$HOME/.config/systemd/user/secretary-node.service" ]] || fail "Linux Node unit missing"
"$ROOT/secretary" node status | grep -q 'systemd' || fail "Node service status missing"
"$ROOT/secretary" node restart
grep -q 'stop secretary-node.service' "$TEST_SYSTEMD_LOG" || fail "Node restart did not stop unit"
grep -q 'start secretary-node.service' "$TEST_SYSTEMD_LOG" || fail "Node restart did not start unit"
"$ROOT/secretary" node uninstall-service
"$ROOT/secretary" uninstall-service
[[ ! -e "$HOME/.config/systemd/user/secretary.service" ]] || fail "service unit retained after uninstall"

export HOME="$TMP/claude-home" PATH="$CLAUDEBIN:/usr/bin:/bin"
"$ROOT/secretary" setup --non-interactive
STATE="$HOME/.local/share/secretary"
grep -q '^harness = "claude_code"$' "$STATE/config.toml" || fail "Claude-only setup selected Codex"
grep -q '^default_harness = "claude_code"$' "$STATE/config.toml" || fail "Claude worker default missing"
"$ROOT/secretary" doctor >/dev/null
"$ROOT/secretary" node doctor >/dev/null
before="$(cat "$STATE/config.toml")"
if "$ROOT/secretary" setup --non-interactive --harness unsupported >/dev/null 2>&1; then fail "invalid harness accepted"; fi
[[ "$(cat "$STATE/config.toml")" == "$before" ]] || fail "invalid harness modified config"
printf 'PASS: clean Codex and Claude-only terminal setup, protected state, non-TTY failures and Linux service lifecycle\n'
