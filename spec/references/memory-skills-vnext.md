# Secretary vNext — Memory ("what") and Skills ("how")

> **Status:** Proposed next-update design and delivery plan; **not implemented**, not an amendment to Public v1 acceptance.
> **Date:** 2026-10-21
> **Intent:** Secretary accumulates useful knowledge and reusable experience across its own restarts, native harnesses, devices, and Execution Nodes.
> **Source proposals:** [Persistent Memory Architecture](./memory.md) · [Skills Learning & Evolution](./skills.md)
> **Current behavior:** [Explicit Markdown memory](../../docs/memory.md) · [Runtime contracts](../../docs/architecture/runtime-contracts.md)

## Product promise

**Secretary remembers WHAT it learned and HOW it learned to work.**

- **WHAT — Memory:** who the owner is, their preferences, past discussions, decisions, project facts, corrections, and what previous Workers actually found.
- **HOW — Skills:** reusable, evidenced procedures for doing work, handling known failure modes, and verifying outcomes.
- Both are **Secretary-owned and harness-independent**. Codex, Claude Code, and future native harnesses consume this knowledge; none of them is its sole owner.
- The owner should *not* have to type `/learn` after every useful Worker outcome. Ordinary successful work should be eligible for automatic background procedural learning.
- The architecture must not replace native harnesses with a custom model-calling executor.

**Release boundary:** finish current installer/native-harness E2E stabilization first. Do not expand the existing Public v1 gate or imply any proposal here has shipped.

## Current baseline and deliberate extension

Secretary already has one server-owned Personal Conversation, persistent Worker/Turn/Attempt/Result state, and `memory.md` / `user_preferences.md` files. Those Markdown documents are edited on an **explicit owner request**, with revision and origin controls; they are not a semantic search or automatic memory learning system.

This proposal adds capabilities without discarding those files, changing the meaning of old stored content, or duplicating the Worker state machine.

The older [memory proposal](./memory.md) describes typed records, provenance, FTS, optional embeddings, and history zoom; the older [skills proposal](./skills.md) describes registry, validation, and Hermes-inspired learning. **This document records updated priorities:** local semantic embeddings are a first-class supported option, and **automatic** evidence-based Skill creation/evolution is a primary product requirement (not a stretch goal dependent on explicit `/learn`).

## 1. Two knowledge systems with distinct authority

| Layer | Question it answers | Authoritative source |
| --- | --- | --- |
| Episodic history | "What was said or done?" | Canonical conversation entries, Worker Results and source artifacts |
| Operational state | "What is running now?" | Existing Worker/Turn/Attempt/Result and Node state; never a stale summary |
| Semantic memory | "What do we know about the owner, project, or decision?" | Typed records with scope, time, origin, source links and corrections |
| Search and summaries | "Where can I find the evidence?" | Rebuildable FTS/vector indexes and optional hierarchy; never the sole truth |
| Procedural Skills | "How do we perform and verify this task?" | Portable versioned `SKILL.md` packages with applicability and outcome evidence |

No Worker may unilaterally write global owner memory or publish a cross-project Skill merely by emitting text. The server owns publication and authorization. Worker observations are **evidence**, not automatically true instructions. Projectless Workers must remain valid; project scope is optional.

## 2. Memory: durable facts + searchable experience

### 2.1 Store and reconcile

- Reuse existing server-owned event, Worker Result, and Artifact IDs; build idempotent ingestion keyed to canonical origin IDs.
- Introduce structured facts/decisions/preferences with stable IDs, scope (`owner`, `project`, `node/environment`, `worker` when appropriate), timestamps, source IDs, and status (`candidate`, `active`, `superseded`, `forgotten`).
- An explicit owner correction wins over a conflicting derived memory **within the same scope**, without erasing past history. Keep timeline questions answerable.
- Preserve existing `memory.md`, `user_preferences.md`, and `user.md`; define whether a typed record is a materialized view of a file entry or a separately maintained record, and make conflict behavior deterministic.
- Automatic extraction may **propose** facts from conversations or completed work. Promotion to authoritative personal preferences must follow an explicit user-controlled policy. Do not treat untrusted Worker output, quoted text, or third-party webpages as owner intent.
- A forget/delete operation must invalidate regular retrieval results **and derived indexes/summaries**, with honest behavior for immutable backups/retention.

