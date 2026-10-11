# Secretary V2 — Skills Learning & Evolution

> **Status:** Design proposal / later-phase extension; not part of the approved Phase 4 MVP acceptance gate.
> **Related:** [memory.md](./memory.md) · [Secretary Phase 4 spec](https://github.com/GitHubFoxy/secretary/blob/main/.scratch/phase-4/spec.md)
> **Inspiration:** Nous Research **Hermes Agent**'s skill learning/review and curator, adapted for Secretary's multi-harness Workers.

## 1. Big idea

**Every completed Worker run can make future Workers better.**

Convert validated, reusable execution experience into portable, versioned **Agent Skills** rather than accumulating a giant `memory.md` of arbitrary notes.

```text
Worker Turn / Attempt outcomes + trace + user corrections
       |
       v
Experience extraction (asynchronous/background)
       |
       v
Candidate lesson or procedure
       |
       v
Find an existing matching Skill first
       |
       +---- update/patch existing Skill
       |
       `---- or create new Skill
       |
       v
Validate against evidence and tests
       |
       v
Versioned Skill Registry (scope + provenance + metrics)
       |
       v
Skill Resolver -> relevant native harness Worker
       |
       v
Measure usefulness, collect feedback, refine or roll back
```

This is **procedural learning**, not model fine-tuning. Skills describe repeatable task strategies, environment-specific workflows, verification steps, and known pitfalls. They are different from personal facts, ongoing Worker state, and raw conversation history (see [memory.md](./memory.md)).

The main objective is not **how many Skills we generate**, but **whether the right Skill is selected and demonstrably improves success on later tasks**.

## 2. Hermes concepts worth borrowing

### 2.1 Background review

Hermes includes an agent background-review mechanism that revisits recent experience and may create/update knowledge. Its review pass is intentionally restricted to memory and skill-management operations. It is separate from the main interactive response and can decide there is **nothing worth saving**.

Borrow these principles:

- Review a **completed or suitably checkpointed** Worker Turn, including tool outcomes, errors, recovery steps and final Result.
- Detect repeatable success, a hard-won recovery, explicit user correction, or useful environment quirk.
- Prefer updating an existing Skill over creating a near-duplicate.
- Extract a **general procedure**, not a verbatim narration of one task.
- Don't save unverified fixes as proven guidance.
- Don't convert temporary tool outages into permanent rules (e.g. "SSH is broken forever").
- Allow "no reusable knowledge found" as a normal outcome.

**Do not make periodic trigger numbers part of the domain model.** Review may run after a successful Turn, after explicit `/learn`, or from a scheduled curator job. Thresholds should be tunable.

### 2.2 `skill_manage`: create, patch, evolve

Model a skill-management operation set equivalent to Hermes's concept:

```text
skill_search
skill_get
skill_propose_create
skill_propose_patch
skill_validate
skill_publish
skill_archive
skill_rollback
```

A proposal is not a published version. All mutations should be versioned, attributable, and reversible. The system should prefer **small patches** to entire rewrites when a minor procedure is corrected.

### 2.3 `/learn`: explicitly learn a workflow

Example user requests:

- "Learn how we just deployed staging; reuse it next time."
- "Learn this service's official API workflow."
- "Запомни, **как** мы починили CI, чтобы следующие Workers делали так же."

The `learn` command requests a *procedural* extraction pipeline. `remember` requests a *semantic/personal fact* write. In ambiguous cases, Secretary can save a scoped fact and propose a procedural Skill separately.

### 2.4 Progressive disclosure

Only compact **name + description + applicability metadata** need to be available during selection. Load the complete `SKILL.md` only when selected, then its `references/`, `scripts/` or templates when necessary.

A large skill library must not become a giant permanent prompt. This is particularly important when the native Claude Code or Codex agent already has its own context and tool operations.

### 2.5 Curator / lifecycle

Treat the library as a maintained system: detect redundant, conflicting, unused, stale, and broken Skills; archive reversibly; protect pinned/user-authored Skills; support version rollback. Hermes has a curator concept with aging and consolidation. Don't blindly copy exact age thresholds; tune from actual usage and task-success data.

**Caution:** source repository/doc examples demonstrate mechanisms, not independent evidence that automatic skills always improve completion rate. Add a controlled evaluation before enabling autonomous publishing.

## 3. Scope and authority

Secretary Phase 4 defines **Worker** as the main execution entity and uses `Worker → Turn → Attempt → AttemptOutcome → Result`. Skill learning consumes those records without creating a competing `Task` model.

| Scope | Examples | Ownership and write policy |
|---|---|---|
| Global reusable | Git workflow, research/report review process | Secretary-owned registry; reviewed publication |
| Project | Project X deploy, build/test conventions | Scoped to project; Worker can propose a patch |
| Node / environment | macOS tool paths; Linux server deployment quirks | Scoped by machine/capability; validate before reuse elsewhere |
| Worker / Turn scratch | Current hypothesis, unsuccessful attempt, temporary command | Local working data, **not** an automatically published Skill |

**Workers consume Skills but do not unilaterally rewrite shared Skills** or global personal memory. Worker output may include a **candidate lesson** with provenance. A server-owned learning service performs deduplication, evaluation, and promotion.

A Worker permanently binds to its Node/HarnessInstance under Phase 4; the Skill Registry **does not** transfer the Worker session to another machine. It only makes a validated procedure available for *future* Workers that match capabilities and scope.

## 4. Portable skill packaging

Use the open **Agent Skills** convention (`SKILL.md` with frontmatter) as the portable authoring format. Store additional governance metadata in the server's registry, not in undocumented mandatory frontmatter that would break cross-harness loading.

```text
deploy-staging/
  SKILL.md
  references/
    runbook.md
  scripts/
    smoke-test.sh
```

Example:

```markdown
---
name: deploy-staging
description: Deploy Project X to staging and verify the service is healthy.
---

# Deploy Project X to staging

## Applicability
Use for Project X's staging environment only.

## Steps
1. Ensure the expected branch is checked out and the tree is clean.
2. Run the repository's required tests.
3. Invoke the documented staging deployment command.
4. Check the health endpoint and inspect startup logs.

## Known pitfalls
- Deploy scripts must run from `backend/`.
- Do not deploy if tests fail.

## Verification
- Health endpoint reports success.
- No new startup errors appear.

## Evidence
See referenced successful Worker runs in the registry metadata.
```

This is an **illustrative** skill, not a claim about how an actual Project X is deployed.

### 4.1 Registry data model (proposed)

```yaml
id: skill_123
name: deploy-staging
version: 3
lifecycle: active          # candidate | validated | active | deprecated | archived
owner: secretary
scope:
  project_id: project-x
  node_capabilities: [linux, ssh]
compatibility:
  harnesses: [claude_code, codex, fx]
source:
  worker_ref: wrk_012
  turn_ids: [turn_91, turn_108]
  event_ids: [evt_501, evt_733]
validation:
  status: passed
  test_suite: staging-smoke
  validated_version: 3
metrics:
  selection_count: 12
  use_count: 10
  success_count: 8
pinned: false
```

Registry needs stable IDs, immutable published versions, content hashes, audit trail, explicit scope and compatibility filters, source citations, tests, current status, last-used timestamp, and the ability to roll back.

## 5. Proposed learning pipeline

### Stage A — Capture

At terminal Worker Turn, capture the canonical `Result`, relevant tool outcome references, verified artifacts, user corrections and existing Skills used. Avoid treating hidden model reasoning as a source of proof. Ensure one learning job per `(worker_ref, turn_id, result_revision)` despite retries/reconnects.

### Stage B — Extract

A review model classifies observations:

```text
no_reusable_learning
new_project_fact          -> memory service, not Skills
new_procedure             -> Skill candidate
refine_existing_skill     -> patch candidate
unverified_hypothesis     -> keep as task/Worker evidence only
```

Ask the model for applicability, steps, expected outcome, environment requirements, failure modes, evidence links and how to test the procedure. **Reject** candidates without sufficient evidence or a falsifiable verification step.

### Stage C — Match/deduplicate

Search existing Skills by lexical keywords, descriptions, embeddings where useful, project scope, and procedure overlap. Prefer patching the closest valid Skill. Flag contradictory advice; do not merge blindly across project/node boundaries.

### Stage D — Validate

Compare candidate instructions to source traces; lint frontmatter and file layout; validate applicability/capabilities; run relevant tests or a sandbox replay where safe. For scripts, use an appropriate permission/approval boundary. A single successful real-world run is evidence but not proof that the procedure generalizes.

### Stage E — Publish

Create an immutable version with provenance and validation result. By default, fully automatic promotion should apply only to explicitly allowed, low-risk classes of skills. User-authored/pinned/external skills require approval for destructive changes. Keep a reversible path to prior versions.

### Stage F — Select and apply

Before a Worker Turn, Secretary/Node resolves available Skills based on project, node, harness capabilities, task intent, scope, and permission policy. Provide a compact catalog; let the native harness load `SKILL.md` through its supported mechanism. Record which Skill and version actually got used, not merely which were suggested.

### Stage G — Feedback and curator

On subsequent Turns, record selection correctness, outcome, user corrections and regressions. Curator periodically proposes archive, consolidation or patch; changes undergo the same validation and rollback rules. "Unused" by itself is not proof of a bad Skill, especially for rare but critical procedures.

## 6. Native harness integration

The design must preserve **real Claude Code, Codex and `fx`**, not replace them with a homegrown executor. Native capabilities and exact configuration mechanisms must be probed per version.

- **Claude Code:** expose relevant Skills using its Skills/project integration; Secretary memory and Skill tools can be MCP-based. Hooks (where supported) can capture lifecycle/checkpoint events or inject selected context, but should not be assumed portable to every harness.
- **Codex:** supply Agent Skills in its documented layout; use its native session and tools. Verify skill discovery behavior with the installed version.
- **`fx` / other harnesses:** write a thin adapter producing supported files, prompts or tool access; if skill support is limited, explicitly record the limitation instead of claiming parity.
- **Node:** owns local harness runtime/session implementation and skill installation/materialization; server owns shared registry and policy. Avoid global edits to user-installed skill folders unless explicitly enabled.

Proposed adapter seam:

```text
resolve_skills(worker_ref, turn_id, project_id, node_capabilities, harness)
    -> [{skill_id, version, relevance, manifest, content_locator}]

materialize_skill_bundle(skill_id, version, node_id, harness)
    -> {ready, installation_path?, native_reference?, warnings[]}

record_skill_use(skill_id, version, worker_ref, turn_id, outcome)
    -> idempotent usage event
```

Skills must not grant new tool permissions. `SKILL.md` is executable *guidance*, but shell access, file mutation, external network, approvals and sandbox decisions remain governed by the harness and Secretary's execution policy.

## 7. Evaluation: saved != learned

**Critical failure mode:** many autogenerated `SKILL.md` files may exist without ever being selected or improving execution. Therefore test both **capture** and **application**.

| Metric | Definition |
|---|---|
| Skill selection recall | Relevant task got the intended Skill |
| Skill selection precision | Irrelevant Skill was not loaded |
| Task success delta | Completion with validated Skill vs no Skill, same harness/model and matched tasks |
| Repeat-error rate | Previously corrected failure recurs on a later similar task |
| Regression rate | New Skill version worsens tasks passed by previous version |
| Provenance coverage | Every claimed pitfall/step has inspectable supporting evidence |
| Time to resolve | Time/tool iterations before successful result |
| Scope leakage | Skill applied outside allowed project/node/harness |
| Utilization | Selected/published Skills that are actually loaded and used |

Required tests:

1. A successful Worker run yields a candidate procedure, but **does not** automatically produce an active global Skill.
2. Background review with no reusable insight produces **no Skill**.
3. Another Worker on a compatible node receives and successfully uses the appropriate skill.
4. A Worker on an incompatible OS/project **does not** receive it.
5. A later user correction yields a versioned patch; prior version remains restorable.
6. A failing regression test blocks publication or triggers rollback.
7. Retry/reconnect does not create duplicated candidates, usage events, or active versions.
8. A malicious or mistaken Worker transcript cannot issue privileged changes through the learning reviewer.
9. A now-unused skill can be archived and restored without losing attribution or source links.
10. An upgraded native harness still loads the same portable skill (or records a clear compatibility failure).

## 8. Implementation phases (suggested)

**Do this after the Phase 4 event/Result/Worker lifecycle contracts are stable**, because learning depends on accurate outcomes and durable evidence.

1. **P0 — Registry and resolution:** Agent Skills packages, scope rules, native harness adapters, metadata, basic usage telemetry; manually authored Skills first.
2. **P1 — Candidate capture:** Worker-end learning job, extraction, provenance, deduplication; explicit `/learn` from successful workflows.
3. **P1 — Validation and evolution:** versioned patches, tests, approval boundary, rollback, outcome metrics.
4. **P2 — Curator:** lifecycle automation, stale/duplicate detection, careful consolidation and archive.
5. **P3 — Optimized retrieval:** learn better Skill selection from measured outcomes, only if baseline resolver underperforms.

Start with a small curated library and demonstrate success-rate improvement before expanding autonomy. The objective is a *working learning system*, not maximally sophisticated memory machinery.

## 9. Primary sources and implementation references

- [Hermes Agent source](https://github.com/NousResearch/hermes-agent)
- [Hermes Skills documentation — skill creation, learning, progressive disclosure](https://hermes-agent.nousresearch.com/docs/user-guide/features/skills/)
- [Hermes background review implementation](https://github.com/NousResearch/hermes-agent/blob/main/agent/background_review.py)
- [Hermes Curator documentation — stale/archive/consolidation](https://hermes-agent.nousresearch.com/docs/user-guide/features/curator/)
- [Agent Skills standard](https://agentskills.io/specification)
- [Claude Code Skills](https://code.claude.com/docs/en/skills)
- [Claude Code hooks](https://code.claude.com/docs/en/hooks)
- [Claude Code memory](https://code.claude.com/docs/en/memory)
- [Codex Skills documentation](https://developers.openai.com/codex/skills)
- [Voyager — reusable skill library in lifelong agent learning](https://arxiv.org/abs/2305.16291)
- [Reflexion — learning from feedback without updating model weights](https://arxiv.org/abs/2303.11366)
- [Secretary Phase 4 approved spec](https://github.com/GitHubFoxy/secretary/blob/main/.scratch/phase-4/spec.md)

**Verification caution:** Hermes and harness integrations evolve. Confirm actual file names, triggers, SDK events, curator defaults, and installed skill-discovery behavior against upstream sources and pinned versions when implementing; treat this document as an architectural proposal, not a statement that Secretary already has these features.
