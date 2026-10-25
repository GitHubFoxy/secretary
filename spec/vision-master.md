# Secretary — Master Vision & Product Design Tree

> **Draft/for review.** Long-term product vision plus a focused decision shortlist. Not production behavior, not a release claim, and not a final owner-approved spec. This belongs to draft PR #212 and will only supersede `spec/spec.md` after the owner explicitly signs off.
>
> **Provenance:** CONFIRMED = current Oct-25 interview; BASELINE = Oct-17 accepted canonical goals, currently under re-review; PROPOSAL = recommended but not accepted; RESEARCH = externally documented technical possibility, not yet validated in Secretary.
>
> **Status rule:** Every question is **PROPOSAL / OPEN** unless directly grounded in a recorded current owner confirmation (**CONFIRMED**) or the older canonical specification (**BASELINE**). Baseline is being reconsidered; a recommendation is never consent. Technical implementation and live release acceptance are separate.

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

## Confirmed cross-cutting decisions from the interview (consolidated)

The rules below consolidate accepted generic Q0006–Q0100 answers removed from the interview catalogue. These are approved decisions, not new proposals:

- **One person, one Secretary:** No multiuser Secretary or multiple Secretaries for one Person. Different people's Secretaries cooperate through a separately scoped Circle; shared hosting never means shared identity or private history. Terry has Terry's own Secretary. Parental controls may be added; guest onboarding details remain open. (Former Q0026–Q0028, Q0050)
- **Link once:** Devices/channels associate to stable `person_id` through a one-time pairing code or supported OAuth; display names and channel handles may change. Knowing a username or a person's voice is not authentication. (Former Q0029–Q0031, Q0035)
- **High-risk action authentication:** Require a stronger factor such as PIN, SMS or OTP, not voice recognition alone. Specific risk thresholds/providers remain to be designed. (Former Q0030)
- **Trusted commands:** The Person and explicitly delegated identities may instruct Secretary. Remote pages, tools, integration outputs and agent messages cannot create new authorization merely by containing instructions. (Former Q0007, Q0032)
- **Opt-in proactivity:** Secretary may initiate recommendations; actual autonomous actions require prior user-configured authorization and limits. For ambiguous requests, choose trivial defaults but clarify consequential uncertainty. (Former Q0008–Q0009)
- **Verified capabilities:** Discover available tool/Node/provider functions; unsupported functionality is explicit rather than simulated. Show verified receipts for real-world results; a plan or accepted command is not execution proof. (Former Q0010–Q0011, Q0021)
- **Offline boundary:** Without connectivity, AI continues **only** with a locally available LLM, local runtime and local executable integrations. Cloud-only LLM conversations stop offline. Independent local automations can keep running under their own rules. (Former Q0017, Q0042, Q0067)
- **Clear authority and durable state:** Server-side canonical identity/conversation/task metadata is independent of LLM output and UI/channel caches; execution happens on available Nodes. Multiple devices/channels map to one Person's private canonical Conversation, with stable message identities and deduplication. (Former Q0014, Q0016, Q0039, Q0051–Q0054, Q0064, Q0066)
- **Privacy and revocation:** Separate histories, memories, credentials and session permissions. No automatic cross-person access or Circle export; revoke devices/grants independently. Personal data disclosed to Circle is purpose-scoped. (Former Q0013, Q0023, Q0038, Q0043, Q0048, Q0060, Q0063)
- **Lifecycle:** History persists across channel unlink/restart until owner deletion/retention policy. Export, backup, archive and deliberate deletion are supported goals; shared Circle records may have independent retention. (Former Q0015, Q0040, Q0055, Q0065)
- **Durable safety:** Cancellation is distinct from confirmed stop and from irreversible effects; concurrent changes require identity/order control. Unknown results are neither success nor failure; reconcile before retrying an external side effect. (Former Q0018–Q0020, Q0044–Q0045, Q0068–Q0070)
- **Ownership of costs:** Support limits and transparent usage per authorized owner/delegation. Local identity does not require a paid cloud account; provider/hosting usage is separately chargeable. (Former Q0022, Q0047, Q0072)
- **Configuration and release:** Human-readable config plus CLI/UI, scoped secret handling, testing of privacy/reconnect/idempotency and live integration seams. Track proposed, implemented, tested and released separately; protect Developer Edition coding baseline while broad capabilities mature. (Former Q0012, Q0024–Q0025, Q0037, Q0049, Q0074–Q0075)
- **Circle coordination:** Explicit invite/consent or configured trusted-group policy, decision-scoped participants, canonical shared history, independently controlled personal Workers and tools, attributable individual approvals, no invented consensus, no access to another Person's credentials. (Former Q0076–Q0100)

<!-- CATALOGUE_BEGIN -->
> **200 domain-specific candidate questions** across 40 domains (five per domain). Repeated generic lifecycle/security/operations templates were removed; accepted cross-cutting decisions are summarized above. Original Q IDs retain gaps for traceability, and are not a commitment to interview a fixed number of questions.

### 01 · Vision and product identity

**Контекст:** Universal open-source personal AI assistant across devices and physical services. **Граница:** coding-first does not mean coding-only. **Этап:** Later. **Основной риск:** inflated promises.

- **Q0001 · CONFIRMED** — Что включает определение «всегда рядом»? **Рекомендация:** Непрерывный персональный контекст и каналы, без фиктивных гарантий офлайн-действий
- **Q0002 · CONFIRMED** — Чем Secretary отличается от очередного чат-бота? **Рекомендация:** Выполняет действия через выбранные инструменты, сохраняет Task lifecycle и присутствует на устройствах
- **Q0003 · CONFIRMED** — Станет ли coding-only продукт тупиком? **Рекомендация:** Не встраивать coding-специфику в ядро identity/conversation
- **Q0004 · CONFIRMED** — Нужна ли единая личность при смене модели? **Рекомендация:** Да, user-owned state не привязан к модели
- **Q0005 · CONFIRMED** — Что первично в roadmap: идеальная Алиса или работающий продукт? **Рекомендация:** Подтверждённый coding v1, затем умный дом, голос и авто

### 02 · Person identity and personal accounts

**Контекст:** Stable independent personal identities across channels. **Граница:** no channel address or voice sample alone grants power. **Этап:** Later. **Основной риск:** shared-account leakage.

- **Q0026 · CONFIRMED** — Может ли один человек иметь несколько персональных Secretary? **Утверждено:** Нет. У каждого Person ровно один персональный Secretary; рабочие/личные контексты организуются внутри него, а не через дублирование личности.
- **Q0027 · CONFIRMED** — Nick и Ora могут видеть взаимные Telegram DM? **Утверждено:** Нет. Строго раздельные личные истории; Circle имеет отдельную общую историю.
- **Q0028 · CONFIRMED** — Как устроены Secretary ребёнка и гостя? **Утверждено:** У Терри есть собственный персональный Secretary, не «домашний Secretary». Для несовершеннолетних возможен настраиваемый parental control (пределы инструментов, расходов, интеграций); точный набор правил ещё предстоит обсудить. Поведение гостя без полноценного аккаунта — открытый зависимый вопрос.
- **Q0029 · CONFIRMED** — Как связать смену имени с памятью? **Утверждено:** Стабильный person_id отдельно от display name, Telegram username и других идентификаторов канала.
- **Q0030 · CONFIRMED** — Как подтверждать высокорисковые действия при голосовом управлении? **Утверждено:** Распознавание голоса само по себе недостаточно; нужна более сильная проверка — PIN, SMS или OTP/одноразовый код либо другой поддерживаемый дополнительный фактор в зависимости от риска. Конкретные способы и threat model определяются позднее.

### 03 · Personal conversations and channel identity

**Контекст:** One private canonical conversation per Person across their authorized channels. **Граница:** Circle shared chat is separate. **Этап:** Baseline. **Основной риск:** message echo and privacy.

- **Q0051 · CONFIRMED** — Что является канонической Conversation entry? **Рекомендация:** Server-issued identity с происхождением, автором, временем и связанным turn
- **Q0052 · CONFIRMED** — Должна ли Telegram General отражать DM владельца? **Рекомендация:** В однопользовательском v1 да, без повторного запуска модели; для разных людей нельзя
- **Q0053 · CONFIRMED** — Могут ли два канала отправить одинаковое сообщение одновременно? **Рекомендация:** Один canonical input при одинаковом ключе, не объединять разные сообщения по тексту
- **Q0054 · CONFIRMED** — Куда отправлять Worker Result? **Рекомендация:** В Origin Conversation конкретного Person
- **Q0055 · CONFIRMED** — Что происходит при удалении канала? **Рекомендация:** Данные Conversation сохраняются согласно политике, канал теряет доступ

### 04 · Circle session and decisions

**Контекст:** Durable scoped multi-person collaboration with proposals and agreement. **Граница:** no superior assistant or implicit consent. **Этап:** Later. **Основной риск:** invented consensus.

