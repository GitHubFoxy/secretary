#!/bin/bash
set -euo pipefail
TASK_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
TASK_TMP="$(mktemp -d)"
trap 'rm -rf "$TASK_TMP"' EXIT
export TASK_TMP HOME="$TASK_TMP/home" PATH="$TASK_TMP/bin:$PATH" TEST_SETUP_MARKER="$TASK_TMP/calls"
mkdir -p "$HOME/.codex" "$TASK_TMP/bin"
printf 'global config canary\n' > "$HOME/.codex/config.toml"
cat > "$TASK_TMP/bin/mise" <<'FIXTURE'
#!/bin/bash
set -eu
[[ "$1 $2 $3 $4 $5" == 'exec -- go build -o' ]]
cat > "$6" <<'BINARY'
#!/bin/bash
set -eu
case "$*" in
  --check-codex-readiness)
    echo native-check >> "$TEST_SETUP_MARKER" ;;
  *--codex-pair-action=install*)
    echo native-pair-install >> "$TEST_SETUP_MARKER" ;;
  *--codex-pair-action=env*)
    printf 'CODEX_PATH=%s/bin/codex\nSECRETARY_ACP_COMMAND=%s/bin/codex-acp\n' "$TASK_TMP" "$TASK_TMP" ;;
  *--codex-pair-action=readiness*)
    echo native-pair-readiness >> "$TEST_SETUP_MARKER" ;;
  *--edit-deployment-config*)
    echo node-edit >> "$TEST_SETUP_MARKER" ;;
  *) echo "unexpected native CLI invocation: $*" >&2; exit 2 ;;
esac
BINARY
chmod +x "$6"
FIXTURE
cat > "$TASK_TMP/bin/codex" <<'FIXTURE'
#!/bin/bash
set -eu
case "$*" in
  "login status") exit 0 ;;
  "--version") echo "codex-cli 0.163.0" ;;
  *) exit 2 ;;
esac
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
chmod +x "$TASK_TMP/bin/mise" "$TASK_TMP/bin/codex" "$TASK_TMP/bin/codex-acp"
"$TASK_ROOT/secretary" setup
"$TASK_ROOT/secretary" doctor
"$TASK_ROOT/secretary" node setup --standalone --server http://127.0.0.1:9999 --name codex-canary
"$TASK_ROOT/secretary" node doctor
[[ "$(cat "$HOME/.codex/config.toml")" == 'global config canary' ]]
[[ "$(cat "$TEST_SETUP_MARKER")" != *legacy-call* ]]
grep -q 'native-pair-install' "$TEST_SETUP_MARKER"
grep -q 'native-pair-readiness' "$TEST_SETUP_MARKER"
[[ ! -e "$HOME/.local/share/secretary/opencode-native-selection.json" ]]
[[ ! -e "$HOME/.local/share/secretary/node/data/opencode-native-selection.json" ]]
grep -Eq '^harness = "codex"$' "$HOME/.local/share/secretary/config.toml"
grep -Eq '"include_opencode": false' "$HOME/.local/share/secretary/node/config.json"
echo 'PASS: clean Codex setup/Doctor/standalone Node need no FX/OpenCode and preserve global config.'
