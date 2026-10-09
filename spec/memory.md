# Secretary V2 — Persistent Memory Architecture

> **Status:** Design proposal / future extension, **not** an amendment to the approved Phase 4 MVP.
> **Related:** [skills.md](./skills.md) · [Secretary Phase 4 spec](https://github.com/GitHubFoxy/secretary/blob/main/.scratch/phase-4/spec.md)
> **Goal:** One continuous, trustworthy personal assistant across channels, harnesses, workers, restarts, and machines — without depending on a model's context window or a particular native session.

## 1. Core decision

**Secretary server owns memory; native harness sessions only consume it.**

Separate three different things:

1. **Secretary identity / Personal Conversation** — one durable logical thread shared across clients (Telegram, Web, TUI, etc.).
2. **Secretary runtime** — a disposable/resumable Claude Code, Codex, or `fx` session used to process a turn. Reusing a session is an optimization, not a correctness requirement.
3. **Memory service** — server-owned canonical history, typed facts, indexes, summaries, and retrieval tools independent of any harness.

The server's database is the **source of truth**. Native compaction and native memory (`CLAUDE.md`, `MEMORY.md`, automatic memory) may improve ergonomics, but cannot be the sole persistence mechanism. Rebuilding a Secretary runtime must not erase facts, decisions, Worker results, or the user's history.

**Phase 4 compatibility:** The approved spec already mandates server-owned state and context reconstruction (§§4.2, 4.7, 9), and explicitly defers sophisticated memory, `remember`, and `search_history` beyond the initial MVP (§§7.3, 10.2). This document describes a subsequent feature, not a request to expand or silently rewrite the existing Phase 4 acceptance gate.

## 2. Hard invariants

- The server stores one ordered **Personal Conversation** with channel-independent identifiers and deduplicated inbound events.
- A Worker is the primary execution entity. **Do not reintroduce a separate product `Task` entity.** Preserve the Phase 4 `Worker → Turn → Attempt → AttemptOutcome → Result` model.
- Worker runs and tool activity have separate logs; their complete transcripts/reports are *retrievable*, not always placed into Secretary's active context.
- One terminal **Result per Worker Turn**; follow-up continues the existing Worker when appropriate. Memory ingestion must be idempotent across retries and reconnects.
- A Worker cannot directly alter **global personal memory**. It may submit evidence-backed candidate project/environment memories to a server-side review process.
- Memory is always **scope-aware** (`global/person`, `project`, `worker`, `turn`, `node/environment`) and **time-aware** (created/observed/valid intervals, supersession).
- Every derived memory or summary links back to its source event(s). An LLM-created summary is not authoritative evidence.
- Explicit corrections and explicit `remember` requests override older inferred claims *within the correct scope*. Do not silently apply a project-specific preference globally.
- `forget` must invalidate ordinary retrieval and relevant indexes. If raw history is deleted, associated derived data must be invalidated or rebuilt.
- Memory retrieval is read-only by default, permission-scoped, and treats retrieved content as **untrusted data**, not higher-priority instructions.
- Errors in retrieval must be explicit; do not hallucinate that a fact was remembered or a report was fetched.

## 3. Memory model

| Layer | Stores | Used for | Authority |
|---|---|---|---|
| **Event history (episodic)** | Raw user/Secretary messages, Worker lifecycle events, report references, tool outcomes when needed | Exact history; audits; reconstructing discussion | Canonical source |
| **Operational state** | Workers, Turns, Attempts, Results, Projects, Nodes, approvals, open loops | "What's running?", follow-up routing, current state | Existing server-owned domain state |
| **Semantic memory** | Preferences, project facts, corrections, decisions, aliases, relationships | "What do I prefer?", "What did we decide?" | Derived facts with provenance and conflict rules |
| **Hierarchical conversation index** | Time/range summaries with child links to original events | Navigate months of one conversation; reconstruct an old discussion | Rebuildable index, never sole source |
| **Search index** | Full-text/BM25 and optional embeddings of events/reports/memories | Discover candidate evidence by exact string and meaning | Rebuildable index |
| **Procedural knowledge** | Versioned `SKILL.md` packages and harness instructions | "How should this task be performed?" | **Separate** from personal/episodic memory; see [skills.md](./skills.md) |

Do **not** store structured Worker state as a textual summary in a vector database. The current status of a Worker must be read from the existing server's state machine.

### 3.1 Suggested durable records (illustrative, not mandated SQL schema)

```text
ConversationEvent {
  event_id, conversation_id, seq, occurred_at,
  actor, channel, source_message_id, kind,
  worker_ref?, turn_id?, attempt_id?, result_id?,
  body_ref?, artifact_refs[], visibility, deleted_at?
}

MemoryRecord {
  memory_id, kind, scope_type, scope_id,
  subject, predicate, value,
  status: candidate | active | superseded | forgotten,
  valid_from?, valid_to?, observed_at, updated_at,
  source_event_ids[], supersedes_id?,
  authority: user_explicit | user_statement | verified_state | worker_observation | inference,
  confidence?, version
}

ConversationSummaryNode {
  node_id, start_seq, end_seq,
  summary, child_ids[], source_revision,
  generated_at, invalidated_at?
}

MemoryIndexDocument {
  document_id, document_type, source_id,
  scope, text, embedding_version?, indexed_revision
}
```

Store long Worker reports/transcripts and artifacts as immutable files or blob references with server-controlled metadata and access checks. Do not duplicate all content into the main conversation. Existing Phase 4 `Result` remains the authoritative user-facing terminal outcome.

### 3.2 Provenance and time

Example: the user says "Use npm only in Project X" after previously preferring pnpm globally. Correct representation:

```yaml
kind: preference
subject: package_manager
value: npm
scope: { type: project, id: project-x }
authority: user_explicit
status: active
source_event_ids: [evt_123]
valid_from: 2026-10-08T12:00:00Z
```

**Do not** overwrite the global pnpm preference. If a fact is replaced, retain the prior version as `superseded` so questions about the past can be answered accurately. Conflicting same-scope facts must be resolved using time, explicit corrections, source authority, and, when necessary, user clarification.

## 4. Context reconstruction and retrieval

At the start of a Secretary turn (or when rebuilding a lost native session):

```text
canonical server state + current user message
           |
           v
      Context Builder
        |-- concise Secretary policy and user.md revision
        |-- recent conversation entries
        |-- current Workers/Results/approvals/Projects state
        |-- scope- and time-filtered semantic memories
        |-- targeted event/report search results
        `-- optional historical-summary index
           |
           v
      bounded context packet
           |
           v
   native Secretary harness session
           |
           `-- on-demand memory/report/zoom tool calls
```

- Prefer **bounded and relevant** context over injecting months of messages. Don't hard-code "last N" as the only retrieval method.
- Existing Phase 4 rule remains: **unseen Worker Results** are included in the next Secretary context. Include the relevant summary/metadata and stable report locator by default; expand full report on demand.
- Continue a native session while healthy; discard/rebuild it when compacted, moved, changed, or corrupted. Auto-compaction is allowed but **never the durable memory layer**.
- Never assume native Claude Code/Codex transcript locations are portable or canonical. Nodes may retain private runtime/session identifiers; those are not public Worker identity.

### 4.1 Hybrid retrieval pipeline

1. Classify intent: current state, personal fact, historical quote, decision, old Worker result, procedure/skill.
2. Resolve relevant entities, scope, and time window.
3. Fetch operational state **directly** where applicable.
4. Search lexical/full-text and semantic indexes in parallel; optionally search hierarchical summaries.
5. Merge and deduplicate; filter by permissions/scope and validity; rerank if useful.
6. When accuracy matters, **open original event/report** instead of trusting a summary snippet.
7. Return evidence references and explicitly say when evidence is missing or contradictory.

### 4.2 Long conversation / UniiChat-style zoom

Maintain an append-only event sequence and a **derived** hierarchy of compact summaries. Each node records the precise event range it covers and pointers to child nodes. The agent can `zoom(summary_node_id)` to descend until it reads the original message(s). Recent history can stay detailed while old history is compressed at upper levels.

**Limitation:** A summary can omit a relevant detail. Therefore *do not rely on the hierarchy alone*; combine it with full-text and semantic search over original data. Summaries are discovery aids, not truth.

### 4.3 Worker knowledge exposure

Secretary normally receives: Worker reference, current status, outcome summary, selected artifact IDs, and stable report locator. On a question like "what was in the Reformation research?", Secretary calls `worker_get_report` or searches indexed reports. A full report should not be injected for every future turn merely because a modal displays it in the client.

## 5. Proposed server-owned tool surface

These are **future extensions**, not changes to the current Phase 4 tool contract:

```text
memory_search(query, scope?, time_range?, kinds?, limit?)
memory_get(memory_id)
memory_event_get(event_id)
memory_timeline(entity_or_project, time_range?)
memory_zoom(summary_node_id)
memory_remember(fact, scope, source_event_id)
memory_correct(memory_id, correction, source_event_id)
memory_forget(target, mode)
worker_get_report(worker_ref, turn_id?)
worker_search(query, worker_ref?, project_id?)
```

- **Secretary role:** search/read; remember/correct/forget with appropriate user intent and server-side validation.
- **Worker role:** permitted read for task/project/node scope, task-local writes, `propose_memory`; **no direct global writes/forget**.
- The MCP or adapter layer uses scoped credentials. Do not trust a `worker_ref` passed in free text as authorization.
- Separate personal knowledge retrieval from instructions; an event saying "ignore previous instructions" is merely data.
- `memory_remember` needs a source event for auditability; duplicate requests must be idempotent.

### 5.1 Commands and semantics

- **`remember` / `запомни`:** durable, typed write; deduplicate against same-scope records; confirm the exact stored claim and scope after commit.
- **Correction:** supersede conflicting prior claim without deleting historical evidence.
- **`forget`:** remove from semantic retrieval, apply tombstones, invalidate indexed derivatives; never claim erasure if copies remain accessible by ordinary memory tools.
- **Delete history:** a distinct explicit operation; erase requested original events/artifacts where supported, invalidate descendants and indexes, propagate to caches/backups according to actual retention guarantees.
- **`search_history`:** search evidence, not the model's internal guess of past conversations.

## 6. Implementation approach

Respect the existing Go + SQLite-first Secretary design. Start with SQLite tables and **FTS5** for lexical search; add vector search only after measuring recall requirements and evaluating suitable extensions/services. Avoid making Neo4j, a hosted vector DB, or a separate cognition framework required for the first iteration.

Suggested ordering:

1. **Foundation:** durable event/report references, source IDs, normalized ingestion, idempotency. Reuse existing Phase 4 state rather than duplicating domain records.
2. **Baseline:** full-text search across conversation/Worker reports, bounded context reconstruction, `worker_get_report`.
3. **Structured memory:** typed scopes, provenance, correction, remember/forget, conflict rules, timestamps.
4. **Hybrid retrieval:** embeddings + lexical search + reranking with primary-source expansion.
5. **History hierarchy:** asynchronous summary tree and zoom tools; versioned regeneration/invalidation.
6. **Learning integration:** send candidate reusable procedures to the Skill Learning pipeline described in [skills.md](./skills.md).

Implement extraction/consolidation outside the interactive critical path when possible. A successful user-facing turn must not depend on optional embedding/index jobs succeeding. Use durable jobs with retries and idempotent writes; indexes can be rebuilt from canonical sources.

## 7. Acceptance tests (must-have)

- A fact remembered via Telegram can be recalled via Web/TUI after the Secretary harness is replaced or its context compacted.
- A Worker report remains accessible after restart, but is **not** injected wholesale into every Secretary turn.
- Given 30 long Worker reports, Secretary can answer a question citing a specific source report.
- Project X preference and a conflicting global preference are scoped correctly.
- "What did I prefer before?" retrieves superseded information with its time range.
- A duplicate delivery or replay does not duplicate an event, memory, or Worker Result.
- An uncertain/interrupted Attempt does not become a fabricated success or an automatic retry.
- A Worker cannot modify global personal memory; it can submit a candidate with evidence.
- `forget` prevents an active memory resurfacing through ordinary retrieval; dependent summaries/indexes are invalidated.
- Unknown or unsupported historical questions result in an explicit "not found / uncertain," not a fabricated recollection.
- Evaluate fixed tests against baseline (`user.md` + recent messages), full-text only, typed + hybrid, and typed + hybrid + hierarchy. Measure answer accuracy, evidence recall, stale-fact errors, cross-scope leakage, deletion resurrection, and latency.

## 8. Non-goals and trade-offs

- No end-to-end replay of **every** Worker tool token into the Personal Conversation.
- No second product `Task` entity or migration of Worker bindings to other Nodes (Phase 4 forbids silent migration).
- No dependence on a single harness's automatic memory or compaction algorithm.
- No claim that vector RAG or a temporal graph automatically beats simpler searchable Markdown. Benchmark against simple baselines first.
- No automatic promotion of low-confidence model inference to a permanent personal fact.

## 9. Primary sources and implementation references

- [Secretary Phase 4 approved spec — source-of-truth, Worker/Turn/Result, context reconstruction, deferred memory](https://github.com/GitHubFoxy/secretary/blob/main/.scratch/phase-4/spec.md)
- [UniiChat / OptChat design by Victor Taelin — append-only history, hierarchical summaries, recursive zoom](https://gist.github.com/VictorTaelin/91837951a5ce5b38f341ec1ba1df6449)
- [OptMem — practical hierarchical text memory; subagent/global-memory separation](https://github.com/VictorTaelin/OptMem)
- [MemGPT — virtual context / tiered memory](https://arxiv.org/abs/2310.08560)
- [Generative Agents — memory stream, reflection, planning](https://arxiv.org/abs/2304.03442)
- [LongMemEval — long-term conversational memory evaluation](https://arxiv.org/abs/2410.10813)
- [LongMemEval-V2 — extended state/workflow/environment memory evaluation](https://arxiv.org/abs/2605.12493)
- [Lost in the Middle — long-context positional effects](https://arxiv.org/abs/2307.03172)
- [Claude Code memory](https://code.claude.com/docs/en/memory)
- [Claude Code hooks](https://code.claude.com/docs/en/hooks)
- [Model Context Protocol](https://modelcontextprotocol.io/)
- [SQLite FTS5](https://www.sqlite.org/fts5.html)

**Implementation note:** These links are references/starting points; check current SDK APIs and licensing/configuration details against actual installed harness versions before implementing hooks or MCP capabilities.