- **Q0076 · CONFIRMED** — Circle — навсегда общий разум семьи? **Рекомендация:** Нет, scoped collaboration session; можно возобновлять, а recurring membership вынести отдельно
- **Q0077 · CONFIRMED** — Кого считать участником Decision? **Рекомендация:** Явно выбранных людей, а не всех членов потенциальной группы
- **Q0078 · CONFIRMED** — Может ли Circle предложить заём? **Рекомендация:** Да, как гипотезу, а не согласие или выполненный перевод
- **Q0079 · CONFIRMED** — Где хранятся предложения и ответы? **Рекомендация:** В canonical shared history Circle, не как Telegram source of truth
- **Q0080 · CONFIRMED** — Что делать если стороны не согласны? **Рекомендация:** Статус разногласия и альтернативы; не выдумывать консенсус

### 05 · A2A and cross-server federation

**Контекст:** Authenticated scoped messages among independently deployed Secretaries. **Граница:** no unbounded debate or implicit sharing. **Этап:** Later. **Основной риск:** cycles and impersonation.

- **Q0101 · PROPOSAL / OPEN** — Должен ли A2A быть базой доменной модели? **Рекомендация:** Нет, протокол обмена между персональными Secretaries, а Circle владеет workflow
- **Q0102 · PROPOSAL / OPEN** — Как проверить, что remote Secretary действительно представляет Ora? **Рекомендация:** Credential-bound federation identity и подтверждённое приглашение
- **Q0103 · PROPOSAL / OPEN** — Может ли агент раскрыть необработанные медицинские записи? **Рекомендация:** Нет, только разрешённые минимальные выводы
- **Q0104 · PROPOSAL / OPEN** — Как обработать отказ другого сервера? **Рекомендация:** Durable pending/timeout, без фиктивного ответа человека
- **Q0105 · PROPOSAL / OPEN** — Может ли агент притвориться человеком при голосовании? **Рекомендация:** Нет, каждое решение требует подтверждённой identity/делегированного права

### 06 · Secretary orchestration

**Контекст:** Persistent personal assistant routing direct replies and delegated work. **Граница:** no supervisor hierarchy. **Этап:** Baseline. **Основной риск:** wrong task dispatch.

- **Q0126 · BASELINE** — Кто решает: прямой ответ или Worker? **Рекомендация:** Secretary с прозрачной политикой и ручным override
- **Q0127 · BASELINE** — Нужен ли отдельный manager поверх Secretary? **Рекомендация:** Нет, не вводить избыточную иерархию
- **Q0128 · PROPOSAL / OPEN** — Кто владеет правдой об active Attempt? **Рекомендация:** Secretary Server и durable receipts, не вывод LLM
- **Q0129 · BASELINE** — Как давать Secretary минимальные lifecycle tools? **Рекомендация:** Typed MCP/capability calls с origin и idempotency
- **Q0130 · BASELINE** — Когда Secretary должен задать уточнение? **Рекомендация:** Когда отсутствуют необходимые параметры или возможна неверная адресация

### 07 · Tasks Workers Attempts and Results

**Контекст:** Durable task and persistent Worker lifecycle. **Граница:** never repeat unknown side effects blindly. **Этап:** Baseline. **Основной риск:** duplicate execution.

- **Q0151 · BASELINE** — Что такое persistent Worker в отличие от subagent? **Рекомендация:** Привязанная к Task сущность с собственной историей и native session
- **Q0152 · BASELINE** — Может ли Worker задавать вопрос, завершив Attempt? **Рекомендация:** Да, Task остаётся открытым до явного закрытия
- **Q0153 · BASELINE** — Чем Steering отличается от /q? **Рекомендация:** Steering прямо сейчас, /q — FIFO на idle
- **Q0154 · BASELINE** — Что означает terminal canceled? **Рекомендация:** Доказано прекращение native execution и сохранён durable terminal outcome
- **Q0155 · BASELINE** — Можно ли запускать Follow-up на ином Node молча? **Рекомендация:** Нет, session binding должен быть доказан или пользователь решает о миграции

### 08 · Codex native harness

**Контекст:** Supported tested Codex runtime and user auth. **Граница:** unsupported explicit model must fail clearly. **Этап:** Baseline. **Основной риск:** native/session mismatch.

- **Q0176 · BASELINE** — Как выбирать Codex default model под ChatGPT auth? **Рекомендация:** По проверенной поддержке native аккаунта; explicit choice не переписывать
- **Q0177 · BASELINE** — Станет ли Codex runtime обязательным для всех установок? **Рекомендация:** Нет, допускается установка только Claude Code
- **Q0178 · BASELINE** — Что делать с ACP readiness до первого Prompt? **Рекомендация:** Реальная initialize/session readiness, а не только процесс/файл
- **Q0179 · BASELINE** — Как показывать native tool calls? **Рекомендация:** В Worker observer с trace/origin
- **Q0180 · BASELINE** — Как подтверждать release? **Рекомендация:** Live Start/Resume/Cancel/Result на опубликованном бинарнике

### 09 · Claude Code native harness

**Контекст:** Supported tested Claude Code ACP/native runtime. **Граница:** cancel receipt is not proven terminal. **Этап:** Baseline. **Основной риск:** leaked native processes.

- **Q0201 · BASELINE** — Можно ли выбрать Claude Code для Secretary и Codex для Worker? **Рекомендация:** Да, независимый конфиг
- **Q0202 · BASELINE** — Как разделить native subscription и API token? **Рекомендация:** Разные проверенные auth modes и ограничения
- **Q0203 · BASELINE** — Что делать при зависшем native Prompt и Cancel? **Рекомендация:** Отмена не должна ждать занятой записи без возможности остановить native CLI
- **Q0204 · BASELINE** — Может ли canceled ошибочно стать failed на Resume? **Рекомендация:** Не путать native canceled/interrupted с failure
- **Q0205 · BASELINE** — Как избежать дочерних процессов после отмены? **Рекомендация:** OS-aware process cleanup с честным отчётом о невозможности доказательства

### 10 · Pi OpenCode FX and harness SDK

**Контекст:** Pluggable experimental and future harness adapters. **Граница:** no untested v1 guarantees. **Этап:** Future. **Основной риск:** capability mismatch.

- **Q0226 · PROPOSAL / OPEN** — Должен ли Pi быть главным harness прямо сейчас? **Рекомендация:** Сначала capability/latency тесты, без обязательства v1
- **Q0227 · PROPOSAL / OPEN** — Что важно в OpenCode? **Рекомендация:** Поддерживаемые providers/models, session lifecycle и extensions
- **Q0228 · PROPOSAL / OPEN** — FX embedded — отдельная продуктовая линия? **Рекомендация:** Пока эксперимент, оценивать стоимость интеграции
- **Q0229 · PROPOSAL / OPEN** — Нужно ли общее SDK адаптеров? **Рекомендация:** Да, минимальный typed contract для prompt/result/tools/cancel
- **Q0230 · PROPOSAL / OPEN** — Подменять ли функции отсутствующего harness? **Рекомендация:** Нет, показывать поддерживаемые capabilities

### 11 · Subscriptions APIs keys and local models

**Контекст:** BYO inference choice via verified adapter. **Граница:** no bypass of provider authorization. **Этап:** Confirmed. **Основной риск:** secrets and incompatible terms.

- **Q0251 · PROPOSAL / OPEN** — Можно ли импортировать подписку как API key? **Рекомендация:** Нет, это разные типы авторизации
- **Q0252 · CONFIRMED** — Нужен ли выбор provider на уровне Task? **Рекомендация:** Да, с прозрачной конфигурацией и возможностью override
- **Q0253 · PROPOSAL / OPEN** — Где хранить provider secret? **Рекомендация:** Secret store/Node environment, не обычная история сообщений
- **Q0254 · CONFIRMED** — Можно ли утверждать, что любая подписка разрешает automation? **Рекомендация:** Нет, проверять актуальные условия поставщика
- **Q0255 · PROPOSAL / OPEN** — Как отменить доступ подключённой модели? **Рекомендация:** Revoke/rotate credential с видимым статусом зависимых Workers

### 12 · Model selection fallback and quotas

**Контекст:** Explicit policy for choosing supported models/providers. **Граница:** never silently override explicit user choice. **Этап:** Future. **Основной риск:** loss of task continuity.

- **Q0276 · PROPOSAL / OPEN** — Когда выбирать дешевую модель? **Рекомендация:** Для подходящей сложности и разрешённого provider route
- **Q0277 · PROPOSAL / OPEN** — Когда переключаться на fallback? **Рекомендация:** На явную ошибку/квоту по заданной политике, не вопреки explicit model выбору
- **Q0278 · PROPOSAL / OPEN** — Как сохранять context при смене provider? **Рекомендация:** Durable canonical Task/Conversation, не гарантия native session compatibility
- **Q0279 · PROPOSAL / OPEN** — Кто ограничивает деньги? **Рекомендация:** Budget per user/Worker/provider с предупреждениями
- **Q0280 · PROPOSAL / OPEN** — Должен ли автоматический fallback повторять уже выполненное действие? **Рекомендация:** Нет, сперва проверять durable outcome и idempotency

