#!/bin/zsh
set -euo pipefail
[[ "$(uname -s)" == Linux ]] || exit 1
ROOT="${0:A:h:h}"
TEST_HOME="$(mktemp -d)"
PID=""
SERVER_PID=""
cleanup() {
 [[ -z "$SERVER_PID" ]] || { kill "$SERVER_PID" 2>/dev/null || true; wait "$SERVER_PID" 2>/dev/null || true; }
 [[ -z "$PID" ]] || { kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; }
 rm -rf "$TEST_HOME"
}
trap cleanup EXIT
export HOME="$TEST_HOME" SECRETARY_BINARY_STAGE_BIN="$TEST_HOME/bin"
mkdir -p "$TEST_HOME/bin" "$TEST_HOME/.local/share/secretary/node"
CONFIG="$TEST_HOME/.local/share/secretary/node/config.json"
BIN="$TEST_HOME/bin/secretary-node"
printf '#include <unistd.h>\nint main(void) { sleep(60); return 0; }\n' > "$TEST_HOME/node.c"
cc -o "$BIN" "$TEST_HOME/node.c"
printf '{"node":"linux-configured"}\n' > "$CONFIG"
diagnose_node() {
 local actual_exe="" actual_argv="" actual_start="" owner="" mode=""
 if kill -0 "$PID" 2>/dev/null; then print -u2 -- "pid_alive=yes"; else print -u2 -- "pid_alive=no"; fi
 actual_exe="$(readlink -f "/proc/$PID/exe" 2>/dev/null || true)"
 [[ -n "$actual_exe" && "$actual_exe" == "${BIN:A}" ]] && print -u2 -- "exe_match=yes" || print -u2 -- "exe_match=no"
 actual_argv="$(ps -ww -p "$PID" -o args= 2>/dev/null || true)"
 [[ "$actual_argv" == "$BIN --config $CONFIG" ]] && print -u2 -- "config_argv_match=yes" || print -u2 -- "config_argv_match=no"
 [[ -f "$TEST_HOME/.local/share/secretary/node/node.pid" && "$(<"$TEST_HOME/.local/share/secretary/node/node.pid")" == "$PID" ]] && print -u2 -- "pidfile_match=yes" || print -u2 -- "pidfile_match=no"
 actual_start="$(ps -p "$PID" -o lstart= 2>/dev/null | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
 [[ -n "$actual_start" && "$actual_start" == "$START" ]] && print -u2 -- "start_match=yes" || print -u2 -- "start_match=no"
 owner="$(stat -c %u "$CONFIG.process" 2>/dev/null || true)"
 mode="$(stat -c %a "$CONFIG.process" 2>/dev/null || true)"
 [[ "$owner" == "$EUID" ]] && print -u2 -- "owner_match=yes" || print -u2 -- "owner_match=no"
 [[ "$mode" == 600 ]] && print -u2 -- "mode_0600=yes" || print -u2 -- "mode_0600=no"
}
"$BIN" --config "$CONFIG" &
PID="$!"
printf '%s\n' "$PID" > "$TEST_HOME/.local/share/secretary/node/node.pid"
START=""
for (( attempt=1; attempt<=40; attempt++ )); do
 if kill -0 "$PID" 2>/dev/null && [[ "$(readlink -f "/proc/$PID/exe" 2>/dev/null || true)" == "${BIN:A}" ]] && [[ "$(ps -ww -p "$PID" -o args= 2>/dev/null || true)" == "$BIN --config $CONFIG" ]]; then
  START="$(ps -p "$PID" -o lstart= 2>/dev/null | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  [[ -n "$START" ]] && break
 fi
 sleep 0.1
done
[[ -n "$START" ]] || { print -u2 -- "fixture_process_not_ready"; diagnose_node; exit 1; }
printf '%s\n%s\n%s\n' "$PID" "$START" "linux-runtime" > "$CONFIG.process"
chmod 600 "$CONFIG.process"
output="$("$ROOT/secretary" node status)"
[[ "$output" == *"Node running pid=$PID identity=linux-runtime configured_identity=linux-configured"* ]] || { print -u2 -- "$output"; diagnose_node; exit 1; }
chmod 644 "$CONFIG.process"
output="$("$ROOT/secretary" node status)"
[[ "$output" == *"Node running pid=$PID identity=unknown configured_identity=linux-configured"* ]] || { print -u2 -- "$output"; exit 1; }
chmod 600 "$CONFIG.process"
printf '%s\n%s\n%s\n' "$PID" "invalid-start" "wrong-runtime" > "$CONFIG.process"
output="$("$ROOT/secretary" node status)"
[[ "$output" == *"Node running pid=$PID identity=unknown configured_identity=linux-configured"* ]] || { print -u2 -- "$output"; exit 1; }
printf '%s\n%s\n%s\n' "999999" "$START" "wrong-runtime" > "$CONFIG.process"
output="$("$ROOT/secretary" node status)"
[[ "$output" == *"Node running pid=$PID identity=unknown configured_identity=linux-configured"* ]] || { print -u2 -- "$output"; exit 1; }
print "MAC12 Linux Node status passed"

SERVER_RELEASE="$TEST_HOME/known-release"
mkdir -p "$SERVER_RELEASE/bin"
printf 'v-fixture\\n' > "$SERVER_RELEASE/VERSION"
printf '0123456789abcdef0123456789abcdef01234567\\n' > "$SERVER_RELEASE/REVISION"
cc -o "$SERVER_RELEASE/bin/secretaryd" "$TEST_HOME/node.c"
"$SERVER_RELEASE/bin/secretaryd" &
SERVER_PID="$!"
printf '%s\\n' "$SERVER_PID" > "$TEST_HOME/.local/share/secretary/server.pid"
server_status="$("$ROOT/secretary" status 2>&1 || true)"
[[ "$server_status" == *"running Server: pid=$SERVER_PID version=v-fixture revision=0123456789abcdef0123456789abcdef01234567"* ]] || { print -u2 -- "$server_status"; exit 1; }
kill "$SERVER_PID"
wait "$SERVER_PID" 2>/dev/null || true
SERVER_PID=""
printf '999999\\n' > "$TEST_HOME/.local/share/secretary/server.pid"
server_status="$("$ROOT/secretary" status 2>&1 || true)"
[[ "$server_status" == *"running Server: stopped"* ]] || { print -u2 -- "$server_status"; exit 1; }
