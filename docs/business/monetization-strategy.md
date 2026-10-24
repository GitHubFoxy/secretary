# Secretary — Product and Monetization Strategy

> **Status:** Working strategy / hypotheses, not a committed roadmap or a launch announcement.
> **Updated:** 2026-10-21
> **Principle:** **Your AI changes. Your Secretary stays.**

## 1. Vision

Secretary is one persistent personal AI assistant: one identity, one durable conversation and memory, and one place to delegate work. The user can change model providers, native agent harnesses, devices, and communication channels without rebuilding the relationship with their assistant.

Secretary itself owns the continuity layer: user preferences, knowledge, conversation, tasks, worker bindings, execution results, and routing. Native harnesses (initially Codex and Claude Code) provide reasoning and tool execution. Integrations and paid services extend what Secretary can accomplish but do not define its identity.

The ambition is not to build another model or a thin chat frontend. It is to make a useful, provider-independent personal delegation system that can follow a person from desktop to mobile, messaging apps, and eventually other surfaces.

**The near-term objective remains a reliable release that completes real tasks.** Monetization experiments must not displace fresh-install testing, real native-harness execution, follow-up/resume, or recovery work.

## 2. Product commitments

1. **Secretary Core stays free.** Source, self-hosting, personal conversation, memory, native harness orchestration, local/remote Execution Nodes, and core task lifecycle should not require paid Secretary services.
2. **Bring your own harness and provider.** Users may use their existing model/provider access and run locally; model switching is a product property, not a premium unlock.
3. **One Secretary, many interfaces.** Telegram, Web/PWA, future social integrations, and future devices should address the same canonical assistant and task system, not create separate memories or personalities.
4. **Paid services are optional.** Charge for convenience, managed operations, compute, prepared workflows, and organizational features—not for escaping artificial limitations in the free core.
5. **User-owned data remains portable.** Personal state and artifacts should be exportable, and paid services should have a self-hosted or user-controlled alternative where practical.
6. **Tools live outside any single harness.** MCP tools, skills, workflows, and durable learning should be reusable across compatible runtimes. A change in today's best model should not erase accumulated useful knowledge.

These are product intentions. They are not claims that every capability already exists.

## 3. Initial customers and expansion

| Sequence | Customer | Existing behavior / problem | Initial value proposition |
| --- | --- | --- | --- |
| First | AI power users and developers on X | Already pay for Codex/Claude and juggle terminals, sessions, machines, and context | One place to delegate to existing native harnesses, observe progress, and continue tasks |
| Next | Technical creators, indie developers, small agencies | Repeated content, research, and development workflows spread across providers | Reusable tools, skills, media pipelines, and optional hosted execution |
| Later | Small teams and businesses | Fragmented tooling, no shared execution policy, unclear access, costly manual coordination | Organizational workspaces, governed automation, shared worker capacity, predictable operations |
| Long term | Mainstream personal-assistant users | Want outcomes rather than prompts, APIs, and infrastructure | An always-available assistant with managed setup and useful integrations |

Start where Secretary's technical differentiation is easiest to demonstrate: people already using AI coding agents. Do not assume that mass-market users want to administer a server or understand ACP.

## 4. Revenue model: services around a free core

### 4.1 Secretary Tools — quality-of-life capabilities

Offer an optional catalog of MCP-compatible tools and managed workflows that can be connected to Secretary or, where supported, to other agent clients.

Potential offerings:

- Image-generation tool: one stable interface over interchangeable image-model providers.
- Video-generation workflow: concept and script → storyboard → assets → generation → assembly → revision → delivered artifact.
- Research, bookmarking, summarization, document publishing, and other repeatable workflow tools.
- Skills/templates that teach a harness how to use these tools consistently; separate reusable instructions from paid execution.

**Monetization hypothesis:** paid hosted tool access, workflow subscriptions, and/or metered credits for expensive media generation. A skill description alone is easy to copy; the defensible value is reliable orchestration, maintenance, provider switching, artifact handling, and outcomes. Avoid promising unlimited generation where each run has a variable cost.

