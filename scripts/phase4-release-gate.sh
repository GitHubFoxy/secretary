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

printf '%s\n' '[1/10] release-gate contract test'
run ./scripts/phase4-release-gate-test.sh

printf '%s\n' '[2/10] gofmt'
unformatted="$(find internal cmd web -type f -name '*.go' -print0 | xargs -0 "${GOFMT[@]}" -l)"
[[ -z "$unformatted" ]] || {
  printf 'Unformatted Go files:\n%s\n' "$unformatted" >&2
  exit 1
}

printf '%s\n' '[3/10] deterministic Go tests'
run "${GO[@]}" test ./...

printf '%s\n' '[4/10] Go race tests'
run "${GO[@]}" test -race -p 1 ./...

printf '%s\n' '[5/10] Go vet and command builds'
run "${GO[@]}" vet ./...
run "${GO[@]}" build ./cmd/...

printf '%s\n' '[6/10] frontend dependency lock and tests'
command -v npm >/dev/null || { printf 'npm is required for the Phase 4 gate\n' >&2; exit 1; }
(
  cd web
  run npm ci --ignore-scripts --no-audit --no-fund
  run npm test
)

printf '%s\n' '[7/10] production frontend builds and embedded assets'
tracked_assets=(
  web/index.html web/app.js web/app.css
  web/control-room/index.html web/control-room/app.js web/control-room/app.css
)
git diff --quiet -- "${tracked_assets[@]}" || {
  printf 'tracked embedded assets are dirty before build\n' >&2
  git diff -- "${tracked_assets[@]}"
  exit 1
}
(
  cd web
  run npm run build:all
)
git diff --quiet -- "${tracked_assets[@]}" || {
  printf 'frontend build changed tracked embedded assets\n' >&2
  git diff -- "${tracked_assets[@]}"
  exit 1
}
for asset in \
  web/index.html web/app.js web/app.css \
  web/control-room/index.html web/control-room/app.js web/control-room/app.css; do
  test -s "$asset" || { printf 'missing or empty embedded asset: %s\n' "$asset" >&2; exit 1; }
done
grep -Eq '^//go:embed .*index\.html .*app\.js .*app\.css' web/embed.go
grep -q 'control-room/index\.html' web/embed.go
run "${GO[@]}" test ./web

printf '%s\n' '[8/10] profile-specific integration tests'
run ./scripts/sex-cli-test.sh
run ./scripts/node-deployment-test.sh
run ./scripts/node-revoke-test.sh

printf '%s\n' '[9/10] final Go and embed verification'
run "${GO[@]}" test ./...
run "${GO[@]}" vet ./...

printf '%s\n' '[10/10] whitespace and diff checks'
run git diff --check

printf '%s\n' 'Phase 4 deterministic release gate passed. Real harness proof remains manual; see docs/phase4-release-gate.md.'
