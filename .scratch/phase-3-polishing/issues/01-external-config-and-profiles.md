# External config and Profiles

Type: task
Status: ready-for-human
Blocked by:

## Work

Implement `config.toml`, external `secretary.md`, `worker.md` and `child-worker.md`, absolute skill paths, canonical tool allowlists, model aliases and immutable config versions. Compile effective Profiles with hashes and persist config diffs/events. Reload must validate before applying; active Workers retain their original profile version.

## Acceptance

- No Profile text is hardcoded in Go.
- Config validation reports path and line while old config remains active.
- Worker binding records effective profile, runtime, model, reasoning, tools and config version.

## Answer

Implemented in `internal/config`, `cmd/secretaryd`, `internal/app` and `internal/core`.

- First startup creates editable external `config.toml` and three Markdown Profiles under the data directory.
- TOML validation rejects invalid harnesses, missing model aliases, unsorted/non-canonical tool names and non-absolute skill paths.
- `SIGHUP` validates and atomically reloads config. Failed parse, validation or durable event recording leaves the previous snapshot active.
- SQLite stores compiled config versions, config change diffs and immutable Profile metadata on every new Worker binding.
- Verified with `go test ./...`, `go test -race ./...`, `go vet ./...`.
