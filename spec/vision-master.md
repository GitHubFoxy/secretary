# Secretary — Master Vision & Product Design Tree

> **Draft/for review.** Long-term product vision plus complete decision checklist. Not production behavior, not a release claim, and not a final owner-approved spec. This belongs to draft PR #212 and will only supersede `spec/spec.md` after the owner explicitly signs off.
>
> **Provenance:** CONFIRMED = current Oct-25 interview; BASELINE = Oct-17 accepted canonical goals, currently under re-review; PROPOSAL = recommended but not accepted; RESEARCH = externally documented technical possibility, not yet validated in Secretary.
>
> **Status rule:** Every question in the forthcoming catalogue begins as **PROPOSAL / OPEN** unless its answer is explicitly recorded in the settled decisions below. A recommendation is not consent. Unknown technical details must not be presented as implemented.

## North star

Secretary is an open-source, user-controlled, always-by-your-side personal AI assistant. It uses the owner's supported AI harnesses, subscriptions, API keys and local models; runs with user-selected compute in local, VPS, serverless and cloud-node topologies; carries personal context across devices; and gradually extends beyond developer delegation into home, vehicle, voice, services and multi-person life. Coding is the first specialization, not the limit.

## Settled current interview outcomes

- **CONFIRMED Q1:** Universal personal assistant, later home and possibly car integrations; coding assistance is only the first focus.
- **CONFIRMED Q2:** Extensible BYO-AI compatibility (harnesses, providers, subscriptions, keys, local models) validated adapter by adapter. No assertion of universal support or of bypassing provider restrictions.
- **CONFIRMED Q3:** Server may run on personal hardware, VPS, supported Cloudflare/serverless/cloud; Nodes may run on local hardware, VPS or sandboxed cloud compute. Identity/state portability and hybrid use are goals, not proofs of live support.
- **CONFIRMED Q4a:** Nick and Ora have independent personal Secretaries and private chats; personal Telegram shows only its owner's queries; Circle is separate.
- **CONFIRMED Q4b:** Circle is a scoped collaborative session, temporary by default and resumable/reusable.
- **CONFIRMED Q4c:** Personal Secretaries may share task-relevant conclusions to Circle; sensitive/raw records need additional authorization.
- **CONFIRMED Q4d:** Circle/agents may negotiate and propose, including loans; a proposal neither proves agreement nor authorizes real payments/booking.
- **CONFIRMED Q4e:** Each participant's Secretary may execute an individually approved payment through a supported connector. Circle and other Secretaries do not obtain the person's payment credentials.
- **CONFIRMED Q4f:** Circle has shared conversation history and optional Telegram group projection; private DMs remain private.
- **CONFIRMED Q4g:** Invited people accept first; trusted groups may opt into auto-joining later.
- **UNREVIEWED:** Q4h–Q4t recommendations remain unapproved in `spec/review-2026-10.md` despite their presence in PR #212.

## Existing Oct-17 v1 baseline (to re-review, not silently redefine)

- **BASELINE:** one owner and one durable Personal Conversation in first open-source Developer Edition; separate later Consumer Edition with managed setup.
- **BASELINE:** Server owns Conversation, Task, Worker, Turn, Attempt, Result and Artifact metadata; channels are projections; Execution Nodes run external harnesses.
- **BASELINE:** Public v1 supports and live-tests both native Codex and Claude Code harnesses (user may have just one). Pi, OpenCode, FX are ideas/experimental integrations.
- **BASELINE:** Secretary handles trivial queries directly, delegates substantive research/work; persistent Workers resume, follow-ups, steer while busy or explicit FIFO `/q`; no Secretary-managed child Workers.
- **BASELINE:** Worker full-access mode on host; no custom smart-approval or read-only release gate; native OS/harness grants define real privileges.
- **BASELINE:** Telegram DM standalone, optional forum group General and Worker Topics; Web/PWA same canonical conversation, owner pairing, no duplicate mirrored Turns.
- **BASELINE:** Canonical Artifact Store for images, sampled video (configurable, 1 FPS only default), audio originals/transcripts, documents and published files; store references instead of machine paths.
- **BASELINE:** Editable explicit `memory.md` and `user_preferences.md`, with older `user.md` preserved; automatic fact learning, semantic index, realtime voice, computer use, native apps, proactivity later.
- **BASELINE:** CLI installer/doctor/service, SQLite, backups, platform matrix, real end-to-end and native-harness acceptance; never retry uncertain external effects blindly.
- **OLD VISION, NOT V1 GUARANTEE:** Pi/Fx/OpenCode, optional read-only modes, harness TUI takeover, desktop tray, phone-as-Node, federated accounts, Cloudflare control plane, cloud microVMs and broad service integrations appear in historical notes and require product decisions.
- **CONFLICT:** New per-Person Secretary and Circles conflict with old one-owner `spec/spec.md`, `GLOSSARY.md` and ADR-0005; old definitions remain binding for current release until explicit revision.
- **CONFLICT:** Serverless/cloud ambitions cannot silently overwrite accepted Developer Edition / Consumer Edition scope boundaries (ADR-0004).

