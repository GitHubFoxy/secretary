#!/bin/zsh
set -euo pipefail
[[ "$(uname -s)" == Linux ]] || exit 1
ROOT="${0:A:h:h}"
TEST_HOME="$(mktemp -d)"
PID=""
cleanup() {
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
"$BIN" --config "$CONFIG" &
PID="$!"
sleep 1
START="$(ps -p "$PID" -o lstart= | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
printf '%s\n%s\n%s\n' "$PID" "$START" "linux-runtime" > "$CONFIG.process"
chmod 600 "$CONFIG.process"
output="$("$ROOT/secretary" node status)"
[[ "$output" == *"Node running pid=$PID identity=linux-runtime configured_identity=linux-configured"* ]] || { print -u2 -- "$output"; exit 1; }
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
