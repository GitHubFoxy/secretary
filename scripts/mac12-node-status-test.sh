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
write_fixture_identity() {
  local config="$1" name="$2" start
  start="$(ps -p "$PID" -o lstart= | sed 's/^[[:space:]]*//;s/[[:space:]]*$//')"
  printf '%s\n%s\n%s\n' "$PID" "$start" "$name" > "$config.process"
  chmod 600 "$config.process"
}
diagnose_node_process() {
  local executable="$1" config="$2" output="$3" args=""
  print -u2 -- "MAC12-02 discovery failure: expected_pid=$PID"
  if kill -0 "$PID" 2>/dev/null; then print -u2 -- "process_alive=yes"; else print -u2 -- "process_alive=no"; fi
  if ps -p "$PID" -o comm= 2>/dev/null | grep -Fqx -- "$executable"; then print -u2 -- "ps_executable_exact=yes"; else print -u2 -- "ps_executable_exact=no"; fi
  if /usr/sbin/lsof -a -p "$PID" -d txt -Fn 2>/dev/null | sed -n 's/^n//p' | grep -Fqx -- "$executable"; then print -u2 -- "lsof_executable_exact=yes"; else print -u2 -- "lsof_executable_exact=no"; fi
  args="$(ps -ww -p "$PID" -o args= 2>/dev/null || true)"
  if [[ "$args" == "$executable --config $config" ]]; then print -u2 -- "ps_args_exact=yes"; else print -u2 -- "ps_args_exact=no"; fi
  if ps -axo pid=,command= 2>/dev/null | grep -Fq -- "$executable --config $config"; then print -u2 -- "ps_candidate_listed=yes"; else print -u2 -- "ps_candidate_listed=no"; fi
  print -u2 -- "expected_canonical=${executable:A}"
  print -u2 -- "fixture_txt_executable_begin"
  /usr/sbin/lsof -a -p "$PID" -d txt -Fn 2>/dev/null | sed -n 's/^n//p' | head -n 6 >&2 || true
  print -u2 -- "fixture_txt_executable_end"
  print -u2 -- "node_status_running=$([[ "$output" == *"Node running pid=$PID "* ]] && print yes || print no)"
}

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
write_fixture_identity "$CONFIG" test-app-node

node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=test-app-node configured_identity=test-app-node outbound-only"* ]] || { diagnose_node_process "$BIN" "$CONFIG" "$node_output"; print -u2 -- "$node_output"; exit 1; }
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
[[ "$node_output" == *"Node running pid=$PID identity=test-app-node configured_identity=other-node"* ]] || exit 1

kill "$PID"
wait "$PID" 2>/dev/null || true
PID=""
rm -f "$TEST_HOME/.local/share/secretary/node/node.pid"
RELEASE="$TEST_HOME/.local/share/secretary/releases/test-release"
mkdir -p "$RELEASE/bin"
cp "$BIN" "$RELEASE/bin/secretary-node"
printf 'vfixture\n' > "$RELEASE/VERSION"
printf 'fixture-revision\n' > "$RELEASE/REVISION"
ln -s "$RELEASE" "$TEST_HOME/.local/share/secretary/current"
rm "$BIN"
ln -s "$TEST_HOME/.local/share/secretary/current/bin/secretary-node" "$BIN"
[[ -L "$BIN" && -L "$TEST_HOME/.local/share/secretary/current" && "${BIN:A}" == "${RELEASE:A}/bin/secretary-node" ]] || exit 1
"$BIN" --config "$CONFIG" &
PID="$!"
sleep 1
write_fixture_identity "$CONFIG" test-app-node
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=test-app-node configured_identity=other-node"* && "$node_output" == *"revision=fixture-revision"* ]] || { diagnose_node_process "$BIN" "$CONFIG" "$node_output"; exit 1; }
status_output="$("$ROOT/secretary" status)"
[[ "$status_output" == *"running Execution node: pid=$PID "* && "$status_output" == *"revision=fixture-revision"* ]] || exit 1
kill "$PID"
wait "$PID" 2>/dev/null || true
PID=""
mkdir -p "$TEST_HOME/.local/share/secretary-node/current/bin"
STANDALONE="$TEST_HOME/.local/share/secretary-node/current/bin/secretary-node"
cp "$BIN" "$STANDALONE"
STANDALONE_CONFIG="$TEST_HOME/.local/share/secretary-node/config.json"
printf '{"node":"standalone-node","server_url":"http://127.0.0.1:8081","data_dir":"%s"}\n' "$TEST_HOME/.local/share/secretary/node/data" > "$STANDALONE_CONFIG"
"$STANDALONE" --config "$STANDALONE_CONFIG" &
PID="$!"
sleep 1
write_fixture_identity "$STANDALONE_CONFIG" standalone-node
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=standalone-node configured_identity=standalone-node"* ]] || { print -u2 -- "$node_output"; exit 1; }
status_output="$("$ROOT/secretary" status)"
[[ "$status_output" == *"running Execution node: pid=$PID "* ]] || exit 1

