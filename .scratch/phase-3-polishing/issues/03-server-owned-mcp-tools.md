# Server-owned MCP tools

Type: task
Status: ready-for-human
Blocked by: 01, 02

## Work

Expose capability-scoped MCP tools to harness sessions. Secretary gets typed Task lifecycle and model catalog tools. Worker gets `spawn_subagent`. Child gets no lifecycle tools. Every invocation validates binding scope and creates events.

## Acceptance

- Secretary no longer shells out to `secretaryctl`.
- `available_models_for_worker` is only available to Secretary.
- Tool calls are visible in durable event history.

## Answer

Implemented the server-owned MCP stdio path.

- Added `cmd/secretary-mcp`, with role-specific handlers for `secretary`, `worker` and `child_worker`.
- Secretary tools use the existing capability-scoped `ctl.Service`; Worker exposes only `spawn_subagent`; Child exposes no tools.
- Added Worker capability rotation and authorization in SQLite. Worker MCP calls validate the Worker reference and capability before execution.
- Added audited `mcp.tool_call` and `mcp.tool_result` events.
- ACP `session/new` and `session/load` now receive per-session stdio MCP definitions with scoped environment values.
- `sex setup` builds the helper binary. The actual Child spawn callback is completed by ticket 04.
- Verified with `go test ./...`, `go test -race ./...`, `go vet ./...`.
