# ADR-0001: One Personal Conversation, persistent Workers and optional Projects

- **Status:** Accepted (product direction; implementation may lag)
- **Date:** 2026-10-17
- **Context:** [Canonical spec](../../spec/spec.md), [grilling Q&A](../../spec/grilling-2026-10-17.md), [GLOSSARY](../../GLOSSARY.md), [runtime contracts](../architecture/runtime-contracts.md)

## Context

A user should not shuttle context between tools, threads, hosts and Worker sessions. They may start with "create a website", answer questions about the website, switch to another topic, and return hours later. The same Personal Conversation is presented in multiple channel adapters. A separate mandatory Project would burden everyday chat and simple file manipulation.

Historically, Secretary has Project mappings/policies and some legacy Child Worker code. Their existence does not establish their necessity as product workflows.

## Decision

1. One configured Person owns one durable **Personal Conversation** in Developer Edition. Secretary server is authoritative for its ordered entries and lifecycle, independent of Channel adapters and Execution nodes.
2. Secretary normally delegates nontrivial conversation/research/work to a **persistent Worker** rather than answering itself. A Worker has a durable binding to its Task and native harness session.
3. A Worker may **ask a question in its terminal Result**. Result completes one Attempt but **not** the Task. A user's contextual reply is a Follow-up to the same Worker, starting another Turn/Attempt. Secretary asks for clarification if recipient is ambiguous.
4. Another message may start unrelated work without shutting down existing Workers. Results go directly into the common conversation **without mandatory automatic Secretary re-response**.
5. User can also open the Worker observer/Topic and steer, queue or cancel work directly; this does not create another Secretary conversation.
6. Workers remain addressable until explicit Task closure/archive. Closing is not history or Artifact deletion.
7. **Projects are not mandatory** in the user product. Existing project mapping/policy components remain optional compatibility for developer use; no mandatory Project setup or Product Project agent.
8. **No Secretary-managed Child Workers.** Native harness subagents are implementation internals of a Worker, not a new server-level product role.

## Consequences

- Positive: intuitive continuity for nontechnical users; reuse of the existing Worker/Attempt/Result state machine and channel-independent identity.
- Tradeoff: Secretary must route short contextual answers correctly and ask on ambiguity. Terminal Result cannot be equated with Task closure.
- Implementation must preserve canonical identity/order and stable Worker binding through restart.
- Existing Project/child code is not automatically removed. Changes require explicit migration/compatibility review.

## Alternatives rejected

- One separate Secretary conversation per channel.
- Every user answer generating a new Worker.
- Mandatory Projects for conversations.
- A new question-specific WaitForInput agent lifecycle when an ordinary Result/Follow-up suffices.
- Secretary-managed Child Worker trees.

## Verification

From public channel/API inputs: Dispatch → Worker asks in Result → user replies in another channel → same Worker resumes; independent topic starts another Worker; replay/duplicate delivery does not cause a duplicate Attempt; closing/archive retains history. Use existing Core, Web API, Telegram and E2E seams.