### 2.2 Local hybrid retrieval

Use **SQLite FTS5** as the lexical baseline and a **pluggable local embedding model** for semantic recall; combine results, deduplicate, rank by relevance/scope/freshness, and open primary sources before asserting important facts.

**EmbeddingGemma 2** is a leading candidate for the local embedder:

- Google's open model card describes a **740M total parameter** multimodal model: **270M text**, plus selectively loadable vision and audio encoders, supporting a unified text/code/image/audio/video vector space.
- It supports **8K input context**, a native **768-dimensional** embedding with shorter 128/256/512-dimensional options, and consumer-device inference.
- First evaluate a text-only configuration on actual supported Secretary host machines. Multimodal artifact indexing is a possible later extension, not a prerequisite.

**Implementation requirements:**

1. An `Embedder` interface independent of Claude/Codex native sessions and vendor APIs; model/version, modality, dimension, normalization, and task-prefix configuration are recorded with each indexed vector.
2. Local inference by default. Never require a remote embedding API to make memory work.
3. Explicit model installation/download and storage size; do not silently pull model weights during setup. Support CPU-only and no-embedder mode.
4. Indexing asynchronous and replayable. A query should still work through FTS if embedding inference/indexing is unavailable or slow.
5. Chunk source records sensibly (not every entire conversation); preserve source ID/range, timestamps, scope, and current revision in vector/FTS metadata.
6. Reindex deterministically when the embedding model or dimensions change; do not silently compare incompatible vectors.
7. Benchmark retrieval accuracy, indexing duration, query latency, memory consumption, and disk cost on Linux/macOS and realistic hardware before selecting a runtime/vector index implementation.
8. Keep vector storage compatible with the existing SQLite-first packaging when practical; a new daemon/database is not assumed.

