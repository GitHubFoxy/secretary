# Secretary — Canonical Product Specification

> **Status:** Approved product direction (grilling, 17 October 2026). **Implementation:** mixed; the desired contract is not a claim that features already ship.
>
> **Authority:** This is the primary product-goal specification. See [Grilling decisions](grilling-2026-10-17.md), [original author spec](old_spec.md), and [references](references/). The original and references remain unchanged. [GLOSSARY](../GLOSSARY.md) owns terms; [runtime contracts](../docs/architecture/runtime-contracts.md) describe present implementation. Durable decisions are recorded in [ADR-0001](../docs/adr/0001-one-conversation-and-worker-lifecycle.md), [ADR-0002](../docs/adr/0002-native-harnesses-and-execution-authority.md), [ADR-0003](../docs/adr/0003-central-artifact-store.md), and [ADR-0004](../docs/adr/0004-two-product-offerings.md), and [ADR-0005](../docs/adr/0005-channel-projections-and-owner-identity.md).
>
> **Status vocabulary:** Required v1 = release gate; Current = exists in code/docs but not necessarily live accepted; Planned = not yet proven ready; Later = beyond Public v1; Rejected = deliberately excluded. Requirement status is separate from implementation status.

## Problem Statement

People lose task context, conversation continuity and results when switching among chat channels, native AI harness sessions and machines. They should not have to relay decisions to each Worker or dig results out of isolated logs. Developers with existing Codex/Claude Code access want a simple persistent assistant. Eventually, nontechnical customers should purchase a working personal assistant without understanding models or servers.

## Solution

Secretary is **one personal assistant with one durable Personal Conversation**. It usually delegates substantive work and research to persistent Workers, automatically choosing a supported harness and Execution node. Workers may ask questions through ordinary Results; Secretary routes subsequent answers as Follow-ups to that same Worker. Results and files return to the common conversation. Users may simultaneously continue other topics or directly steer a Worker.

**Developer Edition** is open-source, one-owner, self-hosted and uses the owner's own authorized native harness access. **Consumer Edition** is a later paid experience using independently provided inference/compute and minimal technical setup; procurement and provider choices are outside this spec.

## User Stories

1. As the owner, I want one conversation across Web/PWA and Telegram, so I never move context manually.
2. As the owner, I want trivial questions answered directly and substantive work delegated, so Secretary remains responsive.
3. As the owner, I want to ask "create a website" and receive clarification from a Worker, so requirements evolve naturally.
4. As the owner, I want to answer the Worker's question in the common chat, so it continues the same Task.
5. As the owner, I want to switch topics while a Worker waits or runs, so work can proceed independently.
6. As the owner, I want existing Workers reused when relevant, so their native session context persists.
7. As the owner, I want ambiguous routing clarified, so instructions reach the right Worker.
8. As the owner, I want to override automatically selected harness and Node, so I can use specialized hardware.
9. As the owner, I want Secretary's harness configured separately from Worker harnesses, so they need not match.
10. As the owner, I want both Codex and Claude Code supported without requiring both installed, so one provider is enough.
11. As the owner, I want Workers to run full-access by default, so there is no duplicate permission framework.
12. As the owner, I want direct Worker messages and an observer, so I can inspect and influence native work.
13. As the owner, I want active instructions to steer and /q to queue, so message intent is clear.
14. As the owner, I want unsupported steering reported rather than silently queued, so actions are predictable.
15. As the owner, I want Worker Results delivered once in my main conversation, so I see progress and answers.
16. As the owner, I want Worker questions to be ordinary Results, so no extra waiting protocol is needed.
17. As the owner, I want closed/archived Workers to keep history, so completed work can be found later.
18. As the owner, I want an interactive terminal installer, so setup requires no browser or manual TOML editing.
19. As the owner, I want a background service with simple management commands, so I can operate Secretary easily.
20. As the owner, I want editable memory.md and user_preferences.md files, so I control explicit memories.
21. As the owner, I want durable SQLite conversation and Worker state, so a restart does not erase work.
22. As the owner, I want the Telegram bot usable in DM alone, so a group is optional.
23. As the owner, I want an optional group with General and Worker Topics, so I can inspect dedicated Workers.
24. As the owner, I want DM and group General mirrored without triggering duplicate model turns, so views stay consistent.
25. As the owner, I want only my paired Telegram identity to command the system, so group membership grants no authority.
26. As the owner, I want Web installable as a PWA, so I can use Secretary on phone and desktop.
27. As the owner, I want to send images to Secretary or Workers, so I can ask visual questions.
28. As the owner, I want to send video with configurable timestamped frame sampling, so agents can inspect it.
29. As the owner, I want video originals and optionally audio/transcripts retained, so I can inspect more than samples.
30. As the owner, I want voice/audio originals and transcripts stored together, so context is not lost.
31. As the owner, I want to upload normal documents and files, so a Worker can manipulate them.
32. As the owner, I want remote Nodes to fetch Artifacts by ID, so local filesystem paths do not break.
33. As the owner, I want Secretary and Workers to publish files back to my chat, so results are usable.
34. As the owner, I want oversized files rejected with clear warnings, so they are not truncated secretly.
35. As the owner, I want media processing failures shown accurately, so an absent frame is not claimed analyzed.
36. As the owner, I want file processing asynchronous, so large media does not block unrelated chat.
37. As the owner, I want Artifacts retained until explicit deletion, so archiving Workers does not destroy them.
38. As the owner, I want backup and restore for central data and files, so I can recover from machine failure.
39. As the owner, I want a disconnected Node not to cause blind retries, so uncertain effects are not repeated.
40. As the owner, I want macOS/Linux Secretary Server and macOS/Linux/Windows Nodes, so I can use my machines.
41. As the owner, I want Web access via existing bootstrap-code sessions, so I need no new Secretary account.
42. As a future nontechnical customer, I want to buy Secretary directly and start chatting, so I avoid model setup.
43. As a future Consumer customer, I want isolated personal data, so other customers never share my conversation.
44. As an open-source contributor, I want release acceptance gates and ADRs, so I can implement the right things.

