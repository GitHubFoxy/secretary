#!/bin/zsh
set -euo pipefail

SCRIPT_DIR="${0:A:h}"
ROOT="${SCRIPT_DIR:h}"
SEX="$ROOT/sex"
ORIGINAL_HOME="$HOME"
REAL_PATH="${PATH:-/usr/bin:/bin}"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/secretary-sex-test.XXXXXX")"
FAKE_BIN="$TEST_HOME/fake-bin"
STATE="$TEST_HOME/.local/share/secretary"
BIN="$TEST_HOME/.local/bin"
PLIST="$TEST_HOME/Library/LaunchAgents/dev.secretary.sex.plist"
LAUNCHCTL_LOG="$TEST_HOME/launchctl.log"
OPEN_LOG="$TEST_HOME/open.log"

fail() {
  print -u2 -- "FAIL: $1"
  exit 1
}

pass() {
  print -- "ok - $1"
}

assert_contains() {
  local value="$1"
  local expected="$2"
  local message="$3"
  [[ "$value" == *"$expected"* ]] || fail "$message: expected '$expected', got '$value'"
}

assert_not_contains() {
  local value="$1"
  local unexpected="$2"
  local message="$3"
  [[ "$value" != *"$unexpected"* ]] || fail "$message: found '$unexpected' in '$value'"
}

assert_file() {
  local path="$1"
  [[ -f "$path" ]] || fail "missing file: $path"
}

assert_executable() {
  local path="$1"
  [[ -x "$path" ]] || fail "missing executable: $path"
}

assert_http_code() {
  local url="$1"
  local expected="$2"
  local actual
  actual="$(curl -sS -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || true)"
  [[ "$actual" == "$expected" ]] || fail "$url: expected HTTP $expected, got $actual"
}

wait_for_server() {
  local url="$1"
  local code
  for _ in {1..80}; do
    code="$(curl -sS -o /dev/null -w '%{http_code}' "$url" 2>/dev/null || true)"
    if [[ "$code" == "200" || "$code" == "401" ]]; then
      return 0
    fi
    sleep 0.25
  done
  return 1
}

kill_matching_processes() {
  local pattern="$1"
  local candidate
  while IFS= read -r candidate; do
    [[ -n "$candidate" && "$candidate" != "$$" ]] || continue
    kill -TERM "$candidate" 2>/dev/null || true
  done < <(pgrep -f -- "$pattern" 2>/dev/null || true)
  sleep 0.1
  while IFS= read -r candidate; do
    [[ -n "$candidate" && "$candidate" != "$$" ]] || continue
    kill -KILL "$candidate" 2>/dev/null || true
  done < <(pgrep -f -- "$pattern" 2>/dev/null || true)
}

stop_background() {
  local pid="$1"
  [[ -n "$pid" ]] || return 0
  kill -TERM -- -"$pid" 2>/dev/null || kill -TERM "$pid" 2>/dev/null || true
  set +e
  wait "$pid"
  set -e
  kill_matching_processes "$STATE/secretaryd.log"
}

cleanup() {
  set +e
  if [[ -n "${logs_pid:-}" ]]; then
    kill -TERM -- -"$logs_pid" 2>/dev/null || kill -TERM "$logs_pid" 2>/dev/null || true
    wait "$logs_pid" 2>/dev/null || true
  fi
  if [[ -n "${serve_pid:-}" ]]; then
    kill -TERM -- -"$serve_pid" 2>/dev/null || kill -TERM "$serve_pid" 2>/dev/null || true
    wait "$serve_pid" 2>/dev/null || true
  fi
  kill_matching_processes "$TEST_HOME"
  if [[ -f "$STATE/server.pid" ]]; then
    HOME="$TEST_HOME" PATH="$FAKE_BIN:$REAL_PATH" "$SEX" stop >/dev/null 2>&1 || true
  fi
  chmod -R u+w "$TEST_HOME" 2>/dev/null || true
  if [[ "${SEX_TEST_KEEP:-0}" == "1" ]]; then
    print -u2 -- "Keeping test HOME: $TEST_HOME"
  else
    rm -rf "$TEST_HOME"
  fi
}
trap cleanup EXIT

[[ -x "$SEX" ]] || fail "sex is not executable: $SEX"
mkdir -p "$FAKE_BIN"

cat > "$FAKE_BIN/codex" <<'EOF'
#!/bin/sh
if [ "$1" = "login" ] && [ "$2" = "status" ]; then
  exit 0
fi
if [ "$1" = "login" ]; then
  exit 0
fi
exit 0
EOF

cat > "$FAKE_BIN/open" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$OPEN_LOG"
EOF

cat > "$FAKE_BIN/launchctl" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "$LAUNCHCTL_LOG"
exit 0
EOF

chmod +x "$FAKE_BIN/codex" "$FAKE_BIN/open" "$FAKE_BIN/launchctl"
export HOME="$TEST_HOME"
export PATH="$FAKE_BIN:$REAL_PATH"
export MISE_DATA_DIR="${MISE_DATA_DIR:-$ORIGINAL_HOME/.local/share/mise}"
export GOMODCACHE="${GOMODCACHE:-$ORIGINAL_HOME/go/pkg/mod}"
export GOCACHE="${GOCACHE:-$ORIGINAL_HOME/Library/Caches/go-build}"
export MISE_YES=1
export OPEN_LOG LAUNCHCTL_LOG
unset SECRETARY_ACP_COMMAND SECRETARY_FX_COMMAND SECRETARY_OPENCODE_COMMAND SECRETARY_CAPABILITY SECRETARY_BOOTSTRAP_TOKEN

