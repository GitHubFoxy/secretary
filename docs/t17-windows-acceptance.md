# T17 Windows Execution Node — acceptance evidence

Issue: [#79](https://github.com/GitHubFoxy/secretary/issues/79). This page distinguishes Windows **host** execution from authenticated **native provider** acceptance; fixture success alone cannot close the release gate.

## Automated Windows host gate

- Workflow: [Windows Execution Node acceptance](../.github/workflows/windows-node.yml), `windows-latest` (actual Windows VM).
- Builds `secretary-node.exe` and runs Windows-specific ACP child-process Start + native session ID Resume tests.
- Runs real Node WebSocket pairing/reconnect, late terminal Result ACK/deduplication, and stale socket / offline transition tests.
- CI run URL and pass/fail: **pending first pull request run**.
- Native harness used: local in-process ACP protocol fixture **spawned as a Windows process**. This verifies host/process compatibility and transport semantics, **not** authenticated Codex/Claude model execution.

## Authenticated native harness manual gate (not yet performed)

Prerequisites: Windows host, current supported native Codex + codex-acp (or Claude Code) installed and authenticated on the Node; reachable real Secretary Server running on macOS or Linux; private pairing token; a registered Project/workspace if selected. Never commit credentials, prompts with secrets or native auth files.

Record before closing #79:

| Check | Required evidence | Status |
| --- | --- | --- |
| Windows host and macOS/Linux Server pair | Windows version, Node binary SHA, Server commit and successful online inventory | Pending |
| Real native Task | Selected harness/version/model, Worker and Attempt IDs, **one** Server Result | Pending |
| Disconnect/reconnect at idle | Node offline then online, same identity without a second pairing | Pending |
| Follow-up same native session | Durable runtime session ID before/after (redact sensitive details), one new Result | Pending |
| Missing dependency / unsupported resume | Explicit error captured, no substituted harness or replay | Pending |
| Interrupted active Attempt | Offline/uncertain state, no blind Task restart | Pending |

**Do not mark T17 fully accepted** until the live Windows-native Task and Follow-up are recorded here with date, host, harness, Server revision and logs/evidence location. Windows CI alone is insufficient.
