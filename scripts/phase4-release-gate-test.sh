#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GATE="$ROOT/scripts/phase4-release-gate.sh"
DOC="$ROOT/docs/phase4-release-gate.md"

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

[[ -x "$GATE" ]] || fail "Phase 4 gate is not executable"
[[ -f "$DOC" ]] || fail "Phase 4 gate documentation is missing"
bash -n "$GATE" || fail "Phase 4 gate has invalid shell syntax"

for command in \
  'test ./...' \
  'test -race' \
  'vet ./...' \
  'build ./cmd/...' \
  'npm test' \
  'npm run build:all' \
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

printf 'Phase 4 release gate contract test passed.\n'