FAKE_ACP_REAL="$FAKE_BIN/codex-acp-real"
mise exec -- go build -o "$FAKE_ACP_REAL" ./cmd/fake-codex-acp >/dev/null
cat > "$FAKE_BIN/codex-acp" <<EOF
#!/bin/sh
exec "$FAKE_ACP_REAL" "\$@"
EOF
cat > "$FAKE_BIN/fx" <<'EOF'
#!/bin/sh
exec "$(dirname "$0")/codex-acp" "$@"
EOF
chmod +x "$FAKE_BIN/codex-acp" "$FAKE_BIN/fx"

print "Testing sex commands in isolated HOME: $TEST_HOME"

output="$($SEX)" || fail "usage command failed"
assert_contains "$output" "Usage: sex" "usage output"
pass "usage"

output="$($SEX status)" || fail "status command failed"
assert_contains "$output" "stopped" "initial status"
pass "status when stopped"

output="$($SEX setup)" || fail "setup command failed"
assert_contains "$output" "Setup complete" "setup output"
assert_executable "$BIN/secretaryd"
assert_executable "$BIN/secretaryctl"
assert_executable "$BIN/secretary-mcp"
assert_file "$STATE/config.toml"
assert_file "$STATE/environment"
assert_file "$STATE/profiles/secretary.md"
assert_file "$STATE/profiles/worker.md"
assert_file "$STATE/profiles/child-worker.md"
assert_contains "$(cat "$STATE/config.toml")" 'harness = "fx"' "default harness"
assert_contains "$(cat "$STATE/config.toml")" 'secretary = "gpt-5.6-luna"' "default Secretary model"
[[ -L "$BIN/sex" ]] || fail "setup did not install sex symlink"
pass "setup"

output="$($SEX doctor)" || fail "doctor command failed"
assert_contains "$output" "Doctor found no problems" "doctor output"
pass "doctor"

existing_code="$(curl -sS -o /dev/null -w '%{http_code}' http://127.0.0.1:8081/v1/web/session 2>/dev/null || true)"
[[ "$existing_code" == "000" ]] || fail "port 8081 is already in use with HTTP $existing_code"

output="$($SEX start)" || fail "start command failed"
assert_contains "$output" "Secretary started" "start output"
assert_not_contains "$output" "code=000" "start readiness output"
status_output="$($SEX status)" || fail "status command failed while running"
assert_contains "$status_output" "mode=normal" "normal running status"
assert_http_code "http://127.0.0.1:8081/control-room" "404"
assert_file "$OPEN_LOG"
assert_contains "$(cat "$OPEN_LOG")" "#bootstrap=" "browser pairing URL"
pass "start in normal mode"

output="$($SEX start)" || fail "start reuse command failed"
assert_contains "$output" "already running" "start reuse output"
pass "start reuses an existing server"

logs_pid=""
$SEX logs >"$TEST_HOME/logs.out" 2>&1 &
logs_pid="$!"
sleep 0.5
stop_background "$logs_pid"
logs_pid=""
assert_file "$TEST_HOME/logs.out"
assert_contains "$(cat "$TEST_HOME/logs.out")" "web API listening" "logs output"
pass "logs"

output="$($SEX stop)" || fail "stop command failed"
assert_contains "$output" "Secretary stopped" "stop output"
output="$($SEX stop)" || fail "stop command failed when already stopped"
assert_contains "$output" "not running" "stopped stop output"
pass "stop"

output="$($SEX restart --debug)" || fail "restart command failed"
assert_contains "$output" "Secretary started" "restart output"
assert_not_contains "$output" "code=000" "restart readiness output"
status_output="$($SEX status)" || fail "status command failed after restart"
assert_contains "$status_output" "mode=debug" "debug running status"
assert_http_code "http://127.0.0.1:8081/control-room" "200"
pass "restart --debug"

output="$($SEX stop)" || fail "stop after restart failed"
assert_contains "$output" "Secretary stopped" "stop after restart output"

output="$($SEX install-service --debug)" || fail "install-service command failed"
assert_contains "$output" "Secretary will start" "install-service output"
assert_file "$PLIST"
assert_contains "$(cat "$PLIST")" "<string>serve</string><string>--debug</string>" "debug launchd plist"
assert_contains "$(cat "$LAUNCHCTL_LOG")" "bootstrap" "launchd bootstrap call"
pass "install-service --debug"

output="$($SEX uninstall-service)" || fail "uninstall-service command failed"
assert_contains "$output" "Service removed" "uninstall-service output"
[[ ! -e "$PLIST" ]] || fail "uninstall-service left plist behind"
assert_contains "$(cat "$LAUNCHCTL_LOG")" "bootout" "launchd bootout call"
pass "uninstall-service"

serve_pid=""
$SEX serve --debug >"$TEST_HOME/serve.out" 2>&1 &
serve_pid="$!"
wait_for_server "http://127.0.0.1:8081/v1/web/session" || fail "serve did not become ready"
status_output="$($SEX status)" || fail "status command failed for serve"
assert_contains "$status_output" "mode=debug" "serve debug status"
assert_http_code "http://127.0.0.1:8081/control-room" "200"
stop_background "$serve_pid"
serve_pid=""
pass "serve --debug"

output="$($SEX status)" || fail "final status command failed"
assert_contains "$output" "stopped" "final status"
pass "final stopped status"

print "All sex CLI command tests passed."
