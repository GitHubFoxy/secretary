#!/bin/bash
# Public CLI canary using executable fixtures, NOT native model acceptance.
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TASK_TMP="$(mktemp -d)"
trap 'rm -rf "$TASK_TMP"' EXIT
export HOME="$TASK_TMP/home" PATH="$TASK_TMP/bin:$PATH" TEST_SETUP_MARKER="$TASK_TMP/calls"
mkdir -p "$HOME/.codex" "$TASK_TMP/bin"
printf 'global config canary\n' > "$HOME/.codex/config.toml"
cat > "$TASK_TMP/bin/mise" <<'FIXTURE'
#!/bin/bash
set -eu
[[ "$1 $2 $3 $4 $5" == 'exec -- go build -o' ]]
cat > "$6" <<'BINARY'
#!/bin/bash
set -eu
[[ "$*" == '--check-codex-readiness' ]] || { echo "unexpected native CLI invocation" >&2; exit 2; }
echo native-check >> "$TEST_SETUP_MARKER"
BINARY
chmod +x "$6"
FIXTURE
cat > "$TASK_TMP/bin/codex-acp" <<'FIXTURE'
#!/bin/bash
exit 0
FIXTURE
for task_binary in opencode fx; do
  cat > "$TASK_TMP/bin/$task_binary" <<'FIXTURE'
#!/bin/bash
echo legacy-call >> "$TEST_SETUP_MARKER"
exit 2
FIXTURE
  chmod +x "$TASK_TMP/bin/$task_binary"
done
chmod +x "$TASK_TMP/bin/mise" "$TASK_TMP/bin/codex-acp"
"$TASK_ROOT/secretary" setup
"$TASK_ROOT/secretary" doctor
"$TASK_ROOT/secretary" node setup --standalone --server http://127.0.0.1:9999 --name codex-canary
"$TASK_ROOT/secretary" node doctor
[[ "$(cat "$HOME/.codex/config.toml")" == 'global config canary' ]]
[[ "$(cat "$TEST_SETUP_MARKER")" != *legacy-call* ]]
[[ ! -e "$HOME/.local/share/secretary/opencode-native-selection.json" ]]
[[ ! -e "$HOME/.local/share/secretary/node/data/opencode-native-selection.json" ]]
rg -q '^harness = "codex"$' "$HOME/.local/share/secretary/config.toml"
rg -q '"include_opencode": false' "$HOME/.local/share/secretary/node/config.json"
echo 'PASS: clean Codex setup/Doctor/standalone Node need no FX/OpenCode and preserve global config.'
