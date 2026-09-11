# Event log and retention

Type: task
Status: ready-for-human
Blocked by: 01

## Work

Add normalized durable runtime events and rotating raw ACP JSONL logs. Record lifecycle, prompts, assembled text, tool states, errors, profile delivery, config versions, auto-approved permissions and child transitions. Add retention by age and size.

## Acceptance

- Events are queryable in order without storing individual streaming tokens.
- Raw protocol logs are linked to runtime/session and survive restart.
- Default retention is 30 days and 1 GiB; config can set `forever`.

## Answer

Implemented normalized durable events, raw ACP JSONL capture and bounded log rotation.

- SQLite `events` stores ordered lifecycle events with Worker, Attempt, session and JSON payload.
- Dispatch, active Attempt and terminal Result create normalized events.
- ACP writes inbound and outbound raw JSONL to per-Worker rotating files under the durable data directory.
- Startup prunes raw ACP logs older than 30 days or beyond 1 GiB.
- Verified with `go test ./...`, `go test -race ./...`, `go vet ./...`.
