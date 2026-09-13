#!/bin/zsh
set -euo pipefail

ROOT="${0:A:h:h}"
ORIGINAL_HOME="$HOME"
TEST_HOME="$(mktemp -d "${TMPDIR:-/tmp}/secretary-node-revoke-test.XXXXXX")"
FAKE_BIN="$TEST_HOME/bin"
trap '[[ -n "${WORK_PID:-}" ]] && kill "$WORK_PID" 2>/dev/null || true; rm -rf "$TEST_HOME"' EXIT
mkdir -p "$FAKE_BIN" "$TEST_HOME/.local/share/secretary/node/data"
cat > "$FAKE_BIN/curl" <<'EOF'
#!/bin/zsh
set -euo pipefail
url="${@[-1]}"
log="$HOME/curl.log"
if [[ "$url" == */v1/nodes/test-node ]]; then
  count=0
  [[ -f "$HOME/status.count" ]] && count="$(<"$HOME/status.count")"
  count=$((count + 1))
  print -r -- "$count" > "$HOME/status.count"
  if [[ "${STATUS_MODE:-wait}" == "timeout" || "$count" == 1 ]]; then
    print '{"node":"test-node","active_attempts":[{"attempt_id":"active"}]}'
  else
    print '{"node":"test-node"}'
  fi
  exit 0
fi
print -r -- "$url $*" >> "$log"
exit 0
EOF
chmod +x "$FAKE_BIN/curl"
export HOME="$TEST_HOME"
export PATH="$FAKE_BIN:/usr/bin:/bin"
export SECRETARY_NODE_ADMIN_TOKEN=admin
mkdir -p "$HOME/.local/share/secretary/node"
cat > "$HOME/.local/share/secretary/node/config.json" <<'EOF'
{
  "server_url": "http://secretary.test",
  "node": "test-node",
  "data_dir": "$HOME/.local/share/secretary/node/data",
  "workspaces": []
}
EOF
cat > "$HOME/.local/share/secretary/node/data/identity.json" <<'EOF'
{"node":"test-node","credential":"AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA","connect_url":"ws://secretary.test/v1/nodes/connect?node=test-node"}
EOF

sleep 20 &
WORK_PID=$!
print -r -- "$WORK_PID" > "$HOME/.local/share/secretary/node/node.pid"
output="$("$ROOT/sex" node revoke --timeout 2 --interval 0.01)"
[[ "$output" == *"Node revoke requested: test-node"* ]] || { print -u2 "revoke did not complete: $output"; exit 1; }
if kill -0 "$WORK_PID" 2>/dev/null; then
  print -u2 "revoke did not stop idle process"
  exit 1
fi
WORK_PID=""
log="$(<"$HOME/curl.log")"
[[ "$log" == *"/drain"* && "$log" == *"/revoke"* ]] || { print -u2 "drain/revoke calls missing: $log"; exit 1; }
[[ "$(<"$HOME/status.count")" -ge 2 ]] || { print -u2 "revoke did not poll remote status"; exit 1; }

: > "$HOME/curl.log"
print 0 > "$HOME/status.count"
export STATUS_MODE=timeout
sleep 20 &
WORK_PID=$!
print -r -- "$WORK_PID" > "$HOME/.local/share/secretary/node/node.pid"
if "$ROOT/sex" node revoke --timeout 0 --interval 0.01 >/dev/null 2>&1; then
  print -u2 "timeout revoke unexpectedly succeeded"
  exit 1
fi
[[ "$WORK_PID" != "" ]] && kill -0 "$WORK_PID" 2>/dev/null || { print -u2 "timeout revoke killed active process"; exit 1; }
log="$(<"$HOME/curl.log")"
[[ "$log" == *"/drain"* && "$log" != *"/revoke"* ]] || { print -u2 "timeout revoke did not refuse safely: $log"; exit 1; }
kill "$WORK_PID"
WORK_PID=""
print "All Node revoke tests passed."