### 13 · Execution Nodes and local hardware

**Контекст:** Paired outbound Nodes on user's machines. **Граница:** full access is real OS authority. **Этап:** Baseline. **Основной риск:** privileged code execution.

- **Q0301 · BASELINE** — Что именно Node исполняет? **Рекомендация:** Worker harness processes и доверенные tools под своими OS capabilities
- **Q0302 · BASELINE** — Нужно ли owner-approved pairing? **Рекомендация:** Да, каждый Node получает собственную identity
- **Q0303 · BASELINE** — Работают ли Windows/macOS/Linux одинаково? **Рекомендация:** Семантика одинакова, acceptance платформ отдельно
- **Q0304 · BASELINE** — Можно ли открыть Node через internet port? **Рекомендация:** Предпочитать outbound secure tunnel/connection
- **Q0305 · PROPOSAL / OPEN** — Важна ли видимость доступа Node? **Рекомендация:** Да, indicator/tray/status и выключение доступа

### 14 · Cloud microVM execution

**Контекст:** Isolated hosted Workers with explicit lifetime and capabilities. **Граница:** not all workloads are safely sandboxed. **Этап:** Future. **Основной риск:** escape and lost state.

- **Q0326 · PROPOSAL / OPEN** — Нужен ли sandbox всем self-host Users? **Рекомендация:** Нет, но managed cloud workers должны иметь явную границу изоляции
- **Q0327 · PROPOSAL / OPEN** — Должна ли microVM переживать завершение Task? **Рекомендация:** По профилю persistent/ephemeral, без фиктивного Resume после уничтожения
- **Q0328 · PROPOSAL / OPEN** — Где хранить файлы между microVM сессиями? **Рекомендация:** Exported durable Artifact references и проверяемые volumes
- **Q0329 · PROPOSAL / OPEN** — Можно ли дать чужой VM домашний SSH ключ? **Рекомендация:** Только минимальная временная capability, не неограниченная копия ключа
- **Q0330 · PROPOSAL / OPEN** — Кто оплачивает CPU/GPU время? **Рекомендация:** Конкретный владелец compute с понятными лимитами

### 15 · Cloudflare Workers and Durable Objects

**Контекст:** Optional serverless control plane with persistent orchestration. **Граница:** native CLI execution stays on Nodes. **Этап:** Future. **Основной риск:** platform lock-in.

- **Q0351 · PROPOSAL / OPEN** — Может ли Secretary полностью жить в Cloudflare Workers? **Рекомендация:** Control plane да, native long-running Worker CLI выносится на Nodes
- **Q0352 · PROPOSAL / OPEN** — Для чего Durable Objects? **Рекомендация:** Owner-scoped orchestration, live connections, canonical coordination при переносимой схеме
- **Q0353 · PROPOSAL / OPEN** — Где хранить видео и документы? **Рекомендация:** Object store с серверными metadata/Artifact IDs
- **Q0354 · PROPOSAL / OPEN** — Что с WebSocket при hibernation? **Рекомендация:** Реконструкция live state из durable журналов и receipts
- **Q0355 · PROPOSAL / OPEN** — Что мешает переносу с Cloudflare? **Рекомендация:** Provider-specific primitives должны быть за adapters, export/import обязательны как цель

### 16 · Self-host VPS hybrid topology

**Контекст:** Equivalent personal experience across supported hosting. **Граница:** verified migration not assumed. **Этап:** Confirmed. **Основной риск:** consistency and network downtime.

- **Q0376 · BASELINE** — Можно ли использовать только Mac? **Рекомендация:** Да, Server и Node могут быть рядом, но как отдельные службы
- **Q0377 · BASELINE** — Можно ли разнести Server и Node по миру? **Рекомендация:** Да, аутентифицированное исходящее соединение
- **Q0378 · CONFIRMED** — Что делать при NAT/offline Node? **Рекомендация:** Сохранять очередь/ошибку, не повторять неизвестный native action
- **Q0379 · CONFIRMED** — Потребуется ли отдельный домен? **Рекомендация:** Для локального Developer Edition нет
- **Q0380 · CONFIRMED** — Как запускать HA bridge рядом с VPS Secretary? **Рекомендация:** Через outbound home companion, без раскрытия локальной сети

### 17 · Backups portability and state migrations

**Контекст:** Portable durable personal state and artifacts. **Граница:** remote native sessions need separate export. **Этап:** Baseline. **Основной риск:** unrecoverable data.

- **Q0401 · BASELINE** — Что считать portable backup? **Рекомендация:** Central DB/event state, config, memory и Artifact bytes с контрольными суммами
- **Q0402 · BASELINE** — Можно ли потерять native session при переезде? **Рекомендация:** Возможна потеря; сообщить заранее и экспортировать если поддержано
- **Q0403 · PROPOSAL / OPEN** — Как выполнять schema migration без потери истории? **Рекомендация:** Версионированный, тестируемый, обратимый когда возможно процесс
- **Q0404 · PROPOSAL / OPEN** — Как восстановить из частичного snapshot? **Рекомендация:** Не подтверждать success пока данные/bytes не сверены
- **Q0405 · PROPOSAL / OPEN** — Можно ли ручное редактирование Markdown считать основной БД Cloudflare? **Рекомендация:** Нет, Markdown удобный экспорт/редактирование, durability отдельно

### 18 · Security authorization audit and secrets

**Контекст:** Explicit identities approvals scopes and verifiable outcomes. **Граница:** no LLM text as source of authority. **Этап:** Baseline. **Основной риск:** dangerous side effects.

- **Q0426 · BASELINE** — Можно ли включить Worker без подтверждения доступов Node? **Рекомендация:** Только в рамках ранее осознанно выданных host permissions
- **Q0427 · PROPOSAL / OPEN** — Кто может разблокировать замок дома? **Рекомендация:** Person с соответствующей scoped capability и подтверждением
- **Q0428 · PROPOSAL / OPEN** — Как защищать Tool от prompt injection сайта? **Рекомендация:** Внешний текст не становится системной командой или правом
- **Q0429 · PROPOSAL / OPEN** — Что журналировать в денежных транзакциях? **Рекомендация:** Action ID, actor, consent, receipt, результат; секреты исключить
- **Q0430 · PROPOSAL / OPEN** — Как выглядит revoke при длительном Task? **Рекомендация:** Новые действия блокируются, active effects проверяются индивидуально

### 19 · Personal memory preferences and retrieval

**Контекст:** Editable durable memory with provenance. **Граница:** automatic inferred writes outside existing v1. **Этап:** Baseline. **Основной риск:** hallucinated preference.

- **Q0451 · CONFIRMED** — Станет ли shared Circle Memory копией всей личной? **Рекомендация:** Нет, scoped summaries и согласованные общие решения
- **Q0452 · BASELINE** — Как редактировать явную память? **Рекомендация:** Пользовательские Markdown files/управляемый UI и audit изменений
- **Q0453 · BASELINE** — Нужна ли automatic memory extraction уже в v1? **Рекомендация:** Нет, только явные записи; эксперимент после safeguards
- **Q0454 · PROPOSAL / OPEN** — Что делать если preference устарело? **Рекомендация:** Сохранять provenance/даты, спрашивать при важных решениях
- **Q0455 · PROPOSAL / OPEN** — Можно ли переносить память между cloud и local? **Рекомендация:** Да, экспорт в переносимом виде с явной областью доступа

### 20 · MCP tools skills and extensibility

**Контекст:** Common tool surface with native harness skills. **Граница:** reject unavailable capability explicitly. **Этап:** Baseline. **Основной риск:** tool overreach.

- **Q0476 · BASELINE** — Кто владеет tools? **Рекомендация:** Система с проверенными scopes, Secretary вызывает, Worker исполняет в native harness
- **Q0477 · BASELINE** — MCP — единственный поддерживаемый протокол? **Рекомендация:** Общий фасад MCP, внутренний adapter может использовать официальный SDK
- **Q0478 · BASELINE** — Где будут жить Skills? **Рекомендация:** Native harness Skills в v1; unified registry позже
- **Q0479 · PROPOSAL / OPEN** — Как выбрать tools для конкретной Node? **Рекомендация:** Capability discovery и списки разрешений per instance
- **Q0480 · PROPOSAL / OPEN** — Может ли плагин запускать сторонний бинарник? **Рекомендация:** Только в предоставленном execution environment, не в облачном control plane

### 21 · Artifacts files audio video and documents

**Контекст:** Canonical binary inputs outputs and references. **Граница:** no silent truncation or fabricated processing. **Этап:** Baseline. **Основной риск:** data corruption.