Model-provider selection inside the tools should be replaceable without changing the Secretary-to-tool interface. Useful tools should work beyond Secretary wherever standard interfaces make that possible.

### 4.2 Secretary Cloud — managed assistant hosting

Run an always-available Secretary for people who do not want to maintain a machine or server. Provide setup, upgrades, backups, recovery, monitoring, and secure access from their devices. Users continue to choose their model/harness and may keep using self-hosted Execution Nodes.

**Monetization hypothesis:** monthly per-person hosting subscription, with clear resource and support limits. The paid value is not ownership of the assistant; it is removing operational work.

### 4.3 Worker Cloud — on-demand execution

Offer optional remote execution capacity for tasks when the user does not want a laptop running or needs an isolated workspace.

- Start with existing infrastructure or container-backed workers to validate demand.
- Explore Firecracker microVM or equivalent stronger isolation only when usage and workload requirements justify the operational complexity.
- Meter compute/storage/time transparently; allow budgets, quotas, stop controls, and per-task cost visibility.
- Distinguish ephemeral task execution from durable Secretary task/session state. Resumption must be honest about what execution state actually survives.

**Monetization hypothesis:** usage-based execution billing, optionally with included capacity in a higher-tier hosting plan.

### 4.4 AI services and provider onboarding

Help users choose and connect suitable providers and subscriptions, configure native harnesses, and build usable model-powered workflows. Where useful, provide separately priced model-backed services for generation or other capabilities.

**Monetization hypothesis:** onboarding/setup service, ongoing workflow access, and usage-based AI operations. This is distinct from Secretary Core; the user may choose to obtain their provider access independently.

### 4.5 Assisted setup and integration

Before operating a hosting fleet, offer paid hands-on installation and configuration:

- Install Secretary on the customer's machine/server.
- Connect their preferred native harness, Telegram/Web, and optional Nodes.
- Demonstrate a real task, follow-up, and restart/recovery behavior.
- Define a narrow support window and deliver a documented configuration.

This may be the shortest route to initial revenue and, more importantly, exposes real onboarding pain before investing in a cloud control plane.

## 5. Secretary Business

**Product concept:** the same Secretary architecture applied to organizations, with explicitly separated personal and organizational contexts.

Potential workflow: a development studio asks Secretary to analyze a sprint, identify blockers, delegate scoped implementation tasks to workers, and prepare PRs with provenance and required approvals. Other early verticals could be research, reporting, and content production.

A business product would need:

- **Organizations and workspaces:** organizational identity, data boundaries, shared projects, and deliberate links to personal accounts.
- **Permissions and approvals:** employees, roles, grants for tools/nodes/data, escalation for sensitive operations.
- **Shared worker fleet:** company-managed or hosted Execution Nodes, isolation policies, quota allocation, and budgets.
- **Audit and observability:** who requested an action, which runtime executed it, what changed, outcome, and cost.
- **Integrations:** GitHub, team communication, documents, issue trackers, internal knowledge sources, and domain-specific workflows.
- **Administration:** provisioning, upgrades, backup/restore, service health, and support.

**Initial delivery approach:** separate managed or customer-controlled Secretary deployment per organization rather than building shared multi-tenancy from day one. Add team-level collaboration only after confirming actual customer workflows and isolation needs.

**Revenue options:** organization subscription, managed operation/support, setup/integration fees, and metered cloud workers or media generation. Sell a measurable business outcome, not protocol names.

## 6. Pricing experiments (not decisions)

All prices below are *starting hypotheses*, not validated demand, published plans, or commitments:

| Offering | Illustrative test price | Charging unit |
| --- | --- | --- |
| Assisted personal setup | $150–500 | One-time, scoped engagement |
| Secretary Cloud | $15–25 | Per person/month |
| Secretary Tools | To validate per workflow | Subscription and/or metered usage |
| Worker Cloud | Cost-plus target to validate | Compute time/storage/other measurable resources |
| Secretary Business | $99–299+ | Organization/month for an initial small-team offering |
| Business deployment/integration | $1,000–5,000 | One-time, scoped project |
| Managed Business | From $299 | Organization/month, with explicit service scope |

