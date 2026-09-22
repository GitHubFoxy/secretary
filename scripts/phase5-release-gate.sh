#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GO=(mise exec go@1.27.1 -- go)
GOFMT=(mise exec go@1.27.1 -- gofmt)

run() {
  printf '+ '
  printf '%q ' "$@"
  printf '\n'
  "$@"
}

fail() {
  printf 'FAIL: %s\n' "$1" >&2
  exit 1
}

printf '%s\n' '[1/7] gofmt check (read-only)'
unformatted="$("${GOFMT[@]}" -l internal cmd)"
test -z "$unformatted" || fail "unformatted Go files: $unformatted"

printf '%s\n' '[2/7] deterministic Go tests'
run "${GO[@]}" test ./...

printf '%s\n' '[3/7] Go vet and command builds'
run "${GO[@]}" vet ./...
run "${GO[@]}" build ./cmd/...

printf '%s\n' '[4/7] Pi viewer TypeScript check (read-only)'
[[ -d phase-1/node_modules ]] || fail "phase-1/node_modules is missing, run: npm ci --ignore-scripts in phase-1"
phase1_diff_before="$(git diff -- phase-1 | shasum)"
(
  cd phase-1
  run npm run check:readonly
)
phase1_diff_after="$(git diff -- phase-1 | shasum)"
[[ "$phase1_diff_before" == "$phase1_diff_after" ]] || fail "check:readonly mutated phase-1, a release gate must only read the tree"

printf '%s\n' '[5/7] viewer documentation contract'
for doc in docs/always-on-runbook.md docs/pi-viewer-runbook.md docs/pi-viewer-release-note.md docs/phase5-release-gate.md; do
  [[ -f "$doc" ]] || fail "missing documentation file: $doc"
done
for scenario in "Server offline" "Node offline" "Tailscale unavailable" "Revoked credential"; do
  grep -q "^### $scenario\$" docs/pi-viewer-runbook.md || fail "pi-viewer-runbook is missing recovery scenario: $scenario"
done
grep -q 'Idempotency-Key' docs/always-on-runbook.md || fail "always-on-runbook does not document the Idempotency-Key mutation contract"
grep -q 'approve' docs/always-on-runbook.md || fail "always-on-runbook does not document the viewer approve flow"
grep -qi 'не является remote control' docs/pi-viewer-release-note.md || fail "release note lacks the explicit read-only disclaimer"
grep -q '~/.config/secretary/viewer-credential' docs/pi-viewer-runbook.md || fail "runbook does not document the credential file path"

grant="$(awk '/^## Documented grant$/ { found = 1; next } /^## / { found = 0 } found' docs/pi-viewer-release-note.md)"
[[ -n "$grant" ]] || fail "release note has no Documented grant section"
if printf '%s\n' "$grant" | grep -qE ':write|client:manage'; then
  fail "documented grant mentions write scopes"
fi
for scope in conversation:read worker:read approval:read; do
  printf '%s\n' "$grant" | grep -q "$scope" || fail "documented grant is missing $scope"
done

printf '%s\n' '[6/7] viewer privacy boundary tests'
run "${GO[@]}" test ./internal/webapi -run 'TestWorkerSurfaceStrictForClientCredential|TestReadOnlyCredentialIsRefusedOutsideTheReadSurface|TestApprovalListReturnsAllowlistedConversationApprovals' -count=1

printf '%s\n' '[7/7] whitespace check'
git diff --check

printf '%s\n' 'phase5 release gate: PASS'