- **Q0501 · BASELINE** — Нужно ли сохранять оригинальный видеофайл? **Рекомендация:** Да, вместе с sampled frames и метаданными обработки
- **Q0502 · BASELINE** — 1 FPS жёстко обязателен? **Рекомендация:** Нет, конфигурируемый default
- **Q0503 · BASELINE** — Где связывать аудио и расшифровку? **Рекомендация:** Как разные связанные Artifacts со стабильными IDs
- **Q0504 · BASELINE** — Может ли Worker вернуть готовый PDF/архив? **Рекомендация:** Да, explicit publish Artifact, не scan общего диска
- **Q0505 · BASELINE** — Что делать с oversized upload? **Рекомендация:** Явный отказ без обрезания файла

### 22 · Telegram direct messages groups and topics

**Контекст:** Private DM with optional General and Worker Topics. **Граница:** mirrors cannot trigger duplicate turns. **Этап:** Baseline. **Основной риск:** public chat leakage.

- **Q0526 · BASELINE** — Можно ли работать только через DM bot? **Рекомендация:** Да, группа не обязательна
- **Q0527 · BASELINE** — Как показывать Worker's Topic? **Рекомендация:** Связать topic с одной canonical Worker identity
- **Q0528 · BASELINE** — Как избежать DM↔General echo? **Рекомендация:** Stable server-owned message IDs и cursor replay protection
- **Q0529 · BASELINE** — Участник чужой Telegram группы получает owner rights? **Рекомендация:** Нет, membership не равно pairing
- **Q0530 · CONFIRMED** — Будет ли Circle shared group видеть мои личные prompts? **Рекомендация:** Нет, только intentional shared Circle entries

### 23 · Web PWA and UI

**Контекст:** Common conversation and Worker observer across browser devices. **Граница:** no parallel backend required. **Этап:** Baseline. **Основной риск:** divergent state.

- **Q0551 · BASELINE** — Может ли PWA показывать один и тот же чат на Mac и телефоне? **Рекомендация:** Да, canonical projection и авторизованная session
- **Q0552 · BASELINE** — Нужно ли автономное offline execution из PWA? **Рекомендация:** Не в v1; честные offline/read-only состояния
- **Q0553 · BASELINE** — Как видеть занятых Workers? **Рекомендация:** Observer по worker identity с live events
- **Q0554 · BASELINE** — Как давать настройки пользователя? **Рекомендация:** Минимальные owner controls и явный config route
- **Q0555 · BASELINE** — Нужен ли второй frontend backend? **Рекомендация:** Нет, использовать тот же Server API

### 24 · Native desktop mobile and tray

**Контекст:** Presence and remote access in phone desktop applets. **Граница:** not mandatory for first v1. **Этап:** Future. **Основной риск:** invisible background privileges.

- **Q0576 · PROPOSAL / OPEN** — Зачем menu-bar/tray? **Рекомендация:** Видно, когда Node имеет доступ к машине, можно его отключить
- **Q0577 · PROPOSAL / OPEN** — Должно ли приложение запускаться при login? **Рекомендация:** Явный выбор пользователя при установке
- **Q0578 · PROPOSAL / OPEN** — Могут ли iOS/Android стать Execution Nodes? **Рекомендация:** Исследовать с ограничениями OS background execution и supported tools
- **Q0579 · PROPOSAL / OPEN** — Что делать без native приложения? **Рекомендация:** Web/PWA, Telegram и local CLI продолжают работать
- **Q0580 · PROPOSAL / OPEN** — Нужен ли Ghostty native takeover? **Рекомендация:** Future optional, не dual-control обязательство v1

### 25 · Voice live conversation and wake word

**Контекст:** Natural hands-free assistant with voice continuity. **Граница:** avoid claiming instant latency everywhere. **Этап:** Future. **Основной риск:** misrecognition.

- **Q0601 · PROPOSAL / OPEN** — Как инициировать голосовой turn? **Рекомендация:** Wake-word/PTT по настройке; распознавание отдельно от прав
- **Q0602 · PROPOSAL / OPEN** — Должен ли Assistant знать комнату микрофона? **Рекомендация:** Использовать paired satellite/room context, не угадывать
- **Q0603 · PROPOSAL / OPEN** — Можно ли получить низкую latency повсюду? **Рекомендация:** Цель streaming pipeline, реальные цифры через измерения
- **Q0604 · PROPOSAL / OPEN** — Как обрабатывать перебивание (barge-in)? **Рекомендация:** Cancel/suspend текущий TTS/turn с корректной state boundary
- **Q0605 · PROPOSAL / OPEN** — Что делать при распознавании нескольких людей? **Рекомендация:** Уточнить identity/адресата; sensitive actions не полагать на биометрию

### 26 · Proactive background assistant

**Контекст:** Optional scheduled suggestions and follow-ups. **Граница:** require user-defined opt-in and budget. **Этап:** Future. **Основной риск:** intrusive automation.

- **Q0626 · PROPOSAL / OPEN** — Нужен ли Secretary heartbeat с расписанием? **Рекомендация:** Только после opt-in и бюджета, не обязательный v1
- **Q0627 · PROPOSAL / OPEN** — Может ли Secretary предлагать купить билеты заранее? **Рекомендация:** Да как proposal, платёж требует отдельного согласия
- **Q0628 · PROPOSAL / OPEN** — Как не будить пользователя ночью? **Рекомендация:** Quiet hours/per-channel preferences
- **Q0629 · PROPOSAL / OPEN** — Что при недоступности Home Assistant? **Рекомендация:** Не симулировать сделанное действие, сообщить о проблеме
- **Q0630 · PROPOSAL / OPEN** — Может ли Task ждать будущего условия? **Рекомендация:** Да через durable triggers, но не бесконтрольные фоновые модельные циклы

### 27 · Browser computer use and cross-app actions

**Контекст:** Use user's tools and environment to operate software. **Граница:** constrain arbitrary action and clarify completion. **Этап:** Future. **Основной риск:** web prompt injection.

- **Q0651 · PROPOSAL / OPEN** — Можно ли поручить Secretary работу в браузере? **Рекомендация:** Да на Execution Node с дозволенным browser/desktop tool
- **Q0652 · PROPOSAL / OPEN** — Нужен ли постоянный screen capture? **Рекомендация:** Нет, запрашивать по задаче с понятным присутствием/доступом
- **Q0653 · PROPOSAL / OPEN** — Можно ли доверять инструкциям с веб-страницы? **Рекомендация:** Нет, web content — untrusted data
- **Q0654 · PROPOSAL / OPEN** — Как подтверждать онлайн-заказ? **Рекомендация:** Различать preview cart и final purchase
- **Q0655 · PROPOSAL / OPEN** — Какие ОС поддерживают computer use? **Рекомендация:** Явная capability матрица, live acceptance для каждого

### 28 · Home Assistant architecture and API

**Контекст:** Native interoperable home control via Home Assistant. **Граница:** preserve local automation without cloud. **Этап:** Future. **Основной риск:** cloud dependence.

- **Q0676 · PROPOSAL / OPEN** — Нужно ли Secretary напрямую управлять Zigbee? **Рекомендация:** Нет, первый адаптер через Home Assistant API
- **Q0677 · PROPOSAL / OPEN** — Где работает Home Bridge? **Рекомендация:** Локально рядом с Home Assistant, outbound secure connection к Secretary
- **Q0678 · PROPOSAL / OPEN** — Что при отключении интернета? **Рекомендация:** Локальные HA automations продолжают работать, облачный ассистент деградирует
- **Q0679 · PROPOSAL / OPEN** — Как обнаружить доступные устройства? **Рекомендация:** HA entity registry/services, capability allowlist и room mapping
- **Q0680 · PROPOSAL / OPEN** — Можно ли назвать выключенную лампочку выключенной до подтверждения? **Рекомендация:** Нет, дождаться service/result и state verification

### 29 · Zigbee Thread Matter MQTT and devices

**Контекст:** Home device network via existing adapters and hubs. **Граница:** Secretary is not a new radio protocol. **Этап:** Future. **Основной риск:** fragmented devices.

- **Q0701 · PROPOSAL / OPEN** — Является ли Matter тем же, что Thread? **Рекомендация:** Нет, Matter application interoperability; Thread сетевой transport
- **Q0702 · PROPOSAL / OPEN** — Заменяет ли Zigbee роутер? **Рекомендация:** Нет, coordinator/bridge нужен в зависимости от сети
- **Q0703 · PROPOSAL / OPEN** — Нужно ли Secretary реализовать Zigbee2MQTT? **Рекомендация:** Нет, использовать HA/ZHA/Zigbee2MQTT, где это уместно
- **Q0704 · PROPOSAL / OPEN** — Что делать с несовместимым устройством? **Рекомендация:** Показать capability absence и setup guide
- **Q0705 · PROPOSAL / OPEN** — Нужно ли покупать конкретный hub? **Рекомендация:** Нет универсального требования, список протестированных альтернатив позже

### 30 · Smart home scenes rooms and guests

**Контекст:** Contextual room-aware actions and home routines. **Граница:** safety-class actions demand scoped consent. **Этап:** Future. **Основной риск:** guest overreach.