**Candidate source:** [EmbeddingGemma 2 model card](https://ai.google.dev/gemma/docs/embeddinggemma/model_card_2) and [Google launch notes](https://blog.google/innovation-and-ai/technology/developers-tools/embeddinggemma-2/). Model choice is subject to measurements; it must remain swappable.

### 2.3 Harness-facing retrieval and context assembly

At a new Secretary turn or native-session rebuild:

```text
canonical state + recent relevant dialogue + active scoped facts
                           |
                      context builder
                           |
                Codex / Claude native harness
                           |
          MCP: memory_search, memory_get, history_get
                           |
       FTS5 + optional local vectors -> original evidence
```

- Inject a bounded, relevant context packet rather than full history or every Worker transcript.
- Give the harness tools to request precise original events, prior Worker reports, and attributable facts.
- Keep current operational state out of vector search; retrieve it from the existing server.
- A historical summary tree with `zoom` (UniiChat / OptMem inspiration) is **optional** after evaluating hybrid retrieval. A summary must never replace its originals.
- Preserve source references in answers where users ask why a decision was made; report uncertainty instead of inventing a memory.

## 3. Skills: automatic procedural learning, inspired by Hermes

### 3.1 Default experience

The desired behavior is **automatic, background, evidence-based learning**. A user should not have to request skill creation after every task; `/learn` can remain an optional override for a specific experience.

```text
completed Worker Turn + verified outcome + correction/event evidence
                             |
                     durable review job
                             |
             no useful lesson? -> store nothing
                             |
        extract candidate procedure + applicability + tests
                             |
       match existing Skill -> patch or create version
                             |
       evaluate/verify -> auto-publish eligible agent-owned skill
                             |
          future compatible Worker loads it natively
                             |
       usage/outcome feedback -> refine, revert, or archive
```

### 3.2 Automatic review and publishing policy

- Trigger after suitable **terminal or validated checkpointed** Worker outcomes; run outside the user-facing critical path with configurable frequency, concurrency, and usage budget.
- The learning service reads canonical Result, available report/tool evidence, user corrections, and prior Skill versions. It does not require private chain-of-thought or unrestricted conversation scraping.
- Output categories: no reusable learning, candidate project/environment fact, new procedure, patch existing procedure, or unverified hypothesis.
- **Automatic publication is allowed for eligible agent-created/agent-managed Skills** after schema checks, source attribution, scope/compatibility checks, and appropriate lightweight validation. Routine learning does **not** require approval every time.
- Never promote unsupported "this fixed it" claims merely because a Worker said so. If validation is insufficient, keep a candidate and explain why it is not active.
- User-written, pinned, external/bundled, or otherwise protected Skills must not be silently modified/archived by automated review.
- Version every published change, keep prior content, track source links, and provide easy inspect, disable, rollback, and "why was this loaded?" controls.
- Background reviewer has limited memory/skill-management capabilities; **learning does not grant shell/network/file privileges** or bypass Secretary/native harness policy.
- Schedule curation to detect conflicting, redundant, unused, or stale *agent-managed* Skills. Archive reversibly; do not silently erase useful rare procedures.
- Learning is a configurable feature; when disabled, normal Worker dispatch and native Skills continue to work unchanged.

### 3.3 Portable Skill Registry

Represent capabilities as portable **Agent Skills** packages, with standard `SKILL.md`, optional `references/` and `scripts/`, plus separately stored Secretary-owned metadata:

```text
skill ID + immutable version + content hash
scope (global/project/node capability, optional project)
compatibility (harness, OS, tools/permissions)
origin/result evidence + author/ownership policy
validation status + success/failure feedback
lifecycle (candidate/active/deprecated/archived)
```

Use **progressive disclosure**: select via compact manifest and load full instructions/references only when relevant. Node/native adapter materializes selected skills for the actual installed harness. Do not rewrite global user-installed skill directories without authorization. Pre-existing Worker native sessions and immutable bindings must not be silently transferred or mutated by registry updates; newly created Workers receive compatible versions.

Native Codex and Claude Code Skills are the execution surface. Secretary owns *learning and distribution*, not a new agent runtime.

**Related design issue:** [#55 — Skills registry: where to store and how to manage skills](https://github.com/GitHubFoxy/secretary/issues/55). Preserve and resolve that issue's ownership/storage questions during the design phase; do not build a second unrelated registry.

## 4. Compute and reliability constraints

- All local search/indexing and review scheduling must be bounded, retryable, and nonblocking for active conversation/Worker Results.
- Local embeddings avoid mandatory per-query paid embedding API calls, **but** generation/revision of summaries and Skills still consumes inference capacity through a configured native runtime or explicitly selected review backend.
- Review calls should be deduplicated by stable Worker turn/result revision; do not spend usage limits repeatedly on replay/reconnect.
- Make reviewer effort, cadence, disabled state, and cost/usage visible. Under limited subscription budgets, prioritization (novel failure recovery, repeated correction, high-value workflow) matters more than reviewing every trivial answer.
- Never automatically re-run uncertain external side effects for the sake of learning or tests. Read evidence and validate safely.
- All embedding and learning tasks must tolerate node outage, restart, and partial progress without duplicating writes.

## 5. Proposed incremental slices

**Slice A — Search foundation.** Index canonical conversation/Result evidence with SQLite FTS5, stable citations, scoped search tools, and bounded retrieval. Tests for restarts, repeated events, and prompt-injection text.

**Slice B — Local semantic memory.** Pluggable EmbeddingGemma 2 proof of concept; benchmark actual hardware; combine vector + lexical ranking. Introduce typed facts, corrections, conflict policies, and controlled extraction. Provide FTS-only mode when local embeddings cannot run.

**Slice C — Native portable Skills.** Resolve and materialize scoped versioned `SKILL.md` into new native Workers, track actual use, support manual Skills and rollback. Settle #55 storage/ownership decisions.

**Slice D — Autonomous Skills learning.** Durable post-result review, candidate classification/dedup, validation, **automatic publication for agent-owned eligible Skills**, safe version patching, and metrics. No required per-task `/learn` or approval dialogs.

**Slice E — Curation and optimization.** Background maintenance for agent-managed Skills, outcome-based refinement, optional hierarchical history zoom and multimodal retrieval only if evaluations show value.

These slices are independently testable deliverables under one umbrella issue. Their existence here does not commit to dates or broaden the current Public v1 release scope.

## 6. Acceptance / evaluation

### Product-facing scenarios

1. **WHAT persists across harnesses:** discuss a decision in Telegram, restart Secretary and replace its harness, then ask in Web why the decision was made; receive the correct answer with original-source references.
2. **Semantic recall:** recall an old conversation using different vocabulary; compare against FTS5-only, record the result, and work without embeddings if the local model is absent.
3. **Corrections and scopes:** an explicit changed preference wins in its scope; an old stale fact is not used as current; "what did I prefer before?" still works.
4. **HOW is learned without prompting:** a Worker solves and verifies a reusable problem; a background learning job automatically creates or patches an eligible Skill (or logs a justified no-op). No `/learn` and no mandatory manual confirmation.
5. **HOW transfers across harnesses:** a new compatible Worker with the other native harness discovers, loads, and applies the saved Skill; an incompatible node/project does not receive it.
6. **Experience improves:** evaluate matched tasks with and without the relevant Skill; report success and repeated-error rate, not the count of SKILL.md files.
7. **Protected content stays protected:** autonomous learning cannot alter pinned/user-written Skills or leak a project-specific procedure into another project.
8. **Resilience and deletion:** restart/replay does not duplicate memory/skills; forgetting invalidates indexes, rollback restores the previous Skill version, reviewer outages never block user turns.
9. **Evidence honesty:** unverified outcomes do not become active skills or asserted memories; current Worker status comes from canonical state rather than retrieval.
10. **Cost control:** disable/limit local embedding inference or background reviewer without losing basic Secretary functionality.

### Metrics

Memory: grounded answer accuracy, exact-source recall, stale-fact/conflict errors, scope leakage, FTS vs hybrid retrieval quality, query latency, CPU/RAM/disk footprint.

Skills: relevant selection precision/recall, actually loaded/used versions, later task-success delta, repeated error reduction, harmful regression/rollback frequency, inference effort spent per useful learned Skill.

Benchmark against the current explicit Markdown baseline and FTS-only search. A more complex subsystem must demonstrate measurable benefit.

## 7. Boundaries and non-goals

- No custom direct-API Secretary harness as a prerequisite.
- No mandatory hosted vector DB, cloud inference, graph database, or external service.
- No giant permanent prompt containing all past interactions or Skills.
- No automatic rewrite of externally owned Skills, no global memory writes directly by Workers.
- No new Secretary-managed child-Worker model or mandatory Project identity.
- No promises of actual human-like permanent recall, or unverified claims that a saved Skill improves future performance.

## Reference links

- [Secretary current explicit memory](../../docs/memory.md)
- [Secretary detailed memory design](./memory.md)
- [Secretary detailed skills design](./skills.md)
- [Hermes Skills](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills/)
- [Hermes Curator](https://hermes-agent.nousresearch.com/docs/user-guide/features/curator/)
- [Agent Skills specification](https://agentskills.io/specification)
- [Google EmbeddingGemma 2 model card](https://ai.google.dev/gemma/docs/embeddinggemma/model_card_2)
- [LongMemEval](https://arxiv.org/abs/2410.10813)
- [OptMem](https://github.com/VictorTaelin/OptMem)
