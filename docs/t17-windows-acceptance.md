# T17 Windows Execution Node — acceptance evidence

Issue: [#79](https://github.com/GitHubFoxy/secretary/issues/79). This page distinguishes Windows **host** execution from authenticated **native provider** acceptance; fixture success alone cannot close the release gate.

## Automated Windows host gate

- Workflow: [Windows Execution Node acceptance](../.github/workflows/windows-node.yml), `windows-latest` (actual Windows VM).
- Builds `secretary-node.exe` and runs Windows-specific ACP child-process Start + native session ID Resume tests.
- Runs real Node WebSocket pairing/reconnect, late terminal Result ACK/deduplication, and stale socket / offline transition tests.
- Windows host run: **PASS**, 17 October 2026, [GitHub Actions #39045653404](https://github.com/GitHubFoxy/secretary/actions/runs/39045653404) on real `windows-latest`; Windows `.exe` build, configuration, Windows ACP native child process Start/Resume and protocol reconnect tests passed at revision `639849fecf1ddf529b5b2bd34b84094e6e23e50a`. Corresponding [full Go tests #39045653391](https://github.com/GitHubFoxy/secretary/actions/runs/39045653391) and [Linux regression #39045653389](https://github.com/GitHubFoxy/secretary/actions/runs/39045653389) passed.
- Native harness used: local in-process ACP protocol fixture **spawned as a Windows process**. This verifies host/process compatibility and transport semantics, **not** authenticated Codex/Claude model execution.

## Authenticated native harness manual gate — BLOCKED (17 October 2026)

**Status: NOT RUN / BLOCKED — P1 acceptance gate remains open.** This work session has GitHub repository access and a GitHub Actions Windows runner, but **no access to a persistent owner-controlled Windows host with authenticated Codex/Claude credentials, and no reachable owner-controlled macOS/Linux Secretary Server**. Public CI secrets/providers are not available for this gate. Simulated ACP child processes are not an authenticated model call and cannot prove cross-host live resume. **No real native session ID, Worker ID, Attempt ID or end-to-end Result exists to record; do not manufacture these.**

Unblock by running the Windows setup documented in [Node deployment](node-deployment.md) against an actual macOS/Linux Server, authorizing Codex or Claude on Windows, then recording these observations with timestamps and minimally-redacted IDs: online Node inventory, Task → single Result, persisted native session ID, Node offline/online without re-pairing, Follow-up → same native session ID and new Result, plus missing-binary / interrupted-execution errors. Include Server revision and harness versions. Credentials, auth stores and prompts containing secrets must not be uploaded.

### Pending manual evidence (no identifiers observed)



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
