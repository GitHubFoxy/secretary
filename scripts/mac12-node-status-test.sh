#!/bin/zsh
set -euo pipefail

[[ "$(uname -s)" == Darwin ]] || exit 0
ROOT="${0:A:h:h}"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/mac12-node-status.XXXXXX")"
PID=""
cleanup() {
  [[ -z "$PID" ]] || { kill "$PID" 2>/dev/null || true; wait "$PID" 2>/dev/null || true; }
  rm -rf "$TEST_HOME"
}
trap cleanup EXIT

export HOME="$TEST_HOME"
export SECRETARY_BINARY_STAGE_BIN="$TEST_HOME/bin"
mkdir -p "$TEST_HOME/bin" "$TEST_HOME/.local/share/secretary/node/data"
CONFIG="$TEST_HOME/.local/share/secretary/node/config.json"
BIN="$TEST_HOME/bin/secretary-node"
cat > "$TEST_HOME/node.c" <<'EOF'
#include <unistd.h>
int main(int argc, char **argv) {
    (void)argc;
    (void)argv;
    sleep(60);
    return 0;
}
EOF
cc -o "$BIN" "$TEST_HOME/node.c"
printf '{"node":"test-app-node","server_url":"http://127.0.0.1:8081","data_dir":"%s"}\n' "$TEST_HOME/.local/share/secretary/node/data" > "$CONFIG"

"$BIN" --config "$CONFIG" &
PID="$!"
sleep 1
kill -0 "$PID"

node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=test-app-node outbound-only"* ]] || { print -u2 -- "$node_output"; exit 1; }
status_output="$("$ROOT/secretary" status)"
[[ "$status_output" == *"running Execution node: pid=$PID "* ]] || { print -u2 -- "$status_output"; exit 1; }

printf '%s\n' "$PID" > "$TEST_HOME/.local/share/secretary/node/node.pid"
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID "* ]] || exit 1
printf '%s\n' "$$" > "$TEST_HOME/.local/share/secretary/node/node.pid"
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID "* ]] || exit 1

printf '{"node":"other-node","server_url":"http://127.0.0.1:8081","data_dir":"%s"}\n' "$TEST_HOME/.local/share/secretary/node/data" > "$CONFIG"
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=other-node"* ]] || exit 1

kill "$PID"
wait "$PID" 2>/dev/null || true
PID=""
rm -f "$TEST_HOME/.local/share/secretary/node/node.pid"
mkdir -p "$TEST_HOME/.local/share/secretary-node/current/bin"
STANDALONE="$TEST_HOME/.local/share/secretary-node/current/bin/secretary-node"
cp "$BIN" "$STANDALONE"
STANDALONE_CONFIG="$TEST_HOME/.local/share/secretary-node/config.json"
printf '{"node":"standalone-node","server_url":"http://127.0.0.1:8081","data_dir":"%s"}\n' "$TEST_HOME/.local/share/secretary/node/data" > "$STANDALONE_CONFIG"
"$STANDALONE" --config "$STANDALONE_CONFIG" &
PID="$!"
sleep 1
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=standalone-node"* ]] || { print -u2 -- "$node_output"; exit 1; }
status_output="$("$ROOT/secretary" status)"
[[ "$status_output" == *"running Execution node: pid=$PID "* ]] || exit 1

OVERRIDE="$TEST_HOME/override.json"
printf '{"node":"override-node","server_url":"http://127.0.0.1:8081","data_dir":"%s"}\n' "$TEST_HOME/.local/share/secretary/node/data" > "$OVERRIDE"
kill "$PID"
wait "$PID" 2>/dev/null || true
SECRETARY_NODE_EXECUTABLE="$STANDALONE" SECRETARY_NODE_CONFIG="$OVERRIDE" "$STANDALONE" --config "$OVERRIDE" &
PID="$!"
sleep 1
node_output="$(SECRETARY_NODE_EXECUTABLE="$STANDALONE" SECRETARY_NODE_CONFIG="$OVERRIDE" "$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=override-node"* ]] || { print -u2 -- "$node_output"; exit 1; }
mkdir -p "$HOME/Library/LaunchAgents"
touch "$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
node_output="$(SECRETARY_NODE_EXECUTABLE="$STANDALONE" SECRETARY_NODE_CONFIG="$OVERRIDE" "$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=override-node"* ]] || exit 1
rm "$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"

kill "$PID"
wait "$PID" 2>/dev/null || true
PID=""
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node stopped identity=other-node"* ]] || exit 1
status_output="$("$ROOT/secretary" status)"
[[ "$status_output" == *"running Execution node: stopped"* ]] || exit 1
print "MAC12-02 Node status regression passed"