OVERRIDE="$TEST_HOME/override.json"
printf '{"node":"override-node","server_url":"http://127.0.0.1:8081","data_dir":"%s"}\n' "$TEST_HOME/.local/share/secretary/node/data" > "$OVERRIDE"
kill "$PID"
wait "$PID" 2>/dev/null || true
SECRETARY_NODE_EXECUTABLE="$STANDALONE" SECRETARY_NODE_CONFIG="$OVERRIDE" "$STANDALONE" --config "$OVERRIDE" &
PID="$!"
sleep 1
write_fixture_identity "$OVERRIDE" override-node
node_output="$(SECRETARY_NODE_EXECUTABLE="$STANDALONE" SECRETARY_NODE_CONFIG="$OVERRIDE" "$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=override-node configured_identity=override-node"* && "$node_output" == *"source/unversioned"* ]] || { diagnose_node_process "$STANDALONE" "$OVERRIDE" "$node_output"; print -u2 -- "$node_output"; exit 1; }
mkdir -p "$HOME/Library/LaunchAgents"
touch "$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"
node_output="$(SECRETARY_NODE_EXECUTABLE="$STANDALONE" SECRETARY_NODE_CONFIG="$OVERRIDE" "$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=override-node configured_identity=override-node"* ]] || exit 1
rm "$HOME/Library/LaunchAgents/dev.secretary.cli-node.plist"

kill "$PID"
wait "$PID" 2>/dev/null || true
PID=""
CUSTOM="$TEST_HOME/custom-execution-binary"
mkdir -p "$TEST_HOME/custom-release/bin"
cp "$BIN" "$TEST_HOME/custom-release/bin/custom-execution-binary"
printf 'vcustom\n' > "$TEST_HOME/custom-release/VERSION"
printf 'custom-revision\n' > "$TEST_HOME/custom-release/REVISION"
CUSTOM="$TEST_HOME/custom-release/bin/custom-execution-binary"
SECRETARY_NODE_EXECUTABLE="$CUSTOM" SECRETARY_NODE_CONFIG="$OVERRIDE" "$CUSTOM" --config "$OVERRIDE" &
PID="$!"
sleep 1
write_fixture_identity "$OVERRIDE" override-node
node_output="$(SECRETARY_NODE_EXECUTABLE="$CUSTOM" SECRETARY_NODE_CONFIG="$OVERRIDE" "$ROOT/secretary" node status)"
[[ "$node_output" == *"Node running pid=$PID identity=override-node configured_identity=override-node"* && "$node_output" == *"revision=custom-revision"* ]] || { diagnose_node_process "$CUSTOM" "$OVERRIDE" "$node_output"; print -u2 -- "$node_output"; exit 1; }
status_output="$(SECRETARY_NODE_EXECUTABLE="$CUSTOM" SECRETARY_NODE_CONFIG="$OVERRIDE" "$ROOT/secretary" status)"
[[ "$status_output" == *"running Execution node: pid=$PID "* && "$status_output" == *"revision=custom-revision"* ]] || exit 1

kill "$PID"
wait "$PID" 2>/dev/null || true
PID=""
node_output="$("$ROOT/secretary" node status)"
[[ "$node_output" == *"Node stopped identity=unknown configured_identity=other-node"* ]] || exit 1
status_output="$("$ROOT/secretary" status)"
[[ "$status_output" == *"running Execution node: stopped"* ]] || exit 1
print "MAC12-02 Node status regression passed"
