# Codex and fx compatibility runtimes

Type: task
Status: ready-for-human
Blocked by: 01, 02, 03

## Work

Upgrade Codex and fx adapters to the shared harness contract. Materialize effective Profile as managed `AGENTS.md` plus bootstrap prompt. Normalize ACP activity, lifecycle, cancellation, permissions and MCP tool calls. Implement fx interrupt-and-continue Steering.

## Implementation notes

- `RuntimeRouter` selects Codex, fx, and OpenCode by immutable Profile runtime and fails explicitly for unavailable or unknown harnesses.
- Codex and fx share the ACP lifecycle, activity, cancellation, permission, MCP, and managed `AGENTS.md` path. fx steering cancels, waits for idle, records a durable follow-up Attempt, and starts the replacement prompt.
- `internal/node/harness_compat_test.go` runs the same compatibility assertions against both adapters.

## Acceptance

- Codex and fx pass the shared harness compatibility suite.
- Control Room identifies delivery as `workspace_instructions`.
- Unavailable harness causes explicit dispatch failure, never silent fallback.
