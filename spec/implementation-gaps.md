# Secretary Developer v1 — Implementation Gap Backlog

> **Status:** Proposed implementation sequencing, not a claim that code is missing/complete. Product authority: [canonical spec](spec.md); interview decisions: [grilling Q&A](grilling-2026-10-17.md). The associated [specification issue](https://github.com/GitHubFoxy/secretary/issues/70) is a handoff, **not** a mandate to implement everything in one PR.
>
> **Rule:** For every ticket, first locate and reuse working Core, adapter and test seams; verify actual behavior before declaring a gap. No Core rewrite.

## P0 — Critical functional path and reliability

| ID | Action | Existing building blocks | Exit criteria |
| --- | --- | --- | --- |
| G01 | Codex/Claude end-to-end acceptance and missing native lifecycle fixes | Current Node runtimes, Worker lifecycle, Phase-5 work and [#19](https://github.com/GitHubFoxy/secretary/issues/19) | Each harness individually can start Worker, ask as Result, resume from user Follow-up, steer or visibly reject, recover after restart |
| G02 | Worker question→answer routing and unrelated topic switching | Secretary context, Worker binding, Follow-up, Result delivery, [#37](https://github.com/GitHubFoxy/secretary/issues/37) | Answer from main chat reliably resumes existing Worker, doesn't spawn a new one; uncertain recipient is clarified |
| G03 | Failure/replay gates | Core recovery, Node command receipts, Web/Telegram event streams | No duplicated Result on replay, no blind unknown-effect retry, visible offline/unsupported messages |

## P1 — User channels and setup

| ID | Action | Existing building blocks | Exit criteria |
| --- | --- | --- | --- |
| G04 | Telegram DM ↔ Group General true two-way sync; owner identity | Telegram forum topic mapping, outbox, some Result mirroring | Owner's canonical messages/reports reflected once between DM/General; no mirror feedback loop; Topics stay Worker-specific |
| G05 | PWA installability using existing Web | Svelte/Vite Web | Installable mobile/desktop PWA points to same Secretary Server, preserves Web-session behavior |
| G06 | Terminal-only setup/config and reliable service management | Existing secretary setup, doctor, service CLI | Fresh owner can install/configure Codex **or** Claude plus local Node and optional Telegram entirely in terminal |

## P1 — Multimodal and file workflows

| ID | Action | Existing building blocks | Exit criteria |
| --- | --- | --- | --- |
| G07 | Central Artifact model, local storage, secure upload/download and Node transfer | Core Result artifact references; Node outbound transport | Stable Artifact IDs and byte-accurate Server-hosted originals, access controlled by owner/Node identity |
| G08 | Image/video preprocessing and config knobs | Media research, native frame/image tooling | Configurable timestamped frames, original video retained, limits reject with warning, failure explicit |
| G09 | Audio artifacts and transcript linkage | Existing Telegram voice ASR and formatter | Original audio + Markdown transcript stored and usable from Secretary/Worker, including remote Node |
| G10 | Explicit Worker/Secretary file publishing and Web/Telegram delivery | Worker callbacks, Result delivery | Generated document/zip/image can publish through Node → Server → owner, without scanning a workspace |
| G11 | Artifact retention/backup + asynchronous processing | Server data directory, runbook | Media prep doesn't stall unrelated chat; closing Worker retains files; delete/backup/restore verified |

## P2 — Product polish and public acceptance

| ID | Action | Existing building blocks | Exit criteria |
| --- | --- | --- | --- |
| G12 | Simple Markdown memory migration | Existing user.md and user-document persistence | memory.md and user_preferences.md supported, old content preserved, writes only on explicit user request |
| G13 | OS/harness support matrix and first-user release | Current tests, CI, CLI, Web/Telegram | Live Codex+Claude acceptance and macOS/Linux server, Windows/macOS/Linux Node checks, clear unsupported-case messages |

## Testing seam

The **highest reusable seam** is a single black-box Server + Channel adapter + Execution Node integration environment with a deterministic fake native harness and file-transfer fixtures. Reuse existing Core/Node/Telegram/Web API/E2E tests; add live acceptance for native harnesses and platforms separately. Tests should assert user-visible entries, exact Result/Worker identity, artifact integrity, clear errors, persistence, and no duplicated execution.

## Explicit non-goals

Do **not** create required Projects, Child Workers, a new approvals engine, read-only mode, a dedicated model router, new multiuser Developer accounts or an independent PWA backend. Do not delete legacy persisted structures as a shortcut.

## Scheduling

This is an ordered gap map, not an invented estimate. Create small implementation issues from these slices with one end-to-end acceptance seam per slice; reuse existing issues when scope overlaps. Closing the umbrella spec issue should require that the v1 release criteria are met or explicitly replanned.