## Research guardrails for physical integrations

- Home Assistant has documented REST/WebSocket APIs and integrations; use it as the device orchestration layer rather than implementing lamp drivers or radio stacks from scratch. Reference: https://contributors.home-assistant.io/architecture/big-picture/
- Tesla Fleet API exposes specific vehicle commands under access permissions and virtual-key requirements; supported commands can differ by vehicle, firmware, parked state and authorization. References: https://developer.tesla.com/docs/fleet-api/endpoints/vehicle-commands and https://developer.tesla.com/docs/fleet-api/virtual-keys/developer-guide
- Smartcar offers a cross-brand vehicle-command abstraction for participating makes, but functions differ and it is not universal coverage. Reference: https://smartcar.com/product/commands
- Voice/media/navigation integration for arbitrary vehicles may rely on different phone projection, OEM or accessory surfaces; never equate informational OBD access with authorized control over windows, locks, steering or acceleration.
- All safety/transactional real-world actions must distinguish suggested plan, authorized command, provider receipt and verified result.

## Design tree / coverage map

Each chapter below defines a separate decision domain. A chapter's candidate answer is a recommendation, not an implemented feature or acceptance. Dependencies often run: person identity → authorization → shared Circle; node topology → native harness → durable execution; smart-home hub → device capability → physical action; car provider auth → discovered functions → safe action; payment authority → booking workflow.

