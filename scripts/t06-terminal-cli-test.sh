#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TMP="$(mktemp -d)"
trap 'rm -rf "$TMP"' EXIT
export T06_NODE_BINARY="$TMP/secretary-node-real"
(cd "$ROOT" && go build -o "$T06_NODE_BINARY" ./cmd/secretary-node)

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
if [[ "$0" == */secretary-node ]]; then
  exec "$T06_NODE_BINARY" "$@"
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
if [[ "$*" == '--version' ]]; then exit 0; fi
if [[ "$*" == 'auth status' ]]; then [[ "${CLAUDE_AUTH_FAIL:-0}" != 1 ]]; exit $?; fi
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
cp "$CLAUDEBIN/claude" "$CODBIN/claude"
before_auth="$(cat "$STATE/config.toml")"
export CLAUDE_AUTH_FAIL=1
if "$ROOT/secretary" setup --non-interactive --harness claude_code >"$TMP/denied-auth-log" 2>&1; then
  fail "setup switched harness when Claude auth status failed"
fi
[[ "$(cat "$STATE/config.toml")" == "$before_auth" ]] || fail "failed Claude auth modified the original Codex config"
grep -q '^default_harness = "codex"$' "$STATE/config.toml" || fail "failed auth switched worker defaults"
"$ROOT/secretary" doctor >/dev/null || fail "Codex doctor stopped working after denied Claude auth"
unset CLAUDE_AUTH_FAIL
"$ROOT/secretary" setup --non-interactive --harness claude_code >/dev/null
grep -q '^harness = "claude_code"$' "$STATE/config.toml" || fail "authorized Claude switch failed"
"$ROOT/secretary" setup --non-interactive --harness codex >/dev/null
before="$(cat "$STATE/environment")"
"$ROOT/secretary" setup --non-interactive >/dev/null
[[ "$(cat "$STATE/environment")" == "$before" ]] || fail "repeat setup rotated credentials"
if "$ROOT/secretary" config >"$TMP/config-log" 2>&1; then fail "config unexpectedly accepted without TTY"; fi
grep -q 'terminal' "$TMP/config-log" || fail "missing config TTY diagnostic"
if command -v script >/dev/null 2>&1 && [[ "$(uname -s)" == Linux ]]; then
  if ! printf '2\n\nwizard-node\n2\n5\n' | script -qec "$ROOT/secretary config" /dev/null >"$TMP/pty-log" 2>&1; then
    cat "$TMP/pty-log" >&2
    fail "interactive terminal wizard failed"
  fi
  grep -q '"node": "wizard-node"' "$STATE/node/config.json" || fail "interactive Node name not applied"
  grep -q '"capacity": 2' "$STATE/node/config.json" || fail "interactive Node capacity not applied"
fi
"$ROOT/secretary" node setup --standalone --name test-node >/dev/null
grep -q '"standalone": true' "$STATE/node/config.json" || fail "standalone config lost"
"$ROOT/secretary" node setup --workspace frontend=/src/frontend --workspace backend=/src/backend >/dev/null
"$ROOT/secretary" node setup --capacity 2 >/dev/null
grep -q '"capacity": 2' "$STATE/node/config.json" || fail "node capacity was not updated"
grep -q '"project_id": "frontend"' "$STATE/node/config.json" || fail "frontend mapping dropped by capacity edit"
grep -q '"project_id": "backend"' "$STATE/node/config.json" || fail "backend mapping dropped by capacity edit"
"$ROOT/secretary" node setup --workspace frontend=/new/frontend --workspace docs=/src/docs >/dev/null
grep -q '"path": "/new/frontend"' "$STATE/node/config.json" || fail "explicit workspace update failed"
grep -q '"project_id": "backend"' "$STATE/node/config.json" || fail "unmodified workspace disappeared"
grep -q '"project_id": "docs"' "$STATE/node/config.json" || fail "second explicit workspace missing"
before_node="$(cat "$STATE/node/config.json")"
if "$ROOT/secretary" node setup --capacity -1 >/dev/null 2>&1; then fail "invalid capacity was accepted"; fi
[[ "$(cat "$STATE/node/config.json")" == "$before_node" ]] || fail "invalid capacity altered Node config"
"$ROOT/secretary" node setup --clear-workspaces >/dev/null
if grep -q '"project_id"' "$STATE/node/config.json"; then fail "explicit workspace clear was ignored"; fi

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
