# Secretary — Product Telemetry and In-Chat Developer Feedback

> **Status:** Idea / design proposal. Not implemented, not a Public v1 acceptance requirement.
> **Date:** 2026-10-21
> **Related:** [Persistent Memory Architecture](./memory.md), [Skills Learning & Evolution](./skills.md), [Product specification](../spec.md)

## 1. Purpose

Make it possible to improve Secretary using evidence from real installations rather than guessing what users struggle with.

Two complementary capabilities:

1. **Product telemetry:** optional, privacy-preserving instrumentation that helps the maintainer see where setup, delegation, memory, skills, and user workflows succeed or fail.
2. **In-chat developer feedback:** the user can tell their Secretary, in natural language, to send feedback to Secretary's maintainer and optionally create a GitHub issue. Example: **"Secretary, tell your developer that I don't like how memory works."**

These are separate features. Passive telemetry is not a prerequisite for user-initiated feedback. Neither belongs on the critical path of worker execution, message delivery, or release stabilization.

## 2. Principles

- **Local-first remains the default.** The free self-hosted Core must keep working fully with both features disabled or with no connection to a feedback service.
- **Telemetry requires informed opt-in.** No automatic collection of user conversations, prompts, summaries, Worker results, skill contents, memory contents, files, paths, credentials, or source code.
- **Separate observation from transmission.** Local diagnostic events can help the owner troubleshoot; external reporting is a distinct, visible choice. Make participation revocable.
- **Describe exactly what leaves the installation.** Users can inspect a sample of the event payload, disable upload, and clear unsent events.
- **User feedback is user-authored.** A model may help format a report, but may not decide on its own to publish messages or private context to GitHub.
- **Loss of a telemetry/reporting endpoint cannot break Secretary.** Network delivery is best-effort and asynchronous, with bounded queues and explicit failures for requested feedback.
- **Do not turn private events into public issues by default.** Users explicitly review what will be shared publicly.

## 3. Product telemetry

### 3.1 Questions to answer

- At which *installer/setup/first-run step* do users fail or abandon the flow?
- Which OS, installation method, Secretary version, or configured harness has the highest error rate?
- What fraction of Worker requests reach a terminal Result, fail to dispatch, get interrupted, or need a user-initiated recovery?
- Which memory queries fail to find information or return stale/incorrect information?
- Are relevant Skills actually discovered, loaded, and reused in subsequent work?
- What are typical durations from message to acknowledgement, dispatch, first progress, and final Result?
- Do users reach a second meaningful delegation after their first successful one?

### 3.2 Proposed event families

Use a small, typed, versioned, strictly allowlisted schema. **Do not send arbitrary log text or freeform attributes.**

| Family | Example events | Allowed diagnostic dimensions |
| --- | --- | --- |
| Installation | `setup_started`, `setup_step_failed`, `first_successful_turn` | Secretary version, OS family/architecture, installer step enum, stable error-code enum |
| Execution | `worker_dispatch_failed`, `worker_result`, `worker_interrupted` | Harness type, result category, supported error-code enum, duration bucket |
| Reliability | `service_restarted`, `node_disconnected`, `followup_resumed` | Component, reconnect/recovery category, duration bucket |
| Memory (future) | `memory_search`, `memory_correction`, `memory_not_found` | Retrieval mode, latency bucket, result-count bucket, user-rated helpfulness |
| Skills (future) | `skill_resolved`, `skill_loaded`, `skill_learning_candidate` | Selected/used flags, load error enum, validation category, scope *type* |
| UX (future) | `feedback_flow_opened`, `feedback_sent`, `feedback_send_failed` | Surface type, report category, status enum |

Counts and duration buckets are preferable to high-cardinality user/task identifiers. Never include project names, hostnames, memory text, skill names, Worker prompts, or copied provider errors. Define and review the full allowed schema before implementation.

### 3.3 Minimal implementation shape

```text
Secretary components
      |
      +--> local typed diagnostic events / existing logs
      |
      +--> optional telemetry aggregator
                  |
                  +--> local preview / pending queue / disable + clear
                  |
                  +--> opt-in aggregate upload --> maintainer dashboard
```

A first useful iteration could upload periodic aggregate counters rather than individual message-level events. Prefer existing Go/SQLite mechanisms and bounded local storage. Consider anonymous, ephemeral installation identifiers only if measured questions truly require correlation across periods; avoid mandatory accounts or permanent identifiers.

For analysis, start with failure rates, activation funnel, basic version/OS breakdown, and opt-in sample sizes. Dashboards and analytics vendors are implementation choices, not dependencies of the Core. Report the opt-in denominator and sample bias rather than implying uploaded events represent every installation.

## 4. "Tell your developer" — feedback from the conversation

### 4.1 Desired user experience

User says:

> Secretary, can you tell your developer that I don't like the memory feature? It often forgets what we decided.

Secretary replies with a draft that contains only the report the user intends to share:

