# ADR-0002: Harness-native execution, full access and no Secretary approval engine

- **Status:** Accepted (product direction; implementation may lag)
- **Date:** 2026-10-17
- **Context:** [Canonical spec](../../spec/spec.md), [grilling Q&A](../../spec/grilling-2026-10-17.md), [runtime contracts](../architecture/runtime-contracts.md)

## Context

Secretary would become unnecessarily complex if it implemented its own execution agent, nested delegation tree, smart approvals, model gateway and isolation system. Codex, Claude Code and other native harnesses already have tools, Skills, sessions, permissions, and optional internal subagents. The owner prioritizes minimal orchestration and the capability to work immediately.

Existing runtime/API code may have approval/child structures. This ADR establishes the **forward product scope**, not an instruction to tear out deployed compatibility mechanisms.

## Decision

1. Secretary owns **Task/Worker lifecycle, dispatch/routing, result persistence and Node selection**. The selected native harness owns task reasoning, tool use, its internal subagents/Skills and native permission behavior.
2. **Full-access execution is the v1 default and the only promised product mode.** Do not build a Secretary read-only toggle or new smart approval engine in v1. Native harness permission prompts may still exist and must be handled accurately according to that harness's real protocol; do not falsely claim a bypass.
3. Codex and Claude Code are the **required supported harnesses for Public v1**, with live native tests. An individual user only needs one installed/authorized. Secretary harness and Worker harness may differ; other adapters remain optional/experimental.
4. Secretary automatically chooses harness and **Execution node** from task, capabilities, availability and user preferences, with an explicit override. Bound existing native sessions are not silently migrated across hosts.
5. Direct Worker messages attempt **Steering** when active; **/q** means durable FIFO Queued message. If steering is unsupported, fail visibly; do not secretly queue or retry.
6. Server supports macOS/Linux; Execution nodes support macOS/Linux/Windows once verified. Node pairing is owner-authorized; Nodes connect outbound.
7. No claimed exactly-once external effects, no automatic replay of uncertain execution acceptance, and no silent "success" when a Worker/Node goes offline.

## Consequences

- Less custom code and more native harness capability for the user.
- **Full access carries real risk** on a trusted owner's host. It is not a sandbox, an automatic security guarantee or a promise of no native confirmations. Remote access and Node identity checks remain necessary.
- Feature matrices must be measured per harness; a native feature is not guaranteed merely because another harness supports it.
- Legacy Project/approval/child internal data must remain migratable; this decision does not authorize deleting history or breaking current clients.

## Alternatives rejected

- Mandatory Secretary-managed subagents and child agents.
- Custom approval scoring engine and mandatory workspace-read-only model in v1.
- Generic "Native takeover" / dual Managed-vs-Native Worker ownership mode.
- Automatic hidden migration/retry of live Worker sessions.

## Verification

Test via Secretary Server→Execution Node→fake/live harness boundary: supported Codex/Claude native start/resume/steer, unsupported steer error, /q FIFO, explicit cancel/recovery, user harness/Node override, unknown dispatch state and offline Node handling. Live test macOS/Linux Servers and macOS/Linux/Windows Nodes before advertising them as supported.
