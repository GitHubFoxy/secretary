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
  if [[ "${T06_REQUIRE_CODEX_ACP:-0}" == 1 ]]; then
    command -v codex-acp >/dev/null 2>&1 || exit 1
  fi
  [[ "${T06_CODEX_AUTH_FAIL:-0}" != 1 ]]
  exit $?
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
#!/usr/bin/env python3
import json, sys
if sys.argv[1:] == ['--version']:
    print('codex-acp 0.16.0')
    sys.exit(0)
for line in sys.stdin:
    request=json.loads(line)
    method=request.get('method')
    if method=='initialize':
        result={'protocolVersion':1,'agentInfo':{'version':'0.16.0'},'agentCapabilities':{'loadSession':True},'_meta':{'steering':{'supported':True}}}
    elif method=='session/new':
        result={'sessionId':'t06-session','models':{'availableModels':[{'modelId':'gpt-fixture'}]}}
    else:
        result={}
    print(json.dumps({'jsonrpc':'2.0','id':request.get('id'),'result':result}),flush=True)
STUB
cat > "$CODBIN/codex" <<'STUB'
#!/usr/bin/env bash
if [[ "$*" == '--version' ]]; then echo 'codex-cli 0.163.0'; exit 0; fi
if [[ "$*" == 'login status' ]]; then
  [[ "${T06_CODEX_AUTH_FAIL:-0}" != 1 ]]
  exit $?
fi
if [[ "$*" == 'login' ]]; then exit 0; fi
exit 2
STUB
cat > "$CLAUDEBIN/claude" <<'STUB'
#!/usr/bin/env bash
if [[ "$*" == '--version' ]]; then exit 0; fi
if [[ "$*" == 'auth status' ]]; then [[ "${CLAUDE_AUTH_FAIL:-0}" != 1 ]]; exit $?; fi
exit 1
STUB
chmod +x "$CODBIN/codex-acp" "$CODBIN/codex" "$CLAUDEBIN/claude"
cat > "$CODBIN/systemctl" <<'STUB'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$TEST_SYSTEMD_LOG"
exit 0
STUB
chmod +x "$CODBIN/systemctl"
cp "$CODBIN/systemctl" "$CLAUDEBIN/systemctl"

adopt_codex_t06_pair() {
  "$T06_NODE_BINARY" --codex-pair-action=install --codex-pair-scope=server --codex-pair-cli="$CODBIN/codex" --codex-pair-acp="$CODBIN/codex-acp" >/dev/null
  "$T06_NODE_BINARY" --codex-pair-action=install --codex-pair-scope=node --codex-pair-cli="$CODBIN/codex" --codex-pair-acp="$CODBIN/codex-acp" >/dev/null
}

export HOME="$TMP/codex-home" PATH="$CODBIN:/usr/bin:/bin" TEST_SYSTEMD_LOG="$TMP/systemd.log"
adopt_codex_t06_pair
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

# Existing config.toml or a previously paired identity must not suppress
# recovery of missing Node secrets on upgrade or an interrupted prior setup.
export HOME="$TMP/upgrade-home" PATH="$CODBIN:/usr/bin:/bin"
UPGRADE_STATE="$HOME/.local/share/secretary"
mkdir -p "$UPGRADE_STATE/node/data"
cp "$ROOT/internal/config/defaults/config.toml" "$UPGRADE_STATE/config.toml"
printf "export SECRETARY_BOOTSTRAP_TOKEN='existing-bootstrap'\nexport SECRETARY_CUSTOM_VALUE='keep-me'\n" > "$UPGRADE_STATE/environment"
printf '{"node":"legacy-paired"}\n' > "$UPGRADE_STATE/node/data/identity.json"
export SECRETARY_NODE_PAIRING_TOKEN="ambient-not-persisted"
export SECRETARY_NODE_ADMIN_TOKEN="ambient-not-persisted"
adopt_codex_t06_pair
before_upgrade_config="$(cat "$UPGRADE_STATE/config.toml")"
"$ROOT/secretary" setup --non-interactive >/dev/null
[[ "$(cat "$UPGRADE_STATE/config.toml")" == "$before_upgrade_config" ]] || fail "upgrade rewrote original config"
grep -q '^export SECRETARY_BOOTSTRAP_TOKEN=' "$UPGRADE_STATE/environment" || fail "upgrade lost bootstrap secret"
grep -q "existing-bootstrap" "$UPGRADE_STATE/environment" || fail "upgrade rotated bootstrap"
grep -q "keep-me" "$UPGRADE_STATE/environment" || fail "upgrade modified unrelated environment"
grep -q '^export SECRETARY_NODE_PAIRING_TOKEN=' "$UPGRADE_STATE/environment" || fail "upgrade did not create pairing token"
grep -q '^export SECRETARY_NODE_ADMIN_TOKEN=' "$UPGRADE_STATE/environment" || fail "upgrade did not create admin token"
if grep -q 'ambient-not-persisted' "$UPGRADE_STATE/environment"; then fail "inherited environment was treated as persisted credentials"; fi
before_upgrade_env="$(cat "$UPGRADE_STATE/environment")"
"$ROOT/secretary" setup --non-interactive >/dev/null
[[ "$(cat "$UPGRADE_STATE/environment")" == "$before_upgrade_env" ]] || fail "repeat upgrade rotated existing secrets"

