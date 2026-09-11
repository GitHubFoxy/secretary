# Child Worker tree

Type: task
Status: ready-for-human
Blocked by: 03

## Work

Implement server-owned child bindings, parent-child links, fan-out limits, parent Result routing and lifecycle recovery. Parent Worker can create up to four children per Attempt. Child cannot create descendants.

## Acceptance

- Parent and Child attempts survive restart and appear as a durable tree.
- Child Result is delivered to parent, not Personal Conversation.
- Control Room can reconstruct the entire tree and every terminal state.

## Answer

Implemented durable bounded Child Workers.

- Child Tasks carry parent Task, parent Attempt and stable child index metadata.
- Parent Attempts can create at most four children; descendants are rejected by role because Child MCP has no tools.
- Child bindings retain parent binding metadata and Child Results are stored without leaking a `worker_result` entry into Personal Conversation.
- Completed Child Tasks close and archive automatically. `TaskDetails` recursively returns the observable tree.
- Worker MCP `spawn_subagent` creates a child Task, waits for its terminal Result and returns the result to the parent model.
- Dispatcher selects `child_worker` profile/MCP role for child Tasks and does not issue a Worker lifecycle capability.
- Verified with `go test ./...`, `go test -race ./...`, `go vet ./...`.
