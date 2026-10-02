# 20 Do not treat ACP progress titles as tool names

Type: task
Status: ready-for-agent

## Work

`fx` sends ACP activity whose `title` is an action/progress label, not necessarily a tool name. The adapter currently turns values such as `Running`, `Waiting for` and `Reading` into tool events, producing messages like `Worker запускает инструмент Waiting for`.

Map ACP status, tool identity and lifecycle fields separately. Emit a tool-started or tool-finished event only when ACP data identifies an actual tool and its state. If an update contains only a progress title, show a generic progress status or suppress it; do not invent a command name. Repeated status updates must not be presented as evidence of separate tool invocations.

## Acceptance

- `Running`, `Waiting for` and `Reading` are never displayed as tool names by themselves.
- Real tool events use the actual available tool identity and correct lifecycle state.
- Status-only events do not claim to identify a command or prove that a tool started or finished.
- Repeated equivalent progress updates are deduplicated or coalesced.
- Tests cover status-only titles, valid tool lifecycle data, repeats and missing metadata.