# A partial setup or manually incomplete environment is repaired one field
# at a time, without rewriting valid credentials.
grep -v '^export SECRETARY_NODE_ADMIN_TOKEN=' "$UPGRADE_STATE/environment" > "$TMP/retry-env"
cp "$TMP/retry-env" "$UPGRADE_STATE/environment"
pair_before="$(grep '^export SECRETARY_NODE_PAIRING_TOKEN=' "$UPGRADE_STATE/environment")"
"$ROOT/secretary" setup --non-interactive >/dev/null
[[ "$(grep '^export SECRETARY_NODE_PAIRING_TOKEN=' "$UPGRADE_STATE/environment")" == "$pair_before" ]] || fail "retry rotated existing pairing token"
grep -q '^export SECRETARY_NODE_ADMIN_TOKEN=' "$UPGRADE_STATE/environment" || fail "retry did not repair missing admin token"
admin_before="$(grep '^export SECRETARY_NODE_ADMIN_TOKEN=' "$UPGRADE_STATE/environment")"
grep -v '^export SECRETARY_NODE_PAIRING_TOKEN=' "$UPGRADE_STATE/environment" > "$TMP/retry-env"
cp "$TMP/retry-env" "$UPGRADE_STATE/environment"
"$ROOT/secretary" setup --non-interactive >/dev/null
[[ "$(grep '^export SECRETARY_NODE_ADMIN_TOKEN=' "$UPGRADE_STATE/environment")" == "$admin_before" ]] || fail "retry rotated existing admin token"
grep -q '^export SECRETARY_NODE_PAIRING_TOKEN=' "$UPGRADE_STATE/environment" || fail "retry did not repair missing pairing token"
unset SECRETARY_NODE_PAIRING_TOKEN SECRETARY_NODE_ADMIN_TOKEN

# Native Codex setup adopts an explicit complete pair and never invokes npm.
export HOME="$TMP/codex-native-adopt-home" PATH="$CODBIN:/usr/bin:/bin"
adopt_codex_t06_pair
"$ROOT/secretary" setup --non-interactive --harness codex >/dev/null
"$ROOT/secretary" doctor >/dev/null
"$ROOT/secretary" node doctor >/dev/null
"$T06_NODE_BINARY" --codex-pair-action=show --codex-pair-scope=server | grep -q '"validated": true' || fail "Server native ACP pair not validated"
"$T06_NODE_BINARY" --codex-pair-action=show --codex-pair-scope=node | grep -q '"validated": true' || fail "Node native ACP pair not validated"
before_native="$(cat "$HOME/.local/share/secretary/environment")"
"$ROOT/secretary" setup --non-interactive >/dev/null
[[ "$(cat "$HOME/.local/share/secretary/environment")" == "$before_native" ]] || fail "repeat native setup rotated credentials"
[[ ! -e "$CODBIN/npm" ]] || fail "native fixture unexpectedly required npm"

# Native unauthed Codex is not preferred over an authorized Claude harness.
export HOME="$TMP/auto-choice-home" PATH="$CODBIN:$CLAUDEBIN:/usr/bin:/bin"
export T06_CODEX_AUTH_FAIL=1
"$ROOT/secretary" setup --non-interactive >/dev/null
AUTO_STATE="$HOME/.local/share/secretary"
grep -q '^harness = "claude_code"$' "$AUTO_STATE/config.toml" || fail "unauthorized Codex wrongly won auto-selection"
grep -q '^default_harness = "claude_code"$' "$AUTO_STATE/config.toml" || fail "auto-selection did not set Claude Worker defaults"
[[ ! -e "$AUTO_STATE/native/releases" ]] || fail "unready native Codex auto-selection downloaded managed binaries"
unset T06_CODEX_AUTH_FAIL

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