- **01 Vision and product identity** — goal: Universal open-source personal AI assistant across devices and physical services; boundary: coding-first does not mean coding-only; stage: Later; key risk: inflated promises
- **02 Person identity and personal accounts** — goal: Stable independent personal identities across channels; boundary: no channel address or voice sample alone grants power; stage: Later; key risk: shared-account leakage
- **03 Personal conversations and channel identity** — goal: One private canonical conversation per Person across their authorized channels; boundary: Circle shared chat is separate; stage: Baseline; key risk: message echo and privacy
- **04 Circle session and decisions** — goal: Durable scoped multi-person collaboration with proposals and agreement; boundary: no superior assistant or implicit consent; stage: Later; key risk: invented consensus
- **05 A2A and cross-server federation** — goal: Authenticated scoped messages among independently deployed Secretaries; boundary: no unbounded debate or implicit sharing; stage: Later; key risk: cycles and impersonation
- **06 Secretary orchestration** — goal: Persistent personal assistant routing direct replies and delegated work; boundary: no supervisor hierarchy; stage: Baseline; key risk: wrong task dispatch
- **07 Tasks Workers Attempts and Results** — goal: Durable task and persistent Worker lifecycle; boundary: never repeat unknown side effects blindly; stage: Baseline; key risk: duplicate execution
- **08 Codex native harness** — goal: Supported tested Codex runtime and user auth; boundary: unsupported explicit model must fail clearly; stage: Baseline; key risk: native/session mismatch
- **09 Claude Code native harness** — goal: Supported tested Claude Code ACP/native runtime; boundary: cancel receipt is not proven terminal; stage: Baseline; key risk: leaked native processes
- **10 Pi OpenCode FX and harness SDK** — goal: Pluggable experimental and future harness adapters; boundary: no untested v1 guarantees; stage: Future; key risk: capability mismatch
- **11 Subscriptions APIs keys and local models** — goal: BYO inference choice via verified adapter; boundary: no bypass of provider authorization; stage: Confirmed; key risk: secrets and incompatible terms
- **12 Model selection fallback and quotas** — goal: Explicit policy for choosing supported models/providers; boundary: never silently override explicit user choice; stage: Future; key risk: loss of task continuity
- **13 Execution Nodes and local hardware** — goal: Paired outbound Nodes on user's machines; boundary: full access is real OS authority; stage: Baseline; key risk: privileged code execution
- **14 Cloud microVM execution** — goal: Isolated hosted Workers with explicit lifetime and capabilities; boundary: not all workloads are safely sandboxed; stage: Future; key risk: escape and lost state
- **15 Cloudflare Workers and Durable Objects** — goal: Optional serverless control plane with persistent orchestration; boundary: native CLI execution stays on Nodes; stage: Future; key risk: platform lock-in
- **16 Self-host VPS hybrid topology** — goal: Equivalent personal experience across supported hosting; boundary: verified migration not assumed; stage: Confirmed; key risk: consistency and network downtime
- **17 Backups portability and state migrations** — goal: Portable durable personal state and artifacts; boundary: remote native sessions need separate export; stage: Baseline; key risk: unrecoverable data
- **18 Security authorization audit and secrets** — goal: Explicit identities approvals scopes and verifiable outcomes; boundary: no LLM text as source of authority; stage: Baseline; key risk: dangerous side effects
- **19 Personal memory preferences and retrieval** — goal: Editable durable memory with provenance; boundary: automatic inferred writes outside existing v1; stage: Baseline; key risk: hallucinated preference
- **20 MCP tools skills and extensibility** — goal: Common tool surface with native harness skills; boundary: reject unavailable capability explicitly; stage: Baseline; key risk: tool overreach
- **21 Artifacts files audio video and documents** — goal: Canonical binary inputs outputs and references; boundary: no silent truncation or fabricated processing; stage: Baseline; key risk: data corruption
- **22 Telegram direct messages groups and topics** — goal: Private DM with optional General and Worker Topics; boundary: mirrors cannot trigger duplicate turns; stage: Baseline; key risk: public chat leakage
- **23 Web PWA and UI** — goal: Common conversation and Worker observer across browser devices; boundary: no parallel backend required; stage: Baseline; key risk: divergent state
- **24 Native desktop mobile and tray** — goal: Presence and remote access in phone desktop applets; boundary: not mandatory for first v1; stage: Future; key risk: invisible background privileges
- **25 Voice live conversation and wake word** — goal: Natural hands-free assistant with voice continuity; boundary: avoid claiming instant latency everywhere; stage: Future; key risk: misrecognition
- **26 Proactive background assistant** — goal: Optional scheduled suggestions and follow-ups; boundary: require user-defined opt-in and budget; stage: Future; key risk: intrusive automation
- **27 Browser computer use and cross-app actions** — goal: Use user's tools and environment to operate software; boundary: constrain arbitrary action and clarify completion; stage: Future; key risk: web prompt injection
- **28 Home Assistant architecture and API** — goal: Native interoperable home control via Home Assistant; boundary: preserve local automation without cloud; stage: Future; key risk: cloud dependence
- **29 Zigbee Thread Matter MQTT and devices** — goal: Home device network via existing adapters and hubs; boundary: Secretary is not a new radio protocol; stage: Future; key risk: fragmented devices
- **30 Smart home scenes rooms and guests** — goal: Contextual room-aware actions and home routines; boundary: safety-class actions demand scoped consent; stage: Future; key risk: guest overreach
- **31 Automobile core concepts and safety** — goal: Capability-based unified interface to supported cars; boundary: cannot promise drive-any-car universal actuator; stage: Future; key risk: physical safety
- **32 Tesla Fleet API and commands** — goal: Owner-authorized Tesla connector via Fleet API; boundary: virtual key and command availability vary; stage: Future; key risk: sensitive unlock/drive operations
- **33 Non-Tesla OEM and Smartcar connectors** — goal: Vendor-dependent remote car capabilities across brands; boundary: no universal window/motion controls via standard API; stage: Future; key risk: false equivalence
- **34 In-car voice navigation and media** — goal: Hands-free car companion for navigation music cabin controls; boundary: driving-critical functions are out of scope without OEM support; stage: Future; key risk: distraction
- **35 Payments transfers purchases and booking** — goal: Authorized multi-person transactional workflows; boundary: proposals are not transfers or bookings; stage: Later; key risk: partial payment failure
- **36 Calendar email travel and shared planning** — goal: Personal schedule access and delegated trip coordination; boundary: private calendar raw events remain private; stage: Later; key risk: unwanted disclosure
- **37 Integration SDK adapters and ecosystem** — goal: Extensible connectors built on declared capabilities; boundary: no privileged arbitrary plugin by default; stage: Future; key risk: supply chain risk
- **38 Setup upgrades services and onboarding** — goal: One command plus terminal setup then simple controls; boundary: no mandatory cloud account in dev v1; stage: Baseline; key risk: fragile installation
- **39 Consumer Edition pricing and managed ops** — goal: Same product core with easy managed onboarding; boundary: billing provider supply and isolation later; stage: Future; key risk: unclear tenant isolation
- **40 Testing CI release acceptance and governance** — goal: Evidence-driven end-to-end product delivery; boundary: simulated tests do not prove real native readiness; stage: Baseline; key risk: misleading shipping claims

## Full decision question catalogue

<!-- AUTOMATED_CATALOGUE_PLACEHOLDER -->

## Review protocol

- Every question gets a numbered identifier, a proposed answer, a phase and a non-approval status. The owner can respond with changed IDs; only explicit approval changes statuses.
- Resolved terms update `GLOSSARY.md` at the appropriate point. ADRs are reserved for surprising, hard-to-reverse trade-offs with genuine alternatives.
- The owner can ask to expand any branch, and new prerequisites invalidate only dependent decisions.
- Final sign-off is required before treating the new vision as canonical requirements or merging PR #212. No code changes are implied.
