#!/bin/bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TASK_TMP="$(mktemp -d)"
trap 'rm -rf "$TASK_TMP"' EXIT
export TASK_TMP HOME="$TASK_TMP/home" PATH="$TASK_TMP/bin:$PATH" TEST_SETUP_MARKER="$TASK_TMP/calls"
mkdir -p "$HOME/.codex" "$TASK_TMP/bin"
printf 'global config canary\n' > "$HOME/.codex/config.toml"

go build -o "$TASK_TMP/bin/secretary-node" "$TASK_ROOT/cmd/secretary-node"
cat > "$TASK_TMP/bin/mise" <<'FIXTURE'
#!/bin/bash
set -eu
[[ "$1 $2 $3 $4 $5" == 'exec -- go build -o' ]]
if [[ "$6" != */secretary-node ]]; then
  cat > "$6" <<'BINARY'
#!/bin/bash
exit 0
BINARY
  chmod +x "$6"
fi
FIXTURE

cat > "$TASK_TMP/bin/codex" <<'FIXTURE'
#!/bin/bash
case "$*" in
  "--version") echo "codex-cli 0.163.0" ;;
  "login status") echo "Logged in" ;;
  *) exit 2 ;;
esac
FIXTURE
cat > "$TASK_TMP/bin/codex-acp" <<'FIXTURE'
#!/usr/bin/env python3
import json, os, sys
if '--version' in sys.argv:
    print('codex-acp 0.16.0')
    sys.exit(0)
for line in sys.stdin:
    request = json.loads(line)
    method = request.get('method')
    with open(os.environ['TEST_SETUP_MARKER'], 'a') as marker:
        marker.write('real-acp-' + str(method) + '\n')
    if method == 'initialize':
        result = {'protocolVersion': 1, 'agentInfo': {'version': '0.16.0'}, 'agentCapabilities': {'loadSession': True}, '_meta': {'steering': {'supported': True}}}
    elif method == 'session/new':
        result = {'sessionId': 'fixture-session', 'models': {'availableModels': [{'modelId': 'gpt-fixture'}]}}
    else:
        result = {}
    print(json.dumps({'jsonrpc': '2.0', 'id': request.get('id'), 'result': result}), flush=True)
FIXTURE
for task_binary in opencode fx; do
  cat > "$TASK_TMP/bin/$task_binary" <<'FIXTURE'
#!/bin/bash
echo legacy-call >> "$TEST_SETUP_MARKER"
exit 2
FIXTURE
  chmod +x "$TASK_TMP/bin/$task_binary"
done
chmod +x "$TASK_TMP/bin/mise" "$TASK_TMP/bin/codex" "$TASK_TMP/bin/codex-acp"
"$TASK_TMP/bin/secretary-node" --codex-pair-action=install --codex-pair-scope=server --codex-pair-cli="$TASK_TMP/bin/codex" --codex-pair-acp="$TASK_TMP/bin/codex-acp"
"$TASK_TMP/bin/secretary-node" --codex-pair-action=install --codex-pair-scope=node --codex-pair-cli="$TASK_TMP/bin/codex" --codex-pair-acp="$TASK_TMP/bin/codex-acp"
"$TASK_ROOT/secretary" setup --non-interactive --harness codex
"$TASK_ROOT/secretary" doctor
"$TASK_ROOT/secretary" node setup --standalone --server http://127.0.0.1:9999 --name codex-canary
"$TASK_ROOT/secretary" node doctor
"$TASK_TMP/bin/secretary-node" --codex-pair-action=show --codex-pair-scope=server > "$TASK_TMP/server.json"
"$TASK_TMP/bin/secretary-node" --codex-pair-action=show --codex-pair-scope=node > "$TASK_TMP/node.json"
python3 - "$TASK_TMP/server.json" "$TASK_TMP/node.json" "$TASK_TMP/bin" <<'PY'
import json,sys
for scope,path in zip(('server','node'),sys.argv[1:3]):
    record=json.load(open(path))
    assert record['scope']==scope and record['compatibility']['validated']
    assert record['codex']['path']==sys.argv[3]+'/codex'
    assert record['acp']['path']==sys.argv[3]+'/codex-acp'
PY
grep -q 'real-acp-initialize' "$TEST_SETUP_MARKER"
grep -q 'real-acp-session/new' "$TEST_SETUP_MARKER"
[[ "$(cat "$HOME/.codex/config.toml")" == 'global config canary' ]]
[[ "$(cat "$TEST_SETUP_MARKER")" != *legacy-call* ]]
grep -Eq '^harness = "codex"$' "$HOME/.local/share/secretary/config.toml"
grep -Eq '^reply_contract = "addressed-reply-v1"$' "$HOME/.local/share/secretary/config.toml"
cp "$HOME/.local/share/secretary/config.toml" "$TASK_TMP/explicit-contract.before"
if zsh -c 'STATE="$1"; source "$2"; set_config_harness claude_code false' _ "$HOME/.local/share/secretary" "$TASK_ROOT/scripts/secretary-interactive.zsh"; then
  echo "Existing explicit Codex contract was silently replaced by Claude" >&2
  exit 1
fi
cmp "$TASK_TMP/explicit-contract.before" "$HOME/.local/share/secretary/config.toml"
grep -Eq '"include_opencode": false' "$HOME/.local/share/secretary/node/config.json"