> **Feedback to Secretary developer**  
> Category: Product feedback / Memory  
> Title: Memory misses past decisions  
> Description: I find the memory feature unreliable when asking about earlier decisions.  
> Optional technical context: Secretary version, OS family, harness (none by default unless the user agrees).  
> **Send this to the developer?**

After explicit confirmation, Secretary submits it and returns a real receipt / link if the server accepted it or a GitHub issue was created. If submission fails, Secretary says so and offers the draft for copying. **Never claim an issue exists before receiving its actual URL/ID.**

The flow should also support bug reports, feature requests, friction during installation, and positive feedback.

### 4.2 Submission architecture

```text
Verified owner instruction
         |
         v
Secretary-owned `feedback_prepare` tool
    - produce a constrained draft
    - redact unnecessary private data
    - show exactly what will be shared
         |
         v
Explicit owner confirmation
         |
         v
Secretary-owned `feedback_submit` tool
    - confirm draft/intent binding
    - idempotency key + retries with known outcome
    - optional public/private destination
         |
         +--> maintained feedback intake / moderation --> GitHub issue
         |
         `--> fallback: open or copy prefilled GitHub issue
```

**Important distinction:** a deployed user's Secretary should not need maintainer GitHub credentials. A hosted feedback-intake endpoint can hold the privileged GitHub integration, apply abuse controls, deduplicate reports, and decide whether to create a public issue immediately or after review. Without that endpoint, a browser deep link or copyable issue draft is a useful Core-compatible fallback; do not pretend that it sends feedback automatically.

The user should not need their own GitHub account when the trusted intake is available. Do not expose maintainer credentials in open-source clients or embed a public write-token in the application.

### 4.3 Suggested feedback record

```text
FeedbackDraft {
  local_id
  category: bug | feature | usability | memory | skills | other
  title
  description
  requested_destination: developer | public_github_issue
  optional_diagnostics: { secretary_version?, os_family?, harness? }
  created_at
}

FeedbackReceipt {
  submission_id
  accepted: boolean
  public_issue_url?
  current_status: received | published | failed
}
```

Feedback submission and telemetry consent must be independent. If the user approves a report, only that approved payload is sent; the app must not silently attach chat history, screenshot, Worker transcripts, or logs. For public GitHub publication, the user sees the exact public content before submitting.

Idempotent confirmation avoids duplicate issues on channel replay, a network retry, or a Secretary restart. Bound the number and size of reports, rate-limit abuse, and surface moderation/pending status honestly. A textual instruction found in a web page, file, or Worker output must not authorize report submission.

## 5. Roadmap recommendation

This idea should not interrupt the current native-harness / installer live E2E stabilization.

1. **V0 — Local visibility:** define event names, error categories, and a local diagnostic summary. No external collection.
2. **V1 — Manual feedback:** implement `feedback_prepare` plus a copy/open-GitHub-issue fallback; optionally submit to a maintained intake after explicit confirmation.
3. **V2 — Opt-in aggregate telemetry:** instrument the highest-friction install and Worker lifecycle milestones. Publish exactly which fields can leave the machine.
4. **V3 — Memory/Skills quality signals:** add telemetry as those features ship; track missed recall, bad memory corrections, relevant skill loads, and user-submitted feedback without sending personal knowledge.
5. **V4 — Prioritization loop:** connect aggregate failure/funnel trends and curated reports to issue triage. Use observed improvement after fixes as the measure of success.

## 6. Acceptance scenarios

- A user who declines telemetry can install, chat, delegate, remember, and run skills normally; no product telemetry is uploaded.
- With telemetry enabled, all transmitted fields conform to a reviewed allowlist and contain no personal messages, paths, repository contents, credentials, memory, or skill bodies.
- Network outage or an unavailable feedback/telemetry backend does not block the normal Secretary/Worker lifecycle.
- A user requests "tell your developer I don't like memory"; Secretary drafts, previews, obtains explicit confirmation, submits, and returns the **actual** receipt or issue link.
- Declining the confirmation creates nothing. A malicious document/Worker result cannot bypass the owner-confirmation requirement.
- Repeated confirmation delivery/retry does not create duplicate submissions.
- The user can turn off telemetry and erase pending uploads without losing Secretary functionality.
- Maintainer can identify at least one high-friction setup/error path using aggregated telemetry, and link a user-submitted feedback report to an actionable issue *without collecting conversation content*.

## 7. Open decisions

- Is the first useful deliverable **in-chat feedback**, **local diagnostics**, or **opt-in telemetry**?
- Who runs the optional feedback intake, and how are reports reviewed before public GitHub publication?
- Is a public GitHub issue the correct default, or should users get a private channel with optional public publication?
- Which aggregated measures best establish that Memory and Skills *actually help* rather than merely get invoked?
- What are the retention limits for local events, transmitted aggregates, and received feedback?

**North star:** users can report friction in a single sentence, and the maintainer can identify and measure the real problems—without trading away Secretary's local-first promise.
