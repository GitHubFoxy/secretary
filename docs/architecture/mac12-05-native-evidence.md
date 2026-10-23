# MAC12-05 native Codex execution evidence

Status: blocked on a native adapter provenance contract; this is not an accepted correction.

The observed outer ACP tool call is a Codex dynamic tool named `exec`. Its `toolCallId` identifies the **outer** invocation only. In `agentclientprotocol/codex-acp` v2.2.2, `src/tool-calls/reporters/DynamicToolReporter.ts` exposes `item.status` and renders `item.contentItems` as textual ACP content. `src/app-server/v2/DynamicToolCallResponse.ts` contains `contentItems` and `success` for the outer dynamic tool. Neither reporter includes a typed nested invocation ID, nested `tools.exec_command` result, or its exit code. The observed outer call reports `completed` even though printed output describes `exit_code:7`.

The separately implemented `CommandReporter.ts` transports native `commandExecution` exit codes, but its item identity is not linked to a nested `tools.exec_command` inside `exec`. Reading `exit_code` from arbitrary text, including valid JSON, is spoofable and must not drive canonical Results.

Minimal required observability change: a trusted native nested execution record identifying the parent dynamic tool call, the nested tool invocation, typed exit status, completion ordering and explicit recovery relationship (or a native equivalent that can be proven). The record must come from the actual executor/adapter boundary, not the model or formatted text. Repeated/out-of-order event handling must be idempotent by native identity; explicit recovery requires an authenticated resolution event with the failed invocation identity. Native prompt success alone cannot clear it. This contract is not present in the pinned adapter.

The prior implementation was reverted because it produced false `failed/tool_execution_failed` Results for user data shaped as valid JSON. Acceptance still requires an authenticated exit-7 failure, explicit same-Attempt recovery, valid-JSON-as-data negative control, provider/cancel precedence and one durable canonical Result with native same-session continuity.

The separate `codex-resume` CI failure in run 39929705991/job 120334652561 is Go `t.TempDir` cleanup of `~/.codex/.tmp/plugins-clone-*/.git` while a plugin clone appears to be writing concurrently. No resume assertion failed in that log. This test is in `internal/nativeexec/codex_direct_linux_test.go`; the ACP-only patch does not call its nativeexec direct-execution path. Investigate process teardown/plugin clone lifecycle independently if reproduced; do not mask with cleanup retries.