## Implementation Decisions

### Conversation, identity, dispatch — Required v1

- Exactly **one configured owner (Person)** and **one ordered Personal Conversation** in Developer Edition. No organization, team, or multiuser product account.
- Secretary server owns Conversation entries, Task, Worker binding, Attempt, terminal Result, canonical Artifact metadata and context reconstruction. Channel adapters merely accept input and project canonical state; Execution nodes run Workers.
- Secretary answers very simple questions directly, but **delegates substantial research, extended discussion and task execution**. Worker decides whether current sources/web/tool use is necessary.
- On initial Dispatch, supply relevant prior conversation and decisions. For user answers and continuing work, Secretary routes Follow-ups contextually to the existing Worker and clarifies if routing is ambiguous.
- Worker may return a **question as the terminal Result of its Attempt**. This completes the Attempt, **not the Task**; the next answer creates a new Turn/Attempt for the same Worker. The Worker remains available even after success until explicit closure/archive.
- User can start unrelated work while other Workers are active or awaiting an answer.
- Default Worker Result is placed verbatim in the Personal Conversation, without an automatic second Secretary LLM response. Per-Worker summary/supervision is optional/later.
- Deduplicate inbound events, Worker actions and terminal Results by stable server-owned identities, never by matching text.
- **Projects are not required** for users or Tasks. Retain existing Project policies/mappings as optional developer compatibility without expanding mandatory UX.

### Harnesses, execution, permissions — Required v1

- Product hierarchy is **Secretary → persistent Worker**. No Secretary-managed Child Workers or new child lifecycle; harness-native subagents remain an internal capability of the Worker.
- Public v1 **supports and tests Codex and Claude Code**; a user needs to configure only one. Secretary itself uses any supported harness and can differ from Workers. Preserve historical/experimental adapters where needed for compatibility.
- Secretary selects an appropriate harness and Node from availability/task/preferences; user may override. Binding to a native session and Node is durable. Never silently migrate an existing Worker to another Node.
- **Full-access is the only promised default execution mode in v1**. No Secretary-built smart approval engine or read-only product mode. The native harness owns its native approvals and tools; full-access is explicitly not an OS sandbox.
- A direct message to a busy Worker attempts **Steering** immediately. Explicit **/q** stores a durable FIFO Queued message delivered as a later Follow-up. Unsupported steering/offline errors are visible and never silently converted into another action.
- The owner can view Worker activity, message, cancel, archive/close, and later resume Workers. Worker Topic communication and direct Worker observer are not extra Secretary conversations.
- Execution Node enrollment/pairing is authorized by the owner and Nodes connect outbound. Native session state remains on its host unless explicitly exported as an Artifact.
- Supported Server platforms: **macOS and Linux**. Supported Node platforms: **macOS, Linux, Windows** subject to live acceptance.
- Crashes, unknown execution state and lost Node connectivity are surfaced honestly. Never assert exactly-once external side effects or blindly repeat unknown actions.