- **Q0726 · PROPOSAL / OPEN** — Как различать две лампы с именем «Свет»? **Рекомендация:** Room/entity IDs плюс запрос уточнения
- **Q0727 · PROPOSAL / OPEN** — Можно ли объединять свет/музыку/климат в сцену? **Рекомендация:** Да, HA scene/script как атомарная доменная команда где доступно
- **Q0728 · PROPOSAL / OPEN** — Можно ли гостю выключить весь дом? **Рекомендация:** Только по настройке guest capabilities
- **Q0729 · PROPOSAL / OPEN** — Чем отличаются локальная automation и AI recommendation? **Рекомендация:** HA исполняет deterministic rules, Secretary формирует намерения/сценарии
- **Q0730 · PROPOSAL / OPEN** — Что делать если термостат занят физическим переключателем? **Рекомендация:** Читать актуальное состояние и не навязывать конфликт

### 31 · Automobile core concepts and safety

**Контекст:** Capability-based unified interface to supported cars. **Граница:** cannot promise drive-any-car universal actuator. **Этап:** Future. **Основной риск:** physical safety.

- **Q0751 · PROPOSAL / OPEN** — Означает ли «Grok отвези меня» управление рулём? **Рекомендация:** Нет; навигация и физическое вождение — разные классы действий
- **Q0752 · PROPOSAL / OPEN** — Как определить функции конкретного автомобиля? **Рекомендация:** Discover supported commands per VIN/provider/model, не hardcode для всех машин
- **Q0753 · PROPOSAL / OPEN** — Что если авто движется? **Рекомендация:** Парковочные и опасные функции блокировать согласно OEM state/политике
- **Q0754 · PROPOSAL / OPEN** — Может ли OBD-II открыть окно на любой машине? **Рекомендация:** Нет, стандартная диагностика не даёт универсального легального actuator API
- **Q0755 · PROPOSAL / OPEN** — Может ли сторонний AI управлять тормозами/рулём? **Рекомендация:** Не включать без безопасного OEM-certified пути; по умолчанию исключить

### 32 · Tesla Fleet API and commands

**Контекст:** Owner-authorized Tesla connector via Fleet API. **Граница:** virtual key and command availability vary. **Этап:** Future. **Основной риск:** sensitive unlock/drive operations.

- **Q0776 · PROPOSAL / OPEN** — Как Secretary получает доступ к Tesla? **Рекомендация:** OAuth scopes/Fleet API и owner-mediated virtual-key pairing при необходимости
- **Q0777 · PROPOSAL / OPEN** — Какие Tesla-команды реалистичны? **Рекомендация:** Навигация, климат, зарядка, некоторые media/windows/locks по конкретной машине и API
- **Q0778 · PROPOSAL / OPEN** — Может ли Secretary открыть окно во время движения? **Рекомендация:** Только при подтверждённой доступности/безопасном состоянии; OEM API может не поддерживать
- **Q0779 · PROPOSAL / OPEN** — Где хранить signing key для Tesla commands? **Рекомендация:** Защищённый connector/secret store с отзывом
- **Q0780 · PROPOSAL / OPEN** — Что делать если Tesla API command возвращает receipt, но авто offline? **Рекомендация:** Состояние pending/unknown и проверка, не утверждать success

### 33 · Non-Tesla OEM and Smartcar connectors

**Контекст:** Vendor-dependent remote car capabilities across brands. **Граница:** no universal window/motion controls via standard API. **Этап:** Future. **Основной риск:** false equivalence.

- **Q0801 · PROPOSAL / OPEN** — Может ли Smartcar управлять любой маркой? **Рекомендация:** Нет, только участвующие бренды/машины/доступные функции
- **Q0802 · PROPOSAL / OPEN** — Как организовать per-OEM plugins? **Рекомендация:** Capability adapters плюс покрытие моделей и версий
- **Q0803 · PROPOSAL / OPEN** — Можно ли заменить отсутствующий API машиной с OBD dongle? **Рекомендация:** Для безопасного диагностического чтения возможно, не считать автоматическим actuators мостом
- **Q0804 · PROPOSAL / OPEN** — Что с оплатой стороннего vehicle API? **Рекомендация:** Показывать API costs, quota, разрешения
- **Q0805 · PROPOSAL / OPEN** — Что делать если владелец отзывает car access? **Рекомендация:** Никаких дальнейших команд, явная ошибка и tool revocation

### 34 · In-car voice navigation and media

**Контекст:** Hands-free car companion for navigation music cabin controls. **Граница:** driving-critical functions are out of scope without OEM support. **Этап:** Future. **Основной риск:** distraction.

- **Q0826 · PROPOSAL / OPEN** — Как отправить адрес в навигацию авто? **Рекомендация:** Через OEM command либо поддерживаемую phone mapping/navigation handoff
- **Q0827 · PROPOSAL / OPEN** — Можно ли менять музыку в любом авто? **Рекомендация:** Через доступную телефонную media session/партнёрское API, не обещать прямой доступ к head unit
- **Q0828 · PROPOSAL / OPEN** — Как распознать контекст «я за рулём»? **Рекомендация:** Надёжный car/phone context, при сомнении не расширять права
- **Q0829 · PROPOSAL / OPEN** — Нужно ли управление стеклоподъёмниками из голосового ассистента? **Рекомендация:** Только через явно документированный OEM capability + safety state
- **Q0830 · PROPOSAL / OPEN** — Должен ли Secretary сам начинать движение автомобиля? **Рекомендация:** Нет, planning/navigation без autonomous driving control

### 35 · Payments transfers purchases and booking

**Контекст:** Authorized multi-person transactional workflows. **Граница:** proposals are not transfers or bookings. **Этап:** Later. **Основной риск:** partial payment failure.

- **Q0851 · CONFIRMED** — Что означает «Z бронирует за всех»? **Рекомендация:** Circle распределяет ответственных, Secretary Z исполняет подтверждённое бронирование
- **Q0852 · CONFIRMED** — Может ли Circle предлагать займ? **Рекомендация:** Да, как вариант; Z должен явно принять условия
- **Q0853 · CONFIRMED** — Кто запрашивает долю Ника и Оры? **Рекомендация:** Их персональные Secretaries по scoped request от Circle
- **Q0854 · CONFIRMED** — Можно ли выполнить платёж сразу после согласия в чате? **Рекомендация:** Только если подключённый payment tool поддерживает точную авторизацию
- **Q0855 · PROPOSAL / OPEN** — Что если платёж успешен, а бронь упала? **Рекомендация:** Durable pending/reconciliation, компенсационные варианты по новому согласию

### 36 · Calendar email travel and shared planning

**Контекст:** Personal schedule access and delegated trip coordination. **Граница:** private calendar raw events remain private. **Этап:** Later. **Основной риск:** unwanted disclosure.

- **Q0876 · PROPOSAL / OPEN** — Когда Circle может читать календарь? **Рекомендация:** Персональный Secretary выдаёт free/busy или релевантное ограничение
- **Q0877 · PROPOSAL / OPEN** — Нужен ли общий семейный календарь? **Рекомендация:** Опционально отдельно от private calendars
- **Q0878 · PROPOSAL / OPEN** — Как договариваться о датах без раскрытия подробностей встреч? **Рекомендация:** Предлагать доступные слоты, не личные названия событий
- **Q0879 · PROPOSAL / OPEN** — Может ли Secretary написать email приглашённому? **Рекомендация:** Только через разрешённую интеграцию и с явным адресатом
- **Q0880 · PROPOSAL / OPEN** — Когда бронь поездки становится общим артефактом? **Рекомендация:** После подтверждения участниками в Circle, с ограничениями доступа

### 37 · Integration SDK adapters and ecosystem

**Контекст:** Extensible connectors built on declared capabilities. **Граница:** no privileged arbitrary plugin by default. **Этап:** Future. **Основной риск:** supply chain risk.

- **Q0901 · PROPOSAL / OPEN** — Нужно ли каждое внешнее приложение писать в Core? **Рекомендация:** Нет, provider adapters/SDK с версионированными capabilities
- **Q0902 · PROPOSAL / OPEN** — Должен ли быть магазин интеграций в v1? **Рекомендация:** Нет, протокол сначала, marketplace позже
- **Q0903 · PROPOSAL / OPEN** — Как внешнее приложение объявляет permissions? **Рекомендация:** Типизированные scopes и explicit user grants
- **Q0904 · PROPOSAL / OPEN** — Как защитить third-party plugins? **Рекомендация:** Signed releases, provenance и изоляция по риску
- **Q0905 · PROPOSAL / OPEN** — Можно ли подключить самописный Home Bridge? **Рекомендация:** Да после проверки auth, schema и host boundaries

### 38 · Setup upgrades services and onboarding

**Контекст:** One command plus terminal setup then simple controls. **Граница:** no mandatory cloud account in dev v1. **Этап:** Baseline. **Основной риск:** fragile installation.

