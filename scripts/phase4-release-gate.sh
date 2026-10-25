#!/usr/bin/env bash
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"

MISE=(mise exec go@1.27.1 --)
GO=("${MISE[@]}" go)
GOFMT=("${MISE[@]}" gofmt)

run() {
  printf '+ '
  printf '%q ' "$@"
  printf '\n'
  "$@"
}

printf '%s\n' '[1/9] release-gate contract test'
run ./scripts/phase4-release-gate-test.sh

printf '%s\n' '[2/9] gofmt'
run bash ./scripts/phase4-gofmt-check.sh

printf '%s\n' '[3/9] deterministic Go tests'
run "${GO[@]}" test -p 1 ./...

printf '%s\n' '[4/9] Go race tests'
run "${GO[@]}" test -race -p 1 ./...

printf '%s\n' '[5/9] Go vet and all-package build'
run "${GO[@]}" vet -p 1 ./...
run "${GO[@]}" build -p 1 ./...

printf '%s\n' '[6/9] frontend dependency lock and tests'
command -v npm >/dev/null || { printf 'npm is required for the Phase 4 gate\n' >&2; exit 1; }
(
  cd web
  run npm ci --ignore-scripts --no-audit --no-fund
  run npm test
)

printf '%s\n' '[7/9] production frontend builds and embedded assets'
BUILD_TMP="$(mktemp -d "${TMPDIR:-/tmp}/phase4-frontend.XXXXXX")"
trap 'rm -rf "$BUILD_TMP"' EXIT
(
  cd web
  run ./node_modules/.bin/vite build --outDir "$BUILD_TMP/dist"
  run node scripts/clean-vite-assets.mjs "$BUILD_TMP/dist"
  (
    cd control-room
    run ../node_modules/.bin/vite build --config vite.config.js --outDir "$BUILD_TMP/dist-control"
    run node ../scripts/clean-vite-assets.mjs "$BUILD_TMP/dist-control"
  )
)
built_assets=(
  "$BUILD_TMP/dist/index.src.html"
  "$BUILD_TMP/dist/app.js"
  "$BUILD_TMP/dist/app.css"
  "$BUILD_TMP/dist-control/index.src.html"
  "$BUILD_TMP/dist-control/app.js"
  "$BUILD_TMP/dist-control/app.css"
)
embedded_assets=(
  web/index.html web/app.js web/app.css
  web/control-room/index.html web/control-room/app.js web/control-room/app.css
)
asset_equality_required=1
if [[ "${GITHUB_EVENT_NAME:-}" == "pull_request" ]]; then
  base_ref="refs/remotes/origin/${GITHUB_BASE_REF:?GITHUB_BASE_REF is required}"
  git rev-parse --verify -q "${base_ref}^{commit}" >/dev/null || { printf 'Cannot resolve asset PR base: %s\n' "$base_ref" >&2; exit 1; }
  merge_base="$(git merge-base "$base_ref" HEAD)" || exit 1
  changed_assets="$(git diff --name-only "$merge_base" HEAD -- web package.json package-lock.json vite.config.js)" || exit 1
  if [[ -z "$changed_assets" ]]; then
    asset_equality_required=0
    printf 'PR asset equality: skipped for unchanged web/build inputs; production builds remain mandatory\n'
  fi
fi
for index in 0 1 2 3 4 5; do
  built="${built_assets[$index]}"
  embedded="${embedded_assets[$index]}"
  test -s "$built" || { printf 'missing or empty production asset: %s\n' "$built" >&2; exit 1; }
  test -s "$embedded" || { printf 'missing or empty embedded asset: %s\n' "$embedded" >&2; exit 1; }
  if (( asset_equality_required )); then
    cmp -s "$built" "$embedded" || {
    printf 'embedded asset differs from production build: %s\n' "$embedded" >&2
    exit 1
    }
  fi
done
grep -Eq '^//go:embed .*index\.html .*app\.js .*app\.css' web/embed.go
grep -q 'control-room/index\.html' web/embed.go
run "${GO[@]}" test ./web

printf '%s\n' '[8/9] profile-specific integration tests'
run ./scripts/secretary-cli-test.sh
run ./scripts/node-deployment-test.sh
run ./scripts/node-revoke-test.sh

printf '%s\n' '[9/9] whitespace and diff checks'
run git diff --check

printf '%s\n' 'Phase 4 deterministic release gate passed. Real harness proof remains manual; see docs/phase4-release-gate.md.'