### Channels and UI — Required v1

- Web interface and **installable PWA** share the existing Secretary API; offline-first mode, push and native apps are not release requirements.
- Telegram bot works in DM **without a group**, or with an optional admin-enabled forum supergroup. General is the Personal Conversation projection; every Worker has a Topic for direct activity/instructions.
- DM and General are synchronized bidirectionally with canonical input/reply identities, with no duplicate execution from mirrored messages. Topic messages go to Workers, not to another Secretary conversation.
- Only the configured owner's Telegram identity may command Secretary; other group members never obtain control merely by seeing the group.
- Web/PWA has a common chat and Worker observer; no mandatory native harness TUI takeover.

### Artifact Pipeline — Required v1

- Maintain **one central Artifact Store on Secretary Server** for uploaded originals, derivatives/transcripts and published output, indexed by stable artifact identifiers, metadata and ownership.
- Support images, video, audio/voice and ordinary documents/files as inbound input to Secretary **and** direct Workers. Secretary and Workers must both be able to send resulting files back through Web/PWA and Telegram.
- Video: preserve original, extract configurable sampled timestamped WebP frames (**1 FPS is only an editable default**), optionally extract audio/transcribe, and track sampling limitations. Users can adjust rate/limits in config.
- Audio: preserve original and Markdown transcript as related Artifacts; reuse existing Telegram voice ASR where applicable, do not reimplement it needlessly.
- All Node-to-Server transfers and downstream fetches use stable Artifact references, not local paths. A Worker explicitly publishes an output Artifact rather than having the server scan arbitrary directories.
- Media preprocessing is asynchronous; dependent prompt proceeds only after processing or explicit failure. Retain original and report errors accurately. Reject oversized uploads with a warning; never silently truncate.
- Archiving/closing Workers does **not** delete Artifacts. Explicit deletion removes unreferenced derivatives. Backups cover store and metadata.

### Memory, installation and service — Required v1

- SQLite remains the durable canonical event/lifecycle store, independent of native model compaction.
- Long-term memory uses editable **memory.md** and **user_preferences.md** for explicit user-requested memories/preferences. No inferred automatic fact writes. Migrate/retain existing user.md content compatibly rather than destroying it.
- Use harness-native Skills. Central registry, autonomous learning, embeddings and hierarchical summaries are later.
- A single install command leads to an interactive **terminal** setup detecting supported harness/auth and configuring local Node/optional Telegram. Browser, Secretary cloud account, and manual config editing are not mandatory.
- Support **secretary config** TUI and optional direct config editing; CLI service operations start/stop/restart/status/logs/update, ideally backed by native OS service management.
- Reuse **existing bootstrap token/code → Web session/cookie** model; do not add passwords, email identities or a second account system for Developer Edition.
- Remote Web is owner-managed (localhost/LAN/private HTTPS); Secretary does not need to host a public URL in Developer Edition.
- Backup/restore includes SQLite, long-term memory, config and central Artifacts, but cannot promise reconstruction of native sessions on remote Nodes from server-only backup.

### Release Gate — Required v1

An unfamiliar developer must be able to independently install Secretary, use either Codex or Claude Code, create/continue a Worker with question/answer turns, switch topics, steer or queue input, connect Web/PWA and Telegram DM/optional group with Worker Topics, upload and receive media/files, connect a local and remote Node, survive tested failure/restart, and restore central backup.

Both Codex and Claude Code must pass real native runtime tests. macOS and Linux Server and macOS/Linux/Windows Node support must be demonstrated, not inferred from build success. No false "implemented" claims based only on stubs, mocks or documentation.

## Testing Decisions

**Primary test seam:** highest useful end-to-end boundary — Secretary Server public API + Channel adapters + Execution Node with deterministic fake harness/file fixtures. Test observable conversation order, Worker identity, Result identity, transferred bytes, session/state and user-visible errors, rather than private functions.

