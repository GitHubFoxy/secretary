# ADR-0004: One Secretary core, Developer Edition first and Consumer Edition later

- **Status:** Accepted (product boundary; Consumer infrastructure deferred)
- **Date:** 2026-10-17
- **Context:** [Canonical spec](../../spec/spec.md), [grilling decisions](../../spec/grilling-2026-10-17.md)

## Context

The initial users are developers already using native AI coding harnesses and paying for their own accounts. They value durable delegation across machines and channels. The longer-term goal is a simple paid personal assistant for nontechnical customers, including users who cannot or do not wish to configure direct subscriptions and infrastructure.

These audiences have different **provisioning needs**, but should not require unrelated implementations of conversation, Workers, Artifacts and channel routing.

## Decision

1. **One logical Secretary product/core.** Conversation/Worker/Artifact experience should be consistent between offerings.
2. **Developer Edition / Public v1:** self-hosted, open-source, one owner, no Secretary cloud account required, Codex and Claude Code supported; each owner brings their own **authorized native harness credentials** and needs only one supported harness. No mandatory Project.
3. **Consumer Edition (Later):** a customer pays Secretary for a usable personal assistant; seller provides inference and may provide compute/execution environments. Setup must not expose model accounts, harness CLI or virtual machines to the customer.
4. Consumer multi-tenant identity, isolated storage, payments, provisioning, hosted infrastructure and applicable provider/region requirements are **future scope**, not developer v1 design/acceptance blockers.
5. **Do not define how commercial inference is sourced or paid for** in the developer-first product spec. Do not assume consumer subscriptions can be pooled or transferred to serve third parties; choose supported commercial supply paths later.
6. The Developer Edition's existing bootstrap-code/Web-session and Telegram owner identity are reused. Future Consumer identity is a distinct delivery concern, not a reason to install an enterprise IAM system now.

## Consequences

- Faster release to users who already understand coding harnesses, while maintaining a general personal assistant concept.
- Business-facing modules can evolve later without replacing Secretary's conversation/workflow domain.
- Current single-owner assumptions must be explicit; multi-tenant deployment requires separate threat model, billing and isolation work before launch.
- Do not treat legal/regional/provider speculation as an architectural requirement of Developer v1.

## Alternatives rejected

- Two independent products and duplicate orchestration systems.
- Requiring ordinary users to subscribe directly to Codex/Claude for Consumer Edition.
- Implementing multi-tenant billing or microVM orchestration as a Public v1 blocker.

## Verification

Developer v1: fresh install, owner bootstrap, each supported harness alone, no multiuser/account requirements. Later Consumer release will require separate isolation, provisioning and paid onboarding acceptance; it is not accepted by Developer v1 tests.