- **Q0926 · PROPOSAL / OPEN** — С чего начинается первая установка? **Рекомендация:** Проверка OS, harnesses/auth и нужных компонентов, затем terminal wizard
- **Q0927 · PROPOSAL / OPEN** — Нужен ли обязательный браузер? **Рекомендация:** Нет, Developer Edition terminal-first, Web опционален
- **Q0928 · PROPOSAL / OPEN** — Можно ли менять настройки без TOML вручную? **Рекомендация:** Да через secretary config TUI
- **Q0929 · PROPOSAL / OPEN** — Нужен ли Windows installer? **Рекомендация:** По платформенной release матрице, не считать поддержку доказанной наличием сборки
- **Q0930 · PROPOSAL / OPEN** — Как обновлять службу без потери Task? **Рекомендация:** Graceful quiesce, upgrade schema, native session compatibility checks

### 39 · Consumer Edition pricing and managed ops

**Контекст:** Same product core with easy managed onboarding. **Граница:** billing provider supply and isolation later. **Этап:** Future. **Основной риск:** unclear tenant isolation.

- **Q0951 · BASELINE** — Нужен ли отдельный продукт для Consumer Edition? **Рекомендация:** Нет, единое ядро с другим managed provisioning
- **Q0952 · BASELINE** — Кто оплачивает inference у consumer? **Рекомендация:** Легальный коммерческий provider/compute путь, модель не зафиксирована
- **Q0953 · PROPOSAL / OPEN** — Можно ли использовать потребительские подписки одних клиентов для других? **Рекомендация:** Нет; не считать их переносимым коммерческим supply
- **Q0954 · BASELINE** — Как изолировать разных customers? **Рекомендация:** Hard tenant isolation данных, credentials и compute с penetration/live tests
- **Q0955 · PROPOSAL / OPEN** — Должен ли signup требовать знания ACP? **Рекомендация:** Нет, consumer onboarding hides models/harness management

### 40 · Testing CI release acceptance and governance

**Контекст:** Evidence-driven end-to-end product delivery. **Граница:** simulated tests do not prove real native readiness. **Этап:** Baseline. **Основной риск:** misleading shipping claims.

- **Q0976 · PROPOSAL / OPEN** — Достаточны ли unit tests для заявленного v1? **Рекомендация:** Нет, black-box API + channel + Node + live native acceptance
- **Q0977 · PROPOSAL / OPEN** — Как фиксировать PR review на exact HEAD? **Рекомендация:** SHA-stable review и CI statuses для именно этого HEAD
- **Q0978 · PROPOSAL / OPEN** — Можно ли сливать draft по завершению checklist? **Рекомендация:** Нет, явное owner sign-off требуется
- **Q0979 · PROPOSAL / OPEN** — Какие failure cases включить для денежных/car/home actions? **Рекомендация:** Offline, timeout, retry, revoked permission, duplicate command и confirmed receipt
- **Q0980 · PROPOSAL / OPEN** — Можно ли объявлять feature shipped без published acceptance? **Рекомендация:** Нет, разделять target, code evidence, integration evidence и live verification

<!-- CATALOGUE_END -->

## Product architecture: logical target (PROPOSED, NOT IMPLEMENTATION)

```text
Person (Nick) -> Personal Secretary + private Conversation + Memory
                              |
                              +-> Task -> Worker -> Harness on Execution Node
                              |                        (local / VPS / microVM)
                              |
                              +-> Tools/Integrations -> Home Bridge -> Home Assistant
                              |                      -> Vehicle Connector -> OEM/API
                              |                      -> Calendar/Browser/Other
                              |
                              +-> Circle <-> other Personal Secretaries / Personas
                                            shared scoped Decisions and agreed actions

Secretary control plane: self-hosted / VPS / supported cloud / Cloudflare Workers+DO
Artifacts: portable object/file store; secrets: deployment-appropriate vault
Channels: Web/PWA, Telegram, later voice satellite / desktop / mobile
```

This is a logical separation, not a mandate for a process per box. In the current code the central Go service + SQLite and Node runtime remain the source of truth until a separately tested serverless port exists. Do not assume the Cloudflare runtime can directly start shell/harness processes. DO/storage implementation and cross-install federation remain open.

## Candidate milestones / MVP-to-future boundary (RECOMMENDED / OPEN)

| Stage | User-visible outcome | Not included by implication |
| --- | --- | --- |
| **M0 Current alpha stabilization** | Reliable Secretary → Worker → Result/Follow-up, mature native Codex/Claude session lifecycle, cancellation, package/install | Claims of release-ready cross-platform production without live evidence |
| **M1 Developer MVP / public-v1 gate** | Self-hosted personal Secretary with Telegram/Web/PWA, Workers on local+remote Nodes, media/files, explicit memory, tested backup and recovery | Consumer multi-tenancy, Circles, home voice, car actuation or managed microVM |
| **M2 Hosted control plane experiment** | Same personal identity in a supported hosted/serverless deployment, outbound Nodes, portable central data | Fully hosted long-running native CLI inside Cloudflare Workers |
| **M3 Home Assistant slice** | Telegram/Web command to one allowlisted light, through home-local Bridge, visible verified state and offline handling | Full replacement for Home Assistant or first-party driver for every device |
| **M4 Realtime voice slice** | Voice input→Secretary→safe action→TTS, wake-word and room-aware speaker context where devices support it | Unconditional speaker biometric authorization or guaranteed instant response |
| **M5 Circles prototype** | Two independent Secretaries propose/accept a movie/meal choice in private DM and shared Circle log | Federated financial transactions by default |
| **M6 Cross-install Circles + delegated actions** | Multi-person travel planning, scoped facts, authenticated A2A, consent, payment/booking multi-stage receipts | Unverified spending, automatic assumed agreement or uncontrolled model loops |
| **M7 Vehicle integrations** | Owner-authorized supported Tesla / participating OEM status and safe documented functions; car voice navigation/media when supported | Steering/braking/autonomous driving or every command on every car |
| **M8 Consumer experience** | One-core managed installation, personal isolation and legitimate inference/compute provisioning | Monetization or account model treated as already decided |

These are sequencing suggestions. Dates, release numbers, acceptance gates and which milestones ship together require explicit owner approval.

## Deep architectural sketch — Smart home (PROPOSED)

1. **Bridge:** Home Assistant handles Zigbee, Thread/Matter, Wi-Fi devices and automations via its integration ecosystem; Secretary never needs to reimplement radio drivers.
2. **Connectivity:** a locally running Home Bridge enrolls with a chosen Secretary and initiates outbound authenticated connection. Owner gives a narrow allowlist of rooms, devices, services and actions; avoid exposing Home Assistant admin API to the public internet.
3. **Discovery:** discover HA entities, areas and supported services; translate them into typed operations (`turn_on`, `set_brightness`, `set_temperature`) with explicit device IDs rather than free-form model-authored API bodies.
4. **Execution:** intent → typed plan → policy/authorization → bridge invocation with action identity → Home Assistant service action → read-back/receipt → truthful conversation reply.
5. **Offline:** Home Assistant still runs deterministic local automations without Internet; hosted Secretary reports disconnected, does not claim a light switched.
6. **Voice:** optional satellite/mic/push-to-talk → wake word/ASR → Secretary → action/answer → TTS. Room/presence inferred from paired device with fallbacks, not from guessed biometrics.
7. **Security:** household guests/children, windows, doors, climate, alarms and irreversible operations have individually configurable controls. Do not make Owner full-access CLI implicitly an unrestricted smart-home credential.
8. **Acceptance:** test fake Home Assistant API errors, stale state, reconnect, duplicate command, revoked credential, and one live lamp/sensor end-to-end before declaring usable.

Open alternatives: Home Assistant add-on vs independently installed Bridge, direct managed HA cloud API vs local bridge, whether common Execution Node handles HA capabilities, routing multiple homes/ownership.

## Deep architectural sketch — Vehicle integrations (PROPOSED)

Vehicle access must be **capability-based per connected car**, not a promise that every manufacturer exposes identical commands.

1. **Read-only phase:** vehicle charging/range/status/location where OEM supports and user grants them; no assumption that standard OBD-II telemetry includes remote actuators.
2. **Tesla phase:** use official Fleet API/OAuth with correct permissions, virtual-key pairing where required, key protection, per-model supported operation discovery and verification. Documented commands include climate, navigation, media and some parked-car window operations, subject to vehicle/API restrictions.
3. **Cross-brand phase:** adapter through an aggregator (e.g. Smartcar) when supported, or direct manufacturer-authorized APIs. Show missing capabilities honestly; do not claim universal OEM coverage.
4. **In-car phase:** use permitted mobile companion / OS projection or OEM app surface for voice, music and navigation when available. This is distinct from remotely controlling physical vehicle components.
5. **Safety boundary:** Secretary may suggest routes, send destination or request supported non-driving comfort controls. It must not turn a user phrase like "отвези меня" into steering/braking/acceleration commands; no unsafe aftermarket actuator claims.
6. **Authority:** car owner/enrolled user grants per-vehicle commands; unlock, location and other sensitive operations require relevant confirmation policy. Never use mere voice similarity for strong vehicle authority.
7. **Reliability:** vehicle might be moving, asleep, offline, rate-limited or in a different state; check prerequisites, keep pending/unknown receipt separate from success, and suppress blind retry.
8. **Compatibility matrix:** provider, country, model, model year, firmware, permission, command set, last verified date and test evidence.
9. **Deliverable:** small Tesla proof-of-concept and one cross-brand adapter evaluation, then production release commitments if proven. No guarantee that every car can open its windows or accept steering.