A paid plan is viable only if revenue exceeds infrastructure, variable provider costs, payment fees, customer support, and acquisition costs. Expensive inference/media generation should not be hidden inside an unbounded flat subscription.

## 7. Go-to-market and validation gates

### Stage 0 — Ship a useful Core (now)

Complete live pre-release acceptance: fresh installs, native provider-backed tasks, same-worker follow-ups, restart behavior, and real channel delivery. Publish candid limitations. Record one end-to-end demonstration: message → delegated worker → progress/result → follow-up.

**Gate:** an unaffiliated person can install Secretary and complete a useful workflow with modest assistance. CI alone is not that proof.

### Stage 1 — Learn from early users

Publish short demos and technical write-ups on X and GitHub. Invite a small cohort (for example 10–20 people already paying for AI tools). Observe installation, first successful task, first follow-up, and repeat usage. Ask what they tried to do—not only which features they request.

**Gate:** repeated real use and clear evidence of a pain worth paying to remove.

### Stage 2 — First revenue, minimal platform work

Offer a few scoped, paid setups. In parallel, test exactly one optional workflow/tool with a narrow audience and measurable outcome (for example, reliable cross-provider media generation). Price based on value and real usage costs.

**Gate:** actual paid customers, support effort understood, and a repeatable offer.

### Stage 3 — Managed Secretary, then workers

Build hosting once enough users consistently ask to avoid running their own always-on server. Add hosted workers if customer demand and utilization justify the operational cost.

**Gate:** retention, contribution margin after support and hosting costs, and reliable operations.

### Stage 4 — Business pilots

Find one or two small companies with an existing repetitive workflow. Start with a separately deployed instance and agreed success criteria. Productize common permissions, audit, and management requirements only after pilots establish what is essential.

**Gate:** documented business value and at least one repeatable deployment pattern.

## 8. Measures that matter

Track a small set of practical measures, initially manually:

- **Activation:** installs that produce a real successful task and follow-up.
- **Reliability:** end-to-end acceptance pass rate, failures requiring human intervention, and time to recovery.
- **Retention:** users who delegate meaningful work again after the first week.
- **Willingness to pay:** paid setups, paid workflow trials, conversion to managed hosting when offered.
- **Economics:** infrastructure and model-service cost per paying customer, support hours, and contribution margin.
- **Business impact:** verified hours saved, turnaround time, or other customer-chosen workflow outcome.

GitHub stars, PR counts, and social impressions may help discovery; they are not substitutes for useful, repeated execution.

## 9. Idea parking lot — not release requirements

These concepts fit the architecture but must not block the first useful release.

- **Secretary Documents:** durable, editable artifacts separate from chat; revisions, citations, and later collaborative editing. Start with versioned Markdown rather than trying to build a complete editor immediately.
- **Secretary Everywhere:** address the same Secretary through social mentions, share sheets, and messaging platforms. Example: mention a bot under an X post to bookmark and summarize it, then receive the result in Telegram. Cross-channel identity and permission boundaries belong to the Secretary layer.
- **Portable learning:** harness-independent lessons/preferences and tool-assisted knowledge capture so users do not repeatedly teach every new runtime the same things.
- **Voice and new devices:** voice and in-car interfaces can become channels to the existing assistant, not separate AI products.

These are directions for user interviews and eventual prioritization, not a schedule.

## 10. Decisions to validate, not make prematurely

- Which first paid workflow produces a result valuable enough for someone to buy repeatedly?
- Will early customers pay first for assisted setup, managed hosting, or specialized tools?
- What specific operational burden justifies managed Secretary versus running the free Core?
- What worker workloads require microVM isolation and justify operating that infrastructure?
- Which business vertical has the shortest path from pilot to repeatable deployment?
- How do personal and organizational knowledge stay separate while preserving a coherent assistant experience?

**Operating rule:** ship reliable delegation first; earn the next product layer through observed use and payment. Preserve the open, provider-independent Core throughout.
