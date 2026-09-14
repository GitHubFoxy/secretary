#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$ROOT/scripts/phase4-release-gate.sh"
DOC="$ROOT/docs/phase4-release-gate.md"
WIZARD="$ROOT/scripts/phase4-manual-acceptance-wizard.sh"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

[[ -x "$GATE" ]] || fail "Phase 4 gate is not executable"
[[ -f "$DOC" ]] || fail "Phase 4 gate documentation is missing"
[[ -x "$WIZARD" ]] || fail "manual acceptance wizard is not executable"
bash -n "$GATE" || fail "Phase 4 gate has invalid shell syntax"
bash -n "$WIZARD" || fail "manual acceptance wizard has invalid shell syntax"

for command in \
  'test ./...' \
  'test -race' \
  'vet ./...' \
  'build ./cmd/...' \
  'npm test' \
  'npm run build:all' \
  'git diff --quiet --' \
  'frontend build changed tracked embedded assets' \
  'git diff --check' \
  'scripts/sex-cli-test.sh' \
  'scripts/node-deployment-test.sh' \
  'scripts/node-revoke-test.sh'; do
  grep -Fq "$command" "$GATE" || fail "gate does not run: $command"
done

for marker in \
  'deterministic automated checks' \
  'manual real-harness proof' \
  'evidence ledger' \
  'blocked/unavailable' \
  'MacBook Node' \
  'home server Node' \
  'OpenCode'; do
  grep -Fqi "$marker" "$DOC" || fail "documentation is missing: $marker"
done

for marker in \
  'ROOT=' \
  'GATE_RUN' \
  'evidence-ledger.md' \
  'BLOCKED' \
  'NOT RUN' \
  'never changes Ticket 15 status'; do
  grep -Fq "$marker" "$WIZARD" || fail "wizard is missing: $marker"
done

printf 'Phase 4 release gate contract test passed.\n'
