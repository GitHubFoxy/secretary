#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TMP="$(mktemp -d "${TMPDIR:-/tmp}/phase4-gofmt-test.XXXXXX")"
trap 'rm -rf "$TMP"' EXIT
mkdir -p "$TMP/scripts" "$TMP/internal/node"
cp "$ROOT/scripts/phase4-gofmt-check.sh" "$TMP/scripts/phase4-gofmt-check.sh"

(
  cd "$TMP"
  git init -q
  git config user.email gate-test@example.invalid
  git config user.name "Gate Test"
  printf 'package node\nfunc Legacy( ){}\n' > internal/node/legacy.go
  git add internal/node/legacy.go
  git -c commit.gpgsign=false commit -qm baseline
  base="$(git rev-parse HEAD)"
  git update-ref refs/remotes/origin/main "$base"

  printf 'package node\nfunc Changed( ){}\n' > internal/node/changed.go
  git add internal/node/changed.go
  git -c commit.gpgsign=false commit -qm changed

  if GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main PHASE4_GOFMT_BASE_REF= bash scripts/phase4-gofmt-check.sh > "$TMP/check.log" 2>&1; then
    printf 'gofmt PR scope accepted an unformatted changed file\n' >&2
    exit 1
  fi
  grep -Fq 'internal/node/changed.go' "$TMP/check.log"
  if grep -Fq 'internal/node/legacy.go' "$TMP/check.log"; then
    printf 'gofmt PR scope included an unchanged baseline file\n' >&2
    exit 1
  fi

  printf 'package node\n\nfunc Changed() {}\n' > internal/node/changed.go
  git add internal/node/changed.go
  git -c commit.gpgsign=false commit -qm formatted
  GITHUB_EVENT_NAME=pull_request GITHUB_BASE_REF=main PHASE4_GOFMT_BASE_REF= bash scripts/phase4-gofmt-check.sh > "$TMP/check.log" 2>&1

  if GITHUB_EVENT_NAME=workflow_dispatch PHASE4_GOFMT_BASE_REF= bash scripts/phase4-gofmt-check.sh > "$TMP/check.log" 2>&1; then
    printf 'standalone gofmt scope skipped existing unformatted Go source\n' >&2
    exit 1
  fi
  grep -Fq 'internal/node/legacy.go' "$TMP/check.log"

  if GITHUB_EVENT_NAME=workflow_dispatch PHASE4_GOFMT_BASE_REF=nonexistent bash scripts/phase4-gofmt-check.sh > "$TMP/check.log" 2>&1; then
    printf 'gofmt PR scope silently accepted a missing base ref\n' >&2
    exit 1
  fi
  grep -Fq 'Cannot resolve gofmt PR base ref' "$TMP/check.log"
)

printf 'Phase 4 gofmt scope contract test passed.\n'