Research: Tesla Fleet API https://developer.tesla.com/docs/fleet-api/endpoints/vehicle-commands ; virtual key https://developer.tesla.com/docs/fleet-api/virtual-keys/developer-guide ; cross-brand https://smartcar.com/product/commands .

## Deep architectural sketch — Circles and multi-person actions (PART CONFIRMED, PART OPEN)

- **Confirmed:** independent Person/Personal Secretary private histories, shared Circle sessions; task-relevant scoped memory summaries; intentional shared chat; invitations; Circle can propose and negotiate autonomously, but cannot turn a suggestion into another person's commitment.
- **Open:** Circle session vs recurring group membership identity; shared host; inter-server transport; voting/delegation policy; budget limits; retention of shared facts; account deletion; who pays for facilitation; release boundary.
- **Example 1:** Nick wants a movie; Nick's and Ora's Secretaries share genre constraints (not their entire personal chats), negotiate candidates, request agreement if required, store one shared Decision.
- **Example 2:** X/Y/Z plan 1–2 day trip. Secretaries can share limited budget/date/activity constraints, negotiate options and make proposals, even proposing a loan. Proposal stays hypothetical until its recipient consents.
- **Example 3:** Participants authorize Z to book and authorize each person's specific payment through their own Secretary, if a compatible payment provider is connected. Circle tracks stages and receipts; booking failure triggers reconciliation rather than inventing a successful trip.
- **Do not** conflate A2A messages with durable consent; a Secretary's model prediction that its user will agree is not a human signature. Personal DMs and shared Circle logs have different viewers and access policies.

## Circles — remaining questions already enumerated in the draft PR (UNREVIEWED)

## Q4 — Remaining design questions for batch review

The following **13 known outstanding decisions (Q4h–Q4t)** were collected at the owner's request rather than drip-fed one at a time. They are **recommendations, not accepted decisions**. Dependency order is indicated by grouping; if an upstream answer changes, its downstream recommendations must be revisited. The Circle model below supersedes the earlier household-wide Secretary hypothesis. Speaker identification, children and voice authorization were explicitly set aside for now.

### Identity, membership, topology

**Q4h — Circle session vs recurring group.** Does reusing a family Circle mean reopening one shared history or starting a new Circle with the same participants/settings? **Recommend:** A Circle is one scoped collaboration around a goal, with durable history; recurring membership is an optional reusable group/template that starts new Circles. Reopen the same Circle only to continue its original goal. Do not merge unrelated movie, holiday and household histories.

**Q4i — Decision participants.** If a Circle has Nick, Ora and Terry, can Nick and Ora make a decision without including Terry? **Recommend:** Yes; Circle membership and per-Decision participants are distinct. Only involved participants receive its scoped details and consent requests.

**Q4j — Independent deployments and federation.** Should Nick's cloud Secretary collaborate with Ora's self-hosted Secretary without moving either person's state to one provider? **Recommend:** Yes as an architectural goal through authenticated, consented cross-deployment Circle/A2A federation; start with supported deployments and defer broad interoperability until tested.

**Q4k — Coordination ownership and compute costs.** Who hosts the Circle's canonical state and pays for shared facilitation if participants use different infrastructure/subscriptions? **Recommend:** A designated Circle host owns durable shared state, initially defaulting to its creator's installation. Each personal Secretary pays for its own inference; explicit Circle budget/owner covers optional shared model work. Participants do not silently subsidize others.

### Intelligence and agreement

**Q4l — Circle coordinator vs another full AI.** Should Circle have an autonomous top-level model that directs participants, or a durable process whose participating Secretaries do the reasoning? **Recommend:** Durable coordinator with structured proposals, evidence, decisions and an optional LLM facilitator for synthesis; personal Secretaries retain private reasoning and tools. A2A transmits typed collaboration messages, not an unbounded free-form debate.

**Q4m — Rule for accepting a decision.** Is a recommendation accepted only unanimously, by majority, or by an explicitly chosen decision policy? **Recommend:** An explicit policy per Decision: unanimity for shared commitments and cost-bearing actions by default; voting/delegated choices only when participants agreed to that policy. Recommendations can exist without any approval.

**Q4n — Proactive initiation and invitations.** May Secretary initiate a Circle without its owner's immediate request, e.g. noticing conflicting holiday schedules? **Recommend:** It may suggest creating a Circle automatically, but invitation dispatch and autonomous creation require an explicit prior user mandate or per-case confirmation. Existing trusted recurring-group rules from Q4g still apply.

**Q4o — Absent people, stalemates and changing minds.** What if Z is offline, a deadline passes or one participant changes their answer? **Recommend:** Async Circle with explicit pending/declined/timed-out states; deadlines are configurable; no invented consensus. Recompute unexecuted plans if consent changes, but do not pretend external actions can always be undone.

### Data, authority and lifecycle

**Q4p — Shared context retention and provenance.** Do summaries offered by Secretaries become permanent shared memory, even if they originated in private context? **Recommend:** Store only the minimum disclosed information necessary to explain a Circle Decision, record the source and sharing scope, and expire or archive detailed contextual snapshots by policy. Do not automatically write every shared statement into any participant's personal long-term memory.

**Q4q — Leaving, removal and history.** If Ora leaves a Circle, what happens to her previously shared messages and completed decisions? **Recommend:** Revoke future access/sharing immediately; retain existing decision/audit records according to an explicit retention policy and allow deletion/redaction where feasible. Never promise that already viewed disclosures can be retracted.

**Q4r — Multi-party external actions and partial failures.** When Nick and Ora approve payment and Z's Secretary books, who owns each step, and what if one payment succeeds while the booking fails? **Recommend:** Every person/Secretary executes only their individually authorized tools; Circle tracks an auditable multi-stage plan, exact approvals and external receipts. Failed/uncertain steps stop further execution and are reconciled before retrying; no silent duplicate transfers or bookings. Circle may propose compensation/refunds, not assume they succeeded.

**Q4s — A2A negotiation limits.** How do we avoid an endless Secretary-to-Secretary argument or unbounded subscription/API spending? **Recommend:** Per-Circle limits on turns, duration, external actions and inference spend; show usage/limits and require an explicit extension if exceeded.

### Release boundary

**Q4t — Which release gets Circles?** Must multi-person Circle federation, payments and bookings ship with the first public v1, or remain a later capability while Secretary first stabilizes personal use? **Recommend:** Preserve the existing single-person release as the first milestone; treat Circles as a clearly documented future product capability, with prototype slices permitted sooner. No change to public-v1 acceptance criteria until explicitly chosen.

## Historical grilling register — October 17 baseline (59 answered questions)

> This is retained as an explicitly historical baseline. Later Q1–Q4g decisions above take precedence for future **proposals**, but do not modify current Public v1 behavior until the revised canonical spec is approved.

### Secretary — Grilling decisions (17 October 2026)

> Compact **question → owner's decision** record of the design interview. These are summarized decisions, **not verbatim quotes**. Where an answer was revised in a later round, the **latest answer wins**. See [canonical spec](spec.md) for normative product requirements.

## Round 1 — Product foundation

| Q | Question | Final decision |
| --- | --- | --- |
| Q1 | Who is Secretary for? | First release for developers already using paid native harnesses; later a mass-market assistant including customers in Russia and other markets. |
| Q2 | Which harnesses are mandatory for Public v1? | **Codex and Claude Code both supported and tested.** Each user can have just one. |
| Q3 | What is the source of product requirements? | **spec/spec.md** canonical, based on authored old_spec.md. Preserve original/references and distinguish implemented, planned and rejected work. |
| Q4 | What durability can Secretary promise? | Preserve intent/history/state; **no guaranteed exactly-once side effects** or automatic blind retries of uncertain execution. |
| Q5 | Should Secretary manage child Workers/subagents? | **No Secretary-managed Child Workers.** Whatever internal subagents native harnesses use is not Secretary's concern. |
| Q6 | Must Secretary answer after a Worker Result? | No. Deliver Worker Result directly by default, without a second Secretary LLM turn; optional per-Worker summary later. |
| Q7 | Should Secretary build sandbox, read-only and smart approvals? | **No. Full-access by default; remove read-only from v1 scope; no new approval system.** Harness owns native tool behavior. |
| Q8 | How should the product be distributed? | Open-source self-hosted Developer Edition first; paid service offering later. |

## Round 2 — Business model and release

