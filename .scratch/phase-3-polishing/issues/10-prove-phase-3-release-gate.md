# Prove Phase 3 release gate

Type: task
Status: ready-for-human
Blocked by: 04, 05, 06, 07, 08, 09

## Work

Add deterministic fake-harness tests and manual real acceptance scripts. Prove User UI, Control Room, config reload/versioning, model selection, children, lifecycle recovery, logs, retention and launcher. Run the complete runtime matrix for installed Codex, fx and OpenCode.

## Implementation notes

- `internal/node/harness_compat_test.go` provides deterministic Codex/fx fake-harness coverage. Existing config, MCP, child-tree, observer, and OpenCode tests cover the remaining local lifecycle paths.
- `scripts/phase3-release-gate.sh` passed `go test ./...`, `go test -race ./...`, `go vet ./...`, command builds, embedded asset checks, and `git diff --check`.
- `npm ci` followed by `npm run build:all` passed for both Svelte/Tailwind entrypoints; generated assets were embedded and checked again.
- `docs/phase3-release-gate.md` records the installed Codex, fx, and OpenCode manual matrix plus User UI and debug Control Room flows.

## Acceptance

- All listed release-gate flows have automated or recorded real proof.
- A missing/unavailable harness is reported explicitly.
- `go test ./...`, `go test -race ./...` and `go vet ./...` pass.