export HOME="$TMP/telegram-mac04-home" PATH="$CODBIN:/usr/bin:/bin"
adopt_codex_t06_pair
"$ROOT/secretary" setup --non-interactive --harness codex >/dev/null
STATE="$HOME/.local/share/secretary"
ENV_FILE="$STATE/environment"
cp "$ENV_FILE" "$TMP/mac04-before"
printf "export SECRETARY_TELEGRAM_ENABLED='true'\nexport SECRETARY_TELEGRAM_BOT_TOKEN='123:fixture-secret'\nexport SECRETARY_TELEGRAM_SERVER_CREDENTIAL='fixture-credential'\n" >> "$ENV_FILE"
if "$ROOT/secretary" doctor >"$TMP/mac04-doctor" 2>&1; then fail "doctor accepted enabled Telegram without origin"; fi
grep -q 'SECRETARY_TELEGRAM_WEB_BASE_URL' "$TMP/mac04-doctor" || fail "doctor omitted Telegram origin diagnostic"
! grep -q 'fixture-secret\|fixture-credential' "$TMP/mac04-doctor" || fail "doctor exposed a credential"
printf "export SECRETARY_TELEGRAM_WEB_BASE_URL='http://example.com'\n" >> "$ENV_FILE"
if "$ROOT/secretary" doctor >"$TMP/mac04-doctor" 2>&1; then fail "doctor accepted HTTP Telegram origin"; fi
sed -i "/SECRETARY_TELEGRAM_WEB_BASE_URL/d" "$ENV_FILE"
printf "export SECRETARY_TELEGRAM_WEB_BASE_URL='https://localhost'\n" >> "$ENV_FILE"
if "$ROOT/secretary" doctor >"$TMP/mac04-doctor" 2>&1; then fail "doctor accepted local Telegram origin"; fi
sed -i "/SECRETARY_TELEGRAM_WEB_BASE_URL/d" "$ENV_FILE"
printf "export SECRETARY_TELEGRAM_WEB_BASE_URL='https://example.com'\n" >> "$ENV_FILE"
"$ROOT/secretary" doctor >/dev/null || fail "doctor rejected valid HTTPS Telegram origin"
printf "export SECRETARY_TELEGRAM_ENABLED='false'\n" >> "$ENV_FILE"
sed -i "/SECRETARY_TELEGRAM_WEB_BASE_URL/d" "$ENV_FILE"
"$ROOT/secretary" doctor >/dev/null || fail "disabled Telegram required origin"

export MAC04_ROOT="$ROOT" MAC04_STATE="$STATE" MAC04_ENV_FILE="$ENV_FILE"
zsh <<'MAC04'
source "$MAC04_ROOT/scripts/secretary-interactive.zsh"
STATE="$MAC04_STATE"
ENV_FILE="$MAC04_ENV_FILE"
terminal_available() { return 0; }
typeset -a answers
integer position=0
terminal_read() {
  (( position++ ))
  REPLY="${answers[position]}"
  [[ -n "$REPLY" ]] || REPLY="${2:-}"
  return 0
}
terminal_secret() {
  (( position++ ))
  REPLY="${answers[position]}"
  return 0
}
fail() { print -u2 "FAIL: $1"; exit 1; }
set_answers() { answers=("$@"); position=0; }
before="$(<"$ENV_FILE")"
set_answers yes 123:fixture-secret '' ''
configure_telegram >/dev/null 2>&1 && fail "empty origin accepted"
[[ "$(<"$ENV_FILE")" == "$before" ]] || fail "empty origin changed environment"
set_answers yes 123:fixture-secret '' 'https://localhost'
configure_telegram >/dev/null 2>&1 && fail "local origin accepted"
[[ "$(<"$ENV_FILE")" == "$before" ]] || fail "invalid origin changed environment"
for candidate in https://user@example.com https://example.com/path 'https://example.com?x=1' https://192.168.1.1; do
  set_answers yes 123:fixture-secret '' "$candidate"
  configure_telegram >/dev/null 2>&1 && fail "invalid Telegram origin accepted: $candidate"
  [[ "$(<"$ENV_FILE")" == "$before" ]] || fail "invalid origin modified environment"
done
terminal_secret() { return 1; }
configure_telegram >/dev/null 2>&1 && fail "interrupted token input accepted"
[[ "$(<"$ENV_FILE")" == "$before" ]] || fail "cancel changed environment"
terminal_secret() { (( position++ )); REPLY="${answers[position]}"; }
set_answers yes 123:fixture-secret '' https://example.com
configure_telegram >/dev/null || fail "valid origin rejected"
source "$ENV_FILE"
[[ "$SECRETARY_TELEGRAM_ENABLED" == true && "$SECRETARY_TELEGRAM_WEB_BASE_URL" == https://example.com ]] || fail "valid origin was not persisted"
first="$(<"$ENV_FILE")"
set_answers yes '' '' ''
configure_telegram >/dev/null || fail "repeat configuration rejected"
[[ "$(<"$ENV_FILE")" == "$first" ]] || fail "repeat configuration modified stored credentials or origin"
MAC04
"$ROOT/secretary" doctor >/dev/null || fail "doctor rejected interactive Telegram configuration"

printf 'PASS: T06 Codex/Claude setup, upgrade/retry credential repair, authenticated auto-selection, workspace persistence and Linux lifecycle\n'