| Q | Question | Final decision |
| --- | --- | --- |
| Q9 | What does a nontechnical customer buy? | A **working paid Secretary assistant**. Seller provides inference and potentially compute/microVMs; do **not** design inference sourcing here. |
| Q10 | One product or different products for developers/consumers? | **One core product.** Developer Edition reuses the person's native subscriptions; Consumer Edition provides inference/compute. |
| Q11 | What gates first Public v1? | Essential Codex/Claude, reliable Worker lifecycle, chat/channels/Nodes and usable setup; defer advanced memory, skills learning, scheduler and Consumer Edition. |

## Round 3 — Secretary behavior and installation

| Q | Question | Final decision |
| --- | --- | --- |
| Q12 | Default permission modes? | **Full-access only for now.** Remove read-only mode from v1; avoid Secretary-level approvals. |
| Q13 | Direct answers or delegate? | **Heavily delegate** substantive questions, research and even evolving project conversations. Secretary answers only super-simple questions. |
| Q14 | Can Secretary use any harness? | Yes. Secretary's harness is independently configurable from Workers. |
| Q15 | Who chooses a Worker harness/Node? | Secretary normally chooses automatically; user may override explicitly. |
| Q16 | Browser wizard or terminal setup? | **Beautiful terminal-only interactive installer**, started from a pasted installation link/command. |
| Q17 | How much memory in v1? | **memory.md + user_preferences.md** suffice. Existing SQLite conversation/lifecycle remains. |
| Q18 | How should Consumer Edition feel? | Customer pays Secretary and starts chatting; no technical model or VM setup. |

## Round 4 — Continuity, Projects and research

| Q | Question | Final decision |
| --- | --- | --- |
| Q19 | Does Worker receive previous conversation context? | Yes. Secretary passes relevant discussion/decisions, not just the latest utterance. |
| Q20 | Reuse Worker for related work? | Yes. Continue same persistent Worker where applicable; new Worker for unrelated work. |
| Q21 | Are Projects necessary? | **Not mandatory.** Ordinary users mostly chat or manipulate files. Existing developer Project plumbing can stay optional, not become a first-class required workflow. |
| Q22 | Must users install both Codex and Claude Code? | No; Public v1 **supports both**, individual owner may use only one. |
| Q23 | Must every answer use the internet? | No. Delegate substantive questions; Worker decides whether browsing/current sources are needed. |

## Round 5 — Worker question/answer loop

| Q | Question | Final decision |
| --- | --- | --- |
| Q24 | How does Worker ask a question? | As an ordinary terminal **Result for an Attempt**, without closing the Task/Worker; resume on Follow-up. |
| Q25 | Where does a user's answer go? | Secretary automatically routes it to the Worker that asked, when context is clear. |
| Q26 | May user switch topics while Worker awaits an answer? | Yes. Unfinished Worker remains available. |
| Q27 | Where are Worker questions shown? | Directly in the shared Personal Conversation with Worker attribution, without Secretary echo. |
| Q28 | Does successful completion destroy Worker? | No. Worker persists for later follow-up until user/Secretary explicitly closes or archives it. |

## Round 6 — UI, Skills, memory and platforms

| Q | Question | Final decision |
| --- | --- | --- |
| Q29 | Which channels for Public v1? | **Web + installable PWA + Telegram DM and optional forum group** with General and per-Worker Topics. |
| Q30 | Can user talk to Worker directly? | Yes: through Secretary or direct Web observer/Telegram Worker Topic. |
| Q31 | Active steering versus queue? | Plain message attempts steering immediately; explicit **/q** means FIFO queued follow-up; no silent fallback. |
| Q32 | Installation interface? | All initial configuration in **terminal**; browser not required. |
| Q33 | Central Skills Registry now? | No. Use **native harness Skills**; registry/auto-learning later. |
| Q34 | Who writes long-term memory? | Secretary reads memory files; **writes facts/preferences only by explicit user request** in v1. |
| Q35 | Is file support text-only? | **No**: images, audio, videos, documents in both directions; agents can return files. |
| Q36 | Supported operating systems? | Secretary Server **macOS/Linux**; Execution Nodes **macOS/Linux/Windows**. |

## Round 7 — Synchronization and multimedia

| Q | Question | Final decision |
| --- | --- | --- |
| Q37 | Mirror Telegram DM and group General? | Yes, **both directions**, one canonical conversation; no duplicate execution from mirroring. |
| Q38 | Who may issue commands via group? | **One owner only** in Developer Edition, linked Telegram identity; being a group member confers no access. |
| Q39 | What is PWA? | Installable version of Web using same Server; full offline/push later. |
| Q40 | Where are Artifacts stored? | **One Artifact Store on the same host as Secretary Server**. Nodes access by server-owned references. |
| Q41 | How to process videos? | Keep original, sample timestamped frames (e.g. 1 FPS); **rate and processing limits are configurable**. Optionally transcribe audio. |
| Q42 | How to handle voice/audio? | Keep original plus transcript as linked Artifacts; reuse existing ASR where practical. |
| Q43 | Can agents send created files? | Yes, Secretary and Worker can return files to Web/PWA and Telegram. |
| Q44 | What if file exceeds limits? | **Reject with warning** for now; never silently truncate. |

## Round 8 — Data and setup

| Q | Question | Final decision |
| --- | --- | --- |
| Q45 | Artifact retention? | Keep until explicit deletion; archiving Worker does not delete files. |
| Q46 | How is generated file published? | Worker explicitly publishes Artifact; no arbitrary auto-scan of Node filesystem. |
| Q47 | Who preprocesses audio/video? | Secretary Server handles intake/storage/common conversions; Worker can still access original. |
| Q48 | What if Node goes offline? | Show offline, preserve Task, **do not automatically migrate existing Worker**. New tasks may use another Node. |
| Q49 | How to change config later? | Interactive **secretary config** terminal UI; files remain editable. |
| Q50 | How is Secretary operated? | Simple start/stop/restart/status/logs/update commands; OS-native background services. |
| Q51 | Need a new owner account/password? | **No. Reuse existing Web bootstrap code/token and owner session cookie**, already implemented. |
| Q52 | Is Telegram group mandatory? | No. DM alone is fully functional; connect group/topics later if wanted. |
| Q53 | What qualifies as Public v1? | Independent installation, supported Codex/Claude, Worker lifecycle, Web/PWA, Telegram, media/files, remote Nodes, persistence/recovery and tested OS matrix. |

## Round 9 — Final storage, reliability and documentation

| Q | Question | Final decision |
| --- | --- | --- |
| Q54 | Does closing/deleting Worker delete its Artifacts? | Closing/archive **does not**; explicit deletion is separate, cleans unreferenced derived data. |
| Q55 | Backups? | Provide backup/restore of SQLite, files, memory and config; **remote native sessions not guaranteed** by Server backup. |
| Q56 | What if conversion or ASR fails? | Preserve original, show clear error, do not claim missing content was analyzed. |
| Q57 | Can media processing block chat? | No; process asynchronously and gate only messages that depend on that Artifact. |
| Q58 | Must Developer Edition host public remote Web? | No; localhost/LAN/owner-managed secure access. |
| Q59 | How should canonical spec be structured? | Product Contract, Architecture Contract, Release Scope and code-gap ledger; distinguish actual implementation from goals. |

## Final confirmation

The owner approved all final Q54–Q59 answers with "yes lgtm", requested that this grilling Q&A be saved as a file, supplied the to-spec workflow, and permitted creation of ADR files. No further design interview is required before drafting the approved specification.

### Reading order

1. [Canonical spec](spec.md) — normative desired product.
2. This file — concise **Q → decision** record.
3. [ADRs](../docs/adr/) — long-lived rationale/tradeoffs.
4. [Runtime contracts](../docs/architecture/runtime-contracts.md) — currently implemented protocol behavior.


## Reviewer checklist for this master document

- The owner should review **confirmed/current**, **historical v1 baseline**, and **proposed future questions** separately; one does not imply approval of another.
- Remaining original Q IDs have intentional gaps after deduplication; Q4h–Q4t remain historical references. Interview only unresolved material decisions, not a quota.
- Every follow-up proposal must say what dependency it requires; ask one question at a time when grilling resumes.
- A feature can be a dream, a proposed requirement, a baseline goal, an implemented behavior, or live-accepted. These are **different** statuses.
- No external integration is declared universally supported solely because an API exists.
- No actual purchases, vehicle commands, smart-home changes, billing, or releases are authorized by this spec.


## Review protocol

- Every question gets a numbered identifier, a proposed answer, a phase and a non-approval status. The owner can respond with changed IDs; only explicit approval changes statuses.
- Resolved terms update `GLOSSARY.md` at the appropriate point. ADRs are reserved for surprising, hard-to-reverse trade-offs with genuine alternatives.
- The owner can ask to expand any branch, and new prerequisites invalidate only dependent decisions.
- Final sign-off is required before treating the new vision as canonical requirements or merging PR #212. No code changes are implied.
