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
  'test -p 1 ./...' \
  'test -race -p 1 ./...' \
  'vet -p 1 ./...' \
  'build -p 1 ./...' \
  'npm test' \
  'node_modules/.bin/vite build' \
  '--outDir "$BUILD_TMP/dist"' \
  'clean-vite-assets.mjs "$BUILD_TMP/dist"' \
  'cmp -s' \
  'git diff --check' \
  'scripts/sex-cli-test.sh' \
  'scripts/node-deployment-test.sh' \
  'scripts/node-revoke-test.sh'; do
  grep -Fq -- "$command" "$GATE" || fail "gate does not run: $command"
done

for command in 'test -p 1 ./...' 'test -race -p 1 ./...' 'vet -p 1 ./...' 'build -p 1 ./...'; do
  [[ "$(grep -Fc "$command" "$GATE")" -eq 1 ]] || fail "gate must run exactly once: $command"
done
if grep -Fq 'npm run build:all' "$GATE" || grep -Fq 'git diff --quiet --' "$GATE" || ! grep -Fq 'BUILD_TMP="$(mktemp -d' "$GATE"; then
  fail "gate must preserve pre-existing edits to embedded assets"
fi

for marker in \
  'deterministic automated checks' \
  'manual real-harness proof' \
  'evidence ledger' \
  'UNAVAILABLE' \
  'MacBook Node' \
  'home server Node' \
  'default OpenCode v2' \
  'go test -p 1 ./...'; do
  grep -Fqi "$marker" "$DOC" || fail "documentation is missing: $marker"
done

for marker in \
  'ROOT=' \
  'GATE_RUN' \
  'evidence-ledger.md' \
  'BLOCKED' \
  'UNAVAILABLE' \
  'NOT RUN' \
  'Owner login or legacy migration needs separate approval.' \
  'default policy selects OpenCode v2' \
  'default OpenCode v2 and explicit fx, Claude Code, Codex adapters' \
  'all required matrix rows are PASS' \
  'never changes Ticket 15 status'; do
  grep -Fq "$marker" "$WIZARD" || fail "wizard is missing: $marker"
done
if grep -Fq 'default policy selects fx' "$WIZARD" || grep -Fq 'conditional OpenCode' "$WIZARD"; then
  fail "wizard has stale harness defaults"
fi

printf 'Phase 4 release gate contract test passed.\n'
