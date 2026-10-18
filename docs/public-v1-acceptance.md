# T18 — Public v1 acceptance ledger

This is a **release gate**, not a completion claim. Base: `bd4120b477aa469ae84f95b8127ace2df33e6eb1`; requirements: [#70](https://github.com/GitHubFoxy/secretary/issues/70), [#89](https://github.com/GitHubFoxy/secretary/issues/89). CI/fixture evidence cannot replace live native sessions, native OS installs or real Telegram/PWA devices.

## Acceptance matrix

| #89 criterion | Existing evidence / reusable slice | Mandatory live evidence / current gate |
| --- | --- | --- |
| Fresh macOS + Linux Server install with **one** harness | [T06 CLI](../.github/workflows/t06-terminal-cli.yml), [terminal installer](terminal-install.md), [install script](../scripts/install.sh) | **OPEN:** fresh normal-user installation on each OS, one authenticated harness alone, actual service start and doctor; record OS/version and command outcomes |
| Native Codex and Claude Code, Secretary → question Result → same Worker Follow-up → Result | [T01](https://github.com/GitHubFoxy/secretary/issues/72), [native T09 Codex resume CI](../.github/workflows/t09-real-resume.yml), [Claude native interactive prerequisite #19](https://github.com/GitHubFoxy/secretary/issues/19) | **OPEN:** both actual provider-backed runs; #19 remains open. For each record harness CLI/adapter version, session identity before and after follow-up, Result count, native same-turn steering or **explicit unsupported** and /q FIFO behavior. CLI smoke and mocks do not establish provider-backed success |
| Web + installable PWA and Telegram DM, General, Worker Topic | [PWA manual steps](quickstart.md#установка-web-как-pwa), [T15 documents](https://github.com/GitHubFoxy/secretary/issues/86), [T13 video](https://github.com/GitHubFoxy/secretary/issues/88) | **OPEN:** real browser install on supported device; real Bot API owner pairing + DM-alone; optional forum group General/Topics mirrored once; replay/reconnect with one canonical entry/action |
| Images, voice/audio, video, documents and published results across Server ↔ Node ↔ Web/Telegram | [T09](https://github.com/GitHubFoxy/secretary/issues/81), [T12 video](https://github.com/GitHubFoxy/secretary/issues/85), [T13](https://github.com/GitHubFoxy/secretary/issues/88), [T15](https://github.com/GitHubFoxy/secretary/issues/86) | **OPEN:** live byte/hash checks for originals, derivatives, transcript, time-coded frames, output download and Telegram receipt. Exercise configured limits, unsupported image capability, oversized video and processing failure |
| Real macOS, Linux and Windows Nodes | [Node deployment](node-deployment.md), [T17 Windows](https://github.com/GitHubFoxy/secretary/issues/79), [T09 resume](../.github/workflows/t09-real-resume.yml) | **OPEN:** on each real OS, enroll Node, dispatch genuine native Worker, reconnect, follow-up to same session; unavailable native session visibly fails with no unverified retry |
| Restart, replay, Artifact retention and central backup/restore | [T16](https://github.com/GitHubFoxy/secretary/issues/87), [Server runbook](always-on-runbook.md) | **OPEN:** production-like Server and Node restarts; replay once with same IDs; offline/uncertain side effects not blindly repeated; backup, restore to distinct Server, hash/download, owner isolation, and missing remote native session truthfully reported |
| Explicit capabilities, failures, exact tested versions | This ledger and linked docs | **OPEN:** attach signed-off per-run results below; record true unsupported functions and blockers, not inferred success |
| Fixtures separate from live | Existing repository CI and T06/T09 workflows | **OPEN until the full mandatory live matrix is evidenced**; GitHub CI only checks applicable automated fixtures |

## Live acceptance record (copy per attempt)

Do not commit credentials, tokens, usernames, real conversation text, provider traces or private file contents.

```text
Date/time UTC:
Repository exact SHA:
Executor / environment reference (private, not secrets):
Secretary Server OS/version, install method, service manager:
Node OS/version, local/remote:
Harness name, CLI version, adapter/SDK version, authorized: yes/no:
Web/PWA browser/version and device:
Telegram: DM-only or forum-group; Bot API exchange verified: yes/no:
Scenario: (criterion row and subcase)
Steps actually performed:
Evidence: redacted CI/job URL or owner-held recording/log location:
Task ID / Worker ID / Attempt count / native session IDs (redact if sensitive):
Result: PASS / FAIL / BLOCKED / NOT RUN
Observed failure and impact:
Retest exact SHA and date:
Reviewer / approval:
```

**Required procedure:**

1. Starting from a **clean owner** on actual macOS and Linux, install by the published terminal command. Test Codex-only and Claude-only installations separately, not a setup where both mask missing dependencies. Check `secretary doctor`, service operation, Web bootstrap login and persistent local Node pairing.
2. For both native harnesses, ask for a website, ensure the Worker returns a question as a terminal Result, answer from another surface, assert **same Worker and native session**, and check one Result per Attempt. Start an unrelated task concurrently. During a safe long-running Attempt send a unique direct steer and distinguish same-turn injection, unsupported steering, and interrupt/queued follow-up; send a separate `/q` and verify FIFO.
3. Install PWA in a real browser, re-open with owner cookie, exercise observer and disconnected state. Pair Telegram in DM alone, then optional forum group. Send input DM → General → Worker Topic and replay duplicate update; assert one canonical execution and proper owner-only authorization.
4. Send image, voice/audio, ordinary document and video in Web and Telegram. Verify byte-identical persisted originals, transcript links, configured sampling timestamp values, remote Node fetch, native visual context, published Worker output download/Telegram deliverability. Test oversize, conversion failures and incapable harness without a fake success.
5. On **real** macOS, Linux and Windows Nodes, run the same native dispatch and reconnect continuation. Interrupt Server/Node while Work is in-flight and verify explicit uncertain state instead of repeat execution.
6. Archive Worker and retrieve output, delete owner-selected Artifact, back up Server SQLite/config/memory/Artifact store and restore to a fresh instance. Compare integrity and Result/Artifact references. **Remote Node-native sessions are not in Server backups**; restore must not promise their recovery.

Gate policy: any `FAIL`, `BLOCKED` or `NOT RUN` in a mandatory row keeps #89 and Public v1 **open**. A green workflow, a built binary, a harness `--help` invocation or a fake-provider response never clears a live row. A failure must be linked to a follow-up ticket or corrected and retested on the exact candidate SHA.

## CI reliability note

The inherited `go-regression.yml` had a malformed Codex test invocation and omitted the actual platform npm package ID when provisioning the manifest. The T18 branch repairs these two CI-evidence blockers; that is **not** evidence of live provider authentication. Any separate baseline push-trigger workflow failure must be recorded separately from the four required PR checks.
