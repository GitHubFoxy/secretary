#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

GO="mise exec go@1.27.1 -- go"

echo "[1/6] deterministic test suite"
$GO test ./...
echo "[2/6] race suite"
$GO test -race ./...
echo "[3/6] vet"
$GO vet ./...
echo "[4/6] command builds"
$GO build ./cmd/...
echo "[5/6] embedded UI and source checks"
test -s web/index.html
test -s web/app.js
test -s web/app.css
test -s web/control-room/index.html
test -s web/control-room/app.js
test -s web/control-room/app.css
test -s web/src/App.svelte
test -s web/control-room/src/App.svelte
grep -q 'control-room' web/embed.go
zsh -n sex
grep -q '^Usage: sex' <(./sex)
grep -q -- '--debug' sex
grep -q 'KeepAlive' sex
git diff --check
echo "[6/6] sex CLI integration"
./scripts/sex-cli-test.sh

echo "Phase 3 release gate passed. Real harness matrix remains a manual run; see docs/phase3-release-gate.md."
