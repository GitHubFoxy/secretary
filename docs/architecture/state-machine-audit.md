# State-machine suitability audit

Baseline: `b7bc8a8e5cba76996347ff84eec3fdf6b90798f7`.

## Classification

| Surface | Existing representation | Decision |
| --- | --- | --- |
| Web PWA bootstrap and session recovery | `loading` and `authenticated`, session recovery utility | Introduce an explicit loading lifecycle without changing authentication semantics. A follow-up can unify authentication and bootstrap states once retry behavior is covered end-to-end. |
| Web Control Room overview | `loading`, `error`, five-second refresh | Replace the initial loading flag with a constrained load lifecycle; keep last good overview visible on refresh errors. |
| Web Worker observer | `observer.loading`, nullable details, separate error | Candidate for `closed/loading/ready/error` with request identity; needs race regression tests for switching Workers and closing the observer during pending requests. |
| Web message submission | `messageStatus` already has idle, accepted, queued, working, succeeded, failed, error plus idempotency and stream updates | Strong candidate for a dedicated event reducer, but status values originate from different server contracts. Preserve the exact server protocol and resolve late-event races before migrating. |
| Web user document saving | `userStatus` already carries idle, saving, saved, error | Already an implicit finite-state workflow. A reducer is useful only if it prevents concurrent save/revision conflicts; replacing strings alone offers little benefit. |
| Web socket connectivity | offline, connected, reconnecting and timers | Candidate for disconnected/connecting/connected/retrying lifecycle; coordinate existing generation fencing and cleanup. |
| Control Room mutations | `actionInFlight` stores operation identity | Keep operation identity; a single global FSM would incorrectly couple independent operations. Consider per-operation transitions for cancellation, conflicts and retries. |
| Durable Worker, Turn, Attempt, Approval, command and schedule lifecycles | Typed server states in `internal/core/phase4_types.go` and durable stores/services | **Do not replace** with a client state machine. Server lifecycle is authoritative and already modeled by explicit states. Separately audit illegal transitions within store transactions. |
| Node/harness execution | Runtime-specific statuses, cancellation and recovery | Assess separately with integration tests; UI load-state helper must never govern runtime lifecycle. |

## Rules for further migration

- Introduce machines for competing boolean flags or real constrained, event-driven transitions, not for every boolean.
- Keep backend lifecycle states as the source of truth.
- Test stale responses, retries, cancellation, reconnection, idempotency and recovery before changing persistence or protocol handling.
- Do not add a general-purpose statechart dependency unless parallel/hierarchical lifecycle requirements justify it.
- This inventory is a targeted architectural classification, not proof that every conditional in the repository is a state-machine candidate.

## Scope of this PR

Use a tiny tested load transition function in both Svelte applications. No backend schema, persistence, runtime or HTTP contract changes.