Scenarios:
1. Dispatch → Worker asks question as Result → user answers through a different channel → same Worker receives Follow-up; switching topics remains possible.
2. Web and Telegram DM/General identical canonical records without echo loops or duplicated model invocations; Topic input only steers the right Worker; reconnect/replay safe.
3. Active steer vs explicit /q FIFO; unsupported steering visibly fails; cancel/close preserve correct binding and Results.
4. Video frames and timestamps respect config; audio original and transcript linked; published output bytes survive Node→Server→DM/Web; file over limits rejected.
5. ASR/transcode failures retain originals and surface accurate errors; lengthy preprocessing does not block separate chat.
6. Restart/unknown execution and offline Node are visible without blind retry; archive retains files; deletion cleans only unreferenced content; backup restore is complete within advertised Server scope.
7. Web bootstrap-code owner sessions, Telegram owner binding, Node pairing, Artifact access authorization retain existing identity protections.
8. **Separate live acceptance** for real Codex/Claude Code, Mac/Linux Server and Mac/Linux/Windows Node. Mock tests alone do not certify them.

Reuse existing test seams in Core Secretary/Worker/queue/context, Telegram voice and adapter, Web API/session, Node runtime/recovery, and repository E2E. Do not introduce a new test framework solely for the spec.

## Out of Scope / Explicitly Rejected

**Rejected:** Secretary-managed Child Workers; new Secretary approvals engine; read-only mode in v1; native TUI Worker takeover or Managed/Native dual control; mandatory Projects.

**Later, not Public v1 release blockers:** advanced semantic memory/index/vector DB, auto-extracted facts, centralized Skills Registry or autonomous learning, cron/heartbeat proactivity, model gateway, realtime voice/live camera, custom computer use, full offline PWA/push, native mobile/desktop apps, team accounts, Consumer billing/inference/microVM implementation.

Pre-existing internals (Projects, approvals, child structs, historical harness adapters) may remain for compatibility: excluding them from product scope **does not authorize blind deletion**.

## Developer and Consumer Product Boundary

- **Developer Edition (v1):** self-hosted open-source, one owner, native authorized harness access paid/provided by the owner. Use only provider-supported integration flows; do not assume transferable or unlimited subscriptions.
- **Consumer Edition (later):** same functional assistant for nontechnical users who pay Secretary, with separately provisioned inference/compute, managed onboarding and data isolation. No design mandate for model sourcing, microVM provider, regional availability or unit economics in this spec.

## Current Code & Gap Ledger (checked 17 October 2026)

| Area | Existing evidence | Gap for required v1 |
| --- | --- | --- |
| Core | Go + SQLite + Worker/Attempt/Result, existing lifecycle contracts | Verify real end-to-end user scenarios |
| Web auth | Bootstrap token exchange and owner cookie session | Reuse; avoid new account system |
| Harnesses | Codex default and Claude runtime adapter | Real live acceptance for both |
| Projects | Existing Project Core + API | Optional only; no Project gate |
| Approvals/children | Historical server/UI structures exist | No new product framework; maintain compatibility |
| Memory | Existing user.md document and context reconstruction | Compatible transition to two specified Markdown files |
| Telegram | Topics, some Result mirroring and voice ASR | DM ↔ Group General full bidirectional sync and identity acceptance |
| PWA | Existing Svelte Web | Installability and client behavior |
| Media/files | Voice transcription, Result artifact reference fields | Canonical binary store, image/video/file ingest, Node transfer, explicit publish |
| Install | Existing secretary setup/doctor/service CLI | Improved TUI, cross-platform acceptance |
| Backup | Existing service/data runbook | Verified full central restore with Artifacts |

**Implementation order (not a claim of completion):** stabilize Codex/Claude lifecycle; finish channels/PWA + CLI setup; add Artifact Pipeline; implement simple memory migration and backup; verify platforms and release gates.

## Further Notes

- Historical references may contradict these owner-approved decisions. This file governs **desired product direction**; runtime-contracts.md continues to document actual behavior until implementation is changed and tested.
- Exact endpoint names, store schema or internal module paths are not prescribed here. Preserve domain vocabulary and adapt existing code/test seams first.
