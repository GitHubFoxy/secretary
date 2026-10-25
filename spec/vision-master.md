# Secretary — Master Vision & Product Design Tree

> **Draft/for review.** Long-term product vision plus complete decision checklist. Not production behavior, not a release claim, and not a final owner-approved spec. This belongs to draft PR #212 and will only supersede `spec/spec.md` after the owner explicitly signs off.
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

<!-- CATALOGUE_BEGIN -->
> **1000 decision questions** = 40 domains × 25 (5 context-specific + 20 lifecycle/security/operations). Each question explicitly indicates **CONFIRMED**, **BASELINE**, or **PROPOSAL / OPEN**; the older baseline is subject to this re-review. IDs are stable for review.

### 01 · Vision and product identity

**Контекст:** Universal open-source personal AI assistant across devices and physical services. **Граница:** coding-first does not mean coding-only. **Этап:** Later. **Основной риск:** inflated promises.

- **Q0001 · CONFIRMED** — Что включает определение «всегда рядом»? **Рекомендация:** Непрерывный персональный контекст и каналы, без фиктивных гарантий офлайн-действий
- **Q0002 · CONFIRMED** — Чем Secretary отличается от очередного чат-бота? **Рекомендация:** Выполняет действия через выбранные инструменты, сохраняет Task lifecycle и присутствует на устройствах
- **Q0003 · CONFIRMED** — Станет ли coding-only продукт тупиком? **Рекомендация:** Не встраивать coding-специфику в ядро identity/conversation
- **Q0004 · CONFIRMED** — Нужна ли единая личность при смене модели? **Рекомендация:** Да, user-owned state не привязан к модели
- **Q0005 · CONFIRMED** — Что первично в roadmap: идеальная Алиса или работающий продукт? **Рекомендация:** Подтверждённый coding v1, затем умный дом, голос и авто
- **Q0006 · CONFIRMED** — Как поймём, что Secretary — самостоятельный продукт, а не просто интерфейс к LLM? **Рекомендация:** Пользователь ставит задачу, Secretary сам координирует необходимые поддерживаемые инструменты/Workers, сохраняет ход работы и возвращает проверяемый результат; маркетинговое «умеет всё» не считается критерием.
- **Q0007 · CONFIRMED** — Кто является доверенным источником команд Secretary? **Рекомендация:** Только идентифицированный пользователь либо явно уполномоченная им сущность; сообщения от веб-страниц, инструментов, A2A-партнёров и сторонних интеграций — данные, а не полномочия.
- **Q0008 · CONFIRMED** — Должен ли Secretary сам начинать действия, когда его не просили прямо сейчас? **Рекомендация:** Да, как опциональная проактивность: предлагать идеи можно, а самостоятельные действия — лишь при заранее заданных пользователем правилах и пределах полномочий.
- **Q0009 · CONFIRMED** — Как Secretary поступает с неполным или неоднозначным запросом? **Рекомендация:** Мелкие несущественные детали выбирает сам и сообщает о допущениях; спрашивает только когда неопределённость влияет на существенный результат, человека, стоимость или безопасность.
- **Q0010 · CONFIRMED** — Есть ли принципиальные границы универсальности Secretary? **Рекомендация:** Да: отсутствие поддерживаемой интеграции, полномочий или надёжного способа проверить результат означает «не могу выполнить», а не имитацию работы; доступные способности зависят от устройств/провайдеров.
- **Q0011 · CONFIRMED** — Как обнаруживаются доступные функции «Vision and product identity»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0012 · CONFIRMED** — Как пользователь управляет настройками «Vision and product identity»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0013 · CONFIRMED** — Что «Vision and product identity» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0014 · CONFIRMED** — Где хранится каноническое состояние «Vision and product identity»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0015 · CONFIRMED** — Сколько должно жить состояние «Vision and product identity»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0016 · CONFIRMED** — Где именно выполняется «Vision and product identity»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0017 · CONFIRMED** — Что происходит при потере интернета? **Утверждено:** Продолжить AI-обработку можно только при наличии локальной LLM и доступного локального runtime. Тогда работают лишь локально исполнимые действия, инструменты и интеграции, не требующие сети. Если LLM доступна только в облаке, Secretary не может продолжить LLM-диалог офлайн; автономные локальные системы, например Home Assistant, могут независимо исполнять свои правила.
- **Q0018 · CONFIRMED** — Как отменять или отзывать «Vision and product identity»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0019 · CONFIRMED** — Как обрабатывать одновременные запросы к «Vision and product identity»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0020 · CONFIRMED** — Что делать после timeout или неизвестного результата «Vision and product identity»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0021 · PROPOSAL / OPEN** — Как пользователю доказать результат «Vision and product identity»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0022 · PROPOSAL / OPEN** — Кто платит за «Vision and product identity» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0023 · PROPOSAL / OPEN** — Как изолировать безопасность «Vision and product identity»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: inflated promises.
- **Q0024 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Vision and product identity»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0025 · PROPOSAL / OPEN** — В какой релиз «Vision and product identity» попадёт? **Рекомендация:** Предлагаемый этап: Later; status не повышать, пока owner и acceptance evidence не готовы.

### 02 · Person identity and personal accounts

**Контекст:** Stable independent personal identities across channels. **Граница:** no channel address or voice sample alone grants power. **Этап:** Later. **Основной риск:** shared-account leakage.

- **Q0026 · PROPOSAL / OPEN** — У одного человека может быть несколько Secretaries? **Рекомендация:** Базово один постоянный персональный Secretary; параллельные профили рассмотреть отдельно
- **Q0027 · CONFIRMED** — Nick и Ora могут видеть взаимные Telegram DM? **Рекомендация:** Нет, строго раздельные личные истории
- **Q0028 · PROPOSAL / OPEN** — Что делать с ребёнком и гостем? **Рекомендация:** Раздельные identities с ограниченным доступом; конкретный голосовой UX позже
- **Q0029 · PROPOSAL / OPEN** — Как связать смену имени с памятью? **Рекомендация:** Стабильный person_id и отдельно display name
- **Q0030 · PROPOSAL / OPEN** — Достаточна ли speaker recognition для платежей? **Рекомендация:** Нет, высокорисковые действия требуют независимой авторизации
- **Q0031 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Person identity and personal accounts»? **Рекомендация:** Определить успех как: Stable independent personal identities across channels; не включать недоказанные обещания.
- **Q0032 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Person identity and personal accounts»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0033 · PROPOSAL / OPEN** — Как инициируется работа «Person identity and personal accounts»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0034 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Person identity and personal accounts»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0035 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Person identity and personal accounts»? **Рекомендация:** Уважать boundary: no channel address or voice sample alone grants power; не расширять ее эвристикой модели.
- **Q0036 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Person identity and personal accounts»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0037 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Person identity and personal accounts»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0038 · PROPOSAL / OPEN** — Что «Person identity and personal accounts» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0039 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Person identity and personal accounts»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0040 · PROPOSAL / OPEN** — Сколько должно жить состояние «Person identity and personal accounts»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0041 · PROPOSAL / OPEN** — Где именно выполняется «Person identity and personal accounts»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0042 · PROPOSAL / OPEN** — Как «Person identity and personal accounts» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0043 · PROPOSAL / OPEN** — Как отменять или отзывать «Person identity and personal accounts»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0044 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Person identity and personal accounts»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0045 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Person identity and personal accounts»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0046 · PROPOSAL / OPEN** — Как пользователю доказать результат «Person identity and personal accounts»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0047 · PROPOSAL / OPEN** — Кто платит за «Person identity and personal accounts» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0048 · PROPOSAL / OPEN** — Как изолировать безопасность «Person identity and personal accounts»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: shared-account leakage.
- **Q0049 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Person identity and personal accounts»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0050 · PROPOSAL / OPEN** — В какой релиз «Person identity and personal accounts» попадёт? **Рекомендация:** Предлагаемый этап: Later; status не повышать, пока owner и acceptance evidence не готовы.

### 03 · Personal conversations and channel identity

**Контекст:** One private canonical conversation per Person across their authorized channels. **Граница:** Circle shared chat is separate. **Этап:** Baseline. **Основной риск:** message echo and privacy.

- **Q0051 · BASELINE** — Что является канонической Conversation entry? **Рекомендация:** Server-issued identity с происхождением, автором, временем и связанным turn
- **Q0052 · BASELINE** — Должна ли Telegram General отражать DM владельца? **Рекомендация:** В однопользовательском v1 да, без повторного запуска модели; для разных людей нельзя
- **Q0053 · BASELINE** — Могут ли два канала отправить одинаковое сообщение одновременно? **Рекомендация:** Один canonical input при одинаковом ключе, не объединять разные сообщения по тексту
- **Q0054 · BASELINE** — Куда отправлять Worker Result? **Рекомендация:** В Origin Conversation конкретного Person
- **Q0055 · PROPOSAL / OPEN** — Что происходит при удалении канала? **Рекомендация:** Данные Conversation сохраняются согласно политике, канал теряет доступ
- **Q0056 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Personal conversations and channel identity»? **Рекомендация:** Определить успех как: One private canonical conversation per Person across their authorized channels; не включать недоказанные обещания.
- **Q0057 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Personal conversations and channel identity»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0058 · PROPOSAL / OPEN** — Как инициируется работа «Personal conversations and channel identity»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0059 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Personal conversations and channel identity»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0060 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Personal conversations and channel identity»? **Рекомендация:** Уважать boundary: Circle shared chat is separate; не расширять ее эвристикой модели.
- **Q0061 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Personal conversations and channel identity»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0062 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Personal conversations and channel identity»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0063 · PROPOSAL / OPEN** — Что «Personal conversations and channel identity» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0064 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Personal conversations and channel identity»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0065 · PROPOSAL / OPEN** — Сколько должно жить состояние «Personal conversations and channel identity»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0066 · PROPOSAL / OPEN** — Где именно выполняется «Personal conversations and channel identity»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0067 · PROPOSAL / OPEN** — Как «Personal conversations and channel identity» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0068 · PROPOSAL / OPEN** — Как отменять или отзывать «Personal conversations and channel identity»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0069 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Personal conversations and channel identity»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0070 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Personal conversations and channel identity»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0071 · PROPOSAL / OPEN** — Как пользователю доказать результат «Personal conversations and channel identity»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0072 · PROPOSAL / OPEN** — Кто платит за «Personal conversations and channel identity» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0073 · PROPOSAL / OPEN** — Как изолировать безопасность «Personal conversations and channel identity»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: message echo and privacy.
- **Q0074 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Personal conversations and channel identity»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0075 · PROPOSAL / OPEN** — В какой релиз «Personal conversations and channel identity» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 04 · Circle session and decisions

**Контекст:** Durable scoped multi-person collaboration with proposals and agreement. **Граница:** no superior assistant or implicit consent. **Этап:** Later. **Основной риск:** invented consensus.

- **Q0076 · CONFIRMED** — Circle — навсегда общий разум семьи? **Рекомендация:** Нет, scoped collaboration session; можно возобновлять, а recurring membership вынести отдельно
- **Q0077 · PROPOSAL / OPEN** — Кого считать участником Decision? **Рекомендация:** Явно выбранных людей, а не всех членов потенциальной группы
- **Q0078 · CONFIRMED** — Может ли Circle предложить заём? **Рекомендация:** Да, как гипотезу, а не согласие или выполненный перевод
- **Q0079 · CONFIRMED** — Где хранятся предложения и ответы? **Рекомендация:** В canonical shared history Circle, не как Telegram source of truth
- **Q0080 · PROPOSAL / OPEN** — Что делать если стороны не согласны? **Рекомендация:** Статус разногласия и альтернативы; не выдумывать консенсус
- **Q0081 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Circle session and decisions»? **Рекомендация:** Определить успех как: Durable scoped multi-person collaboration with proposals and agreement; не включать недоказанные обещания.
- **Q0082 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Circle session and decisions»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0083 · PROPOSAL / OPEN** — Как инициируется работа «Circle session and decisions»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0084 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Circle session and decisions»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0085 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Circle session and decisions»? **Рекомендация:** Уважать boundary: no superior assistant or implicit consent; не расширять ее эвристикой модели.
- **Q0086 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Circle session and decisions»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0087 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Circle session and decisions»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0088 · PROPOSAL / OPEN** — Что «Circle session and decisions» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0089 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Circle session and decisions»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0090 · PROPOSAL / OPEN** — Сколько должно жить состояние «Circle session and decisions»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0091 · PROPOSAL / OPEN** — Где именно выполняется «Circle session and decisions»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0092 · PROPOSAL / OPEN** — Как «Circle session and decisions» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0093 · PROPOSAL / OPEN** — Как отменять или отзывать «Circle session and decisions»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0094 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Circle session and decisions»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0095 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Circle session and decisions»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0096 · PROPOSAL / OPEN** — Как пользователю доказать результат «Circle session and decisions»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0097 · PROPOSAL / OPEN** — Кто платит за «Circle session and decisions» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0098 · PROPOSAL / OPEN** — Как изолировать безопасность «Circle session and decisions»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: invented consensus.
- **Q0099 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Circle session and decisions»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0100 · PROPOSAL / OPEN** — В какой релиз «Circle session and decisions» попадёт? **Рекомендация:** Предлагаемый этап: Later; status не повышать, пока owner и acceptance evidence не готовы.

### 05 · A2A and cross-server federation

**Контекст:** Authenticated scoped messages among independently deployed Secretaries. **Граница:** no unbounded debate or implicit sharing. **Этап:** Later. **Основной риск:** cycles and impersonation.

- **Q0101 · PROPOSAL / OPEN** — Должен ли A2A быть базой доменной модели? **Рекомендация:** Нет, протокол обмена между персональными Secretaries, а Circle владеет workflow
- **Q0102 · PROPOSAL / OPEN** — Как проверить, что remote Secretary действительно представляет Ora? **Рекомендация:** Credential-bound federation identity и подтверждённое приглашение
- **Q0103 · PROPOSAL / OPEN** — Может ли агент раскрыть необработанные медицинские записи? **Рекомендация:** Нет, только разрешённые минимальные выводы
- **Q0104 · PROPOSAL / OPEN** — Как обработать отказ другого сервера? **Рекомендация:** Durable pending/timeout, без фиктивного ответа человека
- **Q0105 · PROPOSAL / OPEN** — Может ли агент притвориться человеком при голосовании? **Рекомендация:** Нет, каждое решение требует подтверждённой identity/делегированного права
- **Q0106 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «A2A and cross-server federation»? **Рекомендация:** Определить успех как: Authenticated scoped messages among independently deployed Secretaries; не включать недоказанные обещания.
- **Q0107 · PROPOSAL / OPEN** — Кому принадлежит право активировать «A2A and cross-server federation»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0108 · PROPOSAL / OPEN** — Как инициируется работа «A2A and cross-server federation»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0109 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «A2A and cross-server federation»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0110 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «A2A and cross-server federation»? **Рекомендация:** Уважать boundary: no unbounded debate or implicit sharing; не расширять ее эвристикой модели.
- **Q0111 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «A2A and cross-server federation»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0112 · PROPOSAL / OPEN** — Как пользователь управляет настройками «A2A and cross-server federation»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0113 · PROPOSAL / OPEN** — Что «A2A and cross-server federation» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0114 · PROPOSAL / OPEN** — Где хранится каноническое состояние «A2A and cross-server federation»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0115 · PROPOSAL / OPEN** — Сколько должно жить состояние «A2A and cross-server federation»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0116 · PROPOSAL / OPEN** — Где именно выполняется «A2A and cross-server federation»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0117 · PROPOSAL / OPEN** — Как «A2A and cross-server federation» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0118 · PROPOSAL / OPEN** — Как отменять или отзывать «A2A and cross-server federation»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0119 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «A2A and cross-server federation»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0120 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «A2A and cross-server federation»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0121 · PROPOSAL / OPEN** — Как пользователю доказать результат «A2A and cross-server federation»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0122 · PROPOSAL / OPEN** — Кто платит за «A2A and cross-server federation» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0123 · PROPOSAL / OPEN** — Как изолировать безопасность «A2A and cross-server federation»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: cycles and impersonation.
- **Q0124 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «A2A and cross-server federation»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0125 · PROPOSAL / OPEN** — В какой релиз «A2A and cross-server federation» попадёт? **Рекомендация:** Предлагаемый этап: Later; status не повышать, пока owner и acceptance evidence не готовы.

### 06 · Secretary orchestration

**Контекст:** Persistent personal assistant routing direct replies and delegated work. **Граница:** no supervisor hierarchy. **Этап:** Baseline. **Основной риск:** wrong task dispatch.

- **Q0126 · BASELINE** — Кто решает: прямой ответ или Worker? **Рекомендация:** Secretary с прозрачной политикой и ручным override
- **Q0127 · BASELINE** — Нужен ли отдельный manager поверх Secretary? **Рекомендация:** Нет, не вводить избыточную иерархию
- **Q0128 · PROPOSAL / OPEN** — Кто владеет правдой об active Attempt? **Рекомендация:** Secretary Server и durable receipts, не вывод LLM
- **Q0129 · BASELINE** — Как давать Secretary минимальные lifecycle tools? **Рекомендация:** Typed MCP/capability calls с origin и idempotency
- **Q0130 · BASELINE** — Когда Secretary должен задать уточнение? **Рекомендация:** Когда отсутствуют необходимые параметры или возможна неверная адресация
- **Q0131 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Secretary orchestration»? **Рекомендация:** Определить успех как: Persistent personal assistant routing direct replies and delegated work; не включать недоказанные обещания.
- **Q0132 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Secretary orchestration»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0133 · PROPOSAL / OPEN** — Как инициируется работа «Secretary orchestration»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0134 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Secretary orchestration»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0135 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Secretary orchestration»? **Рекомендация:** Уважать boundary: no supervisor hierarchy; не расширять ее эвристикой модели.
- **Q0136 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Secretary orchestration»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0137 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Secretary orchestration»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0138 · PROPOSAL / OPEN** — Что «Secretary orchestration» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0139 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Secretary orchestration»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0140 · PROPOSAL / OPEN** — Сколько должно жить состояние «Secretary orchestration»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0141 · PROPOSAL / OPEN** — Где именно выполняется «Secretary orchestration»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0142 · PROPOSAL / OPEN** — Как «Secretary orchestration» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0143 · PROPOSAL / OPEN** — Как отменять или отзывать «Secretary orchestration»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0144 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Secretary orchestration»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0145 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Secretary orchestration»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0146 · PROPOSAL / OPEN** — Как пользователю доказать результат «Secretary orchestration»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0147 · PROPOSAL / OPEN** — Кто платит за «Secretary orchestration» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0148 · PROPOSAL / OPEN** — Как изолировать безопасность «Secretary orchestration»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: wrong task dispatch.
- **Q0149 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Secretary orchestration»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0150 · PROPOSAL / OPEN** — В какой релиз «Secretary orchestration» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 07 · Tasks Workers Attempts and Results

**Контекст:** Durable task and persistent Worker lifecycle. **Граница:** never repeat unknown side effects blindly. **Этап:** Baseline. **Основной риск:** duplicate execution.

- **Q0151 · BASELINE** — Что такое persistent Worker в отличие от subagent? **Рекомендация:** Привязанная к Task сущность с собственной историей и native session
- **Q0152 · BASELINE** — Может ли Worker задавать вопрос, завершив Attempt? **Рекомендация:** Да, Task остаётся открытым до явного закрытия
- **Q0153 · BASELINE** — Чем Steering отличается от /q? **Рекомендация:** Steering прямо сейчас, /q — FIFO на idle
- **Q0154 · BASELINE** — Что означает terminal canceled? **Рекомендация:** Доказано прекращение native execution и сохранён durable terminal outcome
- **Q0155 · BASELINE** — Можно ли запускать Follow-up на ином Node молча? **Рекомендация:** Нет, session binding должен быть доказан или пользователь решает о миграции
- **Q0156 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Tasks Workers Attempts and Results»? **Рекомендация:** Определить успех как: Durable task and persistent Worker lifecycle; не включать недоказанные обещания.
- **Q0157 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Tasks Workers Attempts and Results»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0158 · PROPOSAL / OPEN** — Как инициируется работа «Tasks Workers Attempts and Results»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0159 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Tasks Workers Attempts and Results»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0160 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Tasks Workers Attempts and Results»? **Рекомендация:** Уважать boundary: never repeat unknown side effects blindly; не расширять ее эвристикой модели.
- **Q0161 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Tasks Workers Attempts and Results»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0162 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Tasks Workers Attempts and Results»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0163 · PROPOSAL / OPEN** — Что «Tasks Workers Attempts and Results» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0164 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Tasks Workers Attempts and Results»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0165 · PROPOSAL / OPEN** — Сколько должно жить состояние «Tasks Workers Attempts and Results»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0166 · PROPOSAL / OPEN** — Где именно выполняется «Tasks Workers Attempts and Results»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0167 · PROPOSAL / OPEN** — Как «Tasks Workers Attempts and Results» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0168 · PROPOSAL / OPEN** — Как отменять или отзывать «Tasks Workers Attempts and Results»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0169 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Tasks Workers Attempts and Results»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0170 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Tasks Workers Attempts and Results»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0171 · PROPOSAL / OPEN** — Как пользователю доказать результат «Tasks Workers Attempts and Results»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0172 · PROPOSAL / OPEN** — Кто платит за «Tasks Workers Attempts and Results» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0173 · PROPOSAL / OPEN** — Как изолировать безопасность «Tasks Workers Attempts and Results»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: duplicate execution.
- **Q0174 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Tasks Workers Attempts and Results»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0175 · PROPOSAL / OPEN** — В какой релиз «Tasks Workers Attempts and Results» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 08 · Codex native harness

**Контекст:** Supported tested Codex runtime and user auth. **Граница:** unsupported explicit model must fail clearly. **Этап:** Baseline. **Основной риск:** native/session mismatch.

- **Q0176 · BASELINE** — Как выбирать Codex default model под ChatGPT auth? **Рекомендация:** По проверенной поддержке native аккаунта; explicit choice не переписывать
- **Q0177 · BASELINE** — Станет ли Codex runtime обязательным для всех установок? **Рекомендация:** Нет, допускается установка только Claude Code
- **Q0178 · BASELINE** — Что делать с ACP readiness до первого Prompt? **Рекомендация:** Реальная initialize/session readiness, а не только процесс/файл
- **Q0179 · BASELINE** — Как показывать native tool calls? **Рекомендация:** В Worker observer с trace/origin
- **Q0180 · BASELINE** — Как подтверждать release? **Рекомендация:** Live Start/Resume/Cancel/Result на опубликованном бинарнике
- **Q0181 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Codex native harness»? **Рекомендация:** Определить успех как: Supported tested Codex runtime and user auth; не включать недоказанные обещания.
- **Q0182 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Codex native harness»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0183 · PROPOSAL / OPEN** — Как инициируется работа «Codex native harness»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0184 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Codex native harness»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0185 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Codex native harness»? **Рекомендация:** Уважать boundary: unsupported explicit model must fail clearly; не расширять ее эвристикой модели.
- **Q0186 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Codex native harness»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0187 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Codex native harness»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0188 · PROPOSAL / OPEN** — Что «Codex native harness» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0189 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Codex native harness»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0190 · PROPOSAL / OPEN** — Сколько должно жить состояние «Codex native harness»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0191 · PROPOSAL / OPEN** — Где именно выполняется «Codex native harness»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0192 · PROPOSAL / OPEN** — Как «Codex native harness» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0193 · PROPOSAL / OPEN** — Как отменять или отзывать «Codex native harness»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0194 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Codex native harness»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0195 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Codex native harness»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0196 · PROPOSAL / OPEN** — Как пользователю доказать результат «Codex native harness»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0197 · PROPOSAL / OPEN** — Кто платит за «Codex native harness» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0198 · PROPOSAL / OPEN** — Как изолировать безопасность «Codex native harness»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: native/session mismatch.
- **Q0199 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Codex native harness»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0200 · PROPOSAL / OPEN** — В какой релиз «Codex native harness» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 09 · Claude Code native harness

**Контекст:** Supported tested Claude Code ACP/native runtime. **Граница:** cancel receipt is not proven terminal. **Этап:** Baseline. **Основной риск:** leaked native processes.

- **Q0201 · BASELINE** — Можно ли выбрать Claude Code для Secretary и Codex для Worker? **Рекомендация:** Да, независимый конфиг
- **Q0202 · BASELINE** — Как разделить native subscription и API token? **Рекомендация:** Разные проверенные auth modes и ограничения
- **Q0203 · BASELINE** — Что делать при зависшем native Prompt и Cancel? **Рекомендация:** Отмена не должна ждать занятой записи без возможности остановить native CLI
- **Q0204 · BASELINE** — Может ли canceled ошибочно стать failed на Resume? **Рекомендация:** Не путать native canceled/interrupted с failure
- **Q0205 · BASELINE** — Как избежать дочерних процессов после отмены? **Рекомендация:** OS-aware process cleanup с честным отчётом о невозможности доказательства
- **Q0206 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Claude Code native harness»? **Рекомендация:** Определить успех как: Supported tested Claude Code ACP/native runtime; не включать недоказанные обещания.
- **Q0207 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Claude Code native harness»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0208 · PROPOSAL / OPEN** — Как инициируется работа «Claude Code native harness»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0209 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Claude Code native harness»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0210 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Claude Code native harness»? **Рекомендация:** Уважать boundary: cancel receipt is not proven terminal; не расширять ее эвристикой модели.
- **Q0211 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Claude Code native harness»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0212 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Claude Code native harness»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0213 · PROPOSAL / OPEN** — Что «Claude Code native harness» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0214 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Claude Code native harness»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0215 · PROPOSAL / OPEN** — Сколько должно жить состояние «Claude Code native harness»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0216 · PROPOSAL / OPEN** — Где именно выполняется «Claude Code native harness»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0217 · PROPOSAL / OPEN** — Как «Claude Code native harness» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0218 · PROPOSAL / OPEN** — Как отменять или отзывать «Claude Code native harness»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0219 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Claude Code native harness»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0220 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Claude Code native harness»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0221 · PROPOSAL / OPEN** — Как пользователю доказать результат «Claude Code native harness»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0222 · PROPOSAL / OPEN** — Кто платит за «Claude Code native harness» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0223 · PROPOSAL / OPEN** — Как изолировать безопасность «Claude Code native harness»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: leaked native processes.
- **Q0224 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Claude Code native harness»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0225 · PROPOSAL / OPEN** — В какой релиз «Claude Code native harness» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 10 · Pi OpenCode FX and harness SDK

**Контекст:** Pluggable experimental and future harness adapters. **Граница:** no untested v1 guarantees. **Этап:** Future. **Основной риск:** capability mismatch.

- **Q0226 · PROPOSAL / OPEN** — Должен ли Pi быть главным harness прямо сейчас? **Рекомендация:** Сначала capability/latency тесты, без обязательства v1
- **Q0227 · PROPOSAL / OPEN** — Что важно в OpenCode? **Рекомендация:** Поддерживаемые providers/models, session lifecycle и extensions
- **Q0228 · PROPOSAL / OPEN** — FX embedded — отдельная продуктовая линия? **Рекомендация:** Пока эксперимент, оценивать стоимость интеграции
- **Q0229 · PROPOSAL / OPEN** — Нужно ли общее SDK адаптеров? **Рекомендация:** Да, минимальный typed contract для prompt/result/tools/cancel
- **Q0230 · PROPOSAL / OPEN** — Подменять ли функции отсутствующего harness? **Рекомендация:** Нет, показывать поддерживаемые capabilities
- **Q0231 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Pi OpenCode FX and harness SDK»? **Рекомендация:** Определить успех как: Pluggable experimental and future harness adapters; не включать недоказанные обещания.
- **Q0232 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Pi OpenCode FX and harness SDK»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0233 · PROPOSAL / OPEN** — Как инициируется работа «Pi OpenCode FX and harness SDK»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0234 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Pi OpenCode FX and harness SDK»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0235 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Pi OpenCode FX and harness SDK»? **Рекомендация:** Уважать boundary: no untested v1 guarantees; не расширять ее эвристикой модели.
- **Q0236 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Pi OpenCode FX and harness SDK»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0237 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Pi OpenCode FX and harness SDK»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0238 · PROPOSAL / OPEN** — Что «Pi OpenCode FX and harness SDK» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0239 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Pi OpenCode FX and harness SDK»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0240 · PROPOSAL / OPEN** — Сколько должно жить состояние «Pi OpenCode FX and harness SDK»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0241 · PROPOSAL / OPEN** — Где именно выполняется «Pi OpenCode FX and harness SDK»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0242 · PROPOSAL / OPEN** — Как «Pi OpenCode FX and harness SDK» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0243 · PROPOSAL / OPEN** — Как отменять или отзывать «Pi OpenCode FX and harness SDK»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0244 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Pi OpenCode FX and harness SDK»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0245 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Pi OpenCode FX and harness SDK»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0246 · PROPOSAL / OPEN** — Как пользователю доказать результат «Pi OpenCode FX and harness SDK»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0247 · PROPOSAL / OPEN** — Кто платит за «Pi OpenCode FX and harness SDK» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0248 · PROPOSAL / OPEN** — Как изолировать безопасность «Pi OpenCode FX and harness SDK»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: capability mismatch.
- **Q0249 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Pi OpenCode FX and harness SDK»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0250 · PROPOSAL / OPEN** — В какой релиз «Pi OpenCode FX and harness SDK» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 11 · Subscriptions APIs keys and local models

**Контекст:** BYO inference choice via verified adapter. **Граница:** no bypass of provider authorization. **Этап:** Confirmed. **Основной риск:** secrets and incompatible terms.

- **Q0251 · PROPOSAL / OPEN** — Можно ли импортировать подписку как API key? **Рекомендация:** Нет, это разные типы авторизации
- **Q0252 · CONFIRMED** — Нужен ли выбор provider на уровне Task? **Рекомендация:** Да, с прозрачной конфигурацией и возможностью override
- **Q0253 · PROPOSAL / OPEN** — Где хранить provider secret? **Рекомендация:** Secret store/Node environment, не обычная история сообщений
- **Q0254 · CONFIRMED** — Можно ли утверждать, что любая подписка разрешает automation? **Рекомендация:** Нет, проверять актуальные условия поставщика
- **Q0255 · PROPOSAL / OPEN** — Как отменить доступ подключённой модели? **Рекомендация:** Revoke/rotate credential с видимым статусом зависимых Workers
- **Q0256 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Subscriptions APIs keys and local models»? **Рекомендация:** Определить успех как: BYO inference choice via verified adapter; не включать недоказанные обещания.
- **Q0257 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Subscriptions APIs keys and local models»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0258 · PROPOSAL / OPEN** — Как инициируется работа «Subscriptions APIs keys and local models»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0259 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Subscriptions APIs keys and local models»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0260 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Subscriptions APIs keys and local models»? **Рекомендация:** Уважать boundary: no bypass of provider authorization; не расширять ее эвристикой модели.
- **Q0261 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Subscriptions APIs keys and local models»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0262 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Subscriptions APIs keys and local models»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0263 · PROPOSAL / OPEN** — Что «Subscriptions APIs keys and local models» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0264 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Subscriptions APIs keys and local models»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0265 · PROPOSAL / OPEN** — Сколько должно жить состояние «Subscriptions APIs keys and local models»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0266 · PROPOSAL / OPEN** — Где именно выполняется «Subscriptions APIs keys and local models»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0267 · PROPOSAL / OPEN** — Как «Subscriptions APIs keys and local models» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0268 · PROPOSAL / OPEN** — Как отменять или отзывать «Subscriptions APIs keys and local models»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0269 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Subscriptions APIs keys and local models»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0270 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Subscriptions APIs keys and local models»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0271 · PROPOSAL / OPEN** — Как пользователю доказать результат «Subscriptions APIs keys and local models»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0272 · PROPOSAL / OPEN** — Кто платит за «Subscriptions APIs keys and local models» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0273 · PROPOSAL / OPEN** — Как изолировать безопасность «Subscriptions APIs keys and local models»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: secrets and incompatible terms.
- **Q0274 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Subscriptions APIs keys and local models»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0275 · PROPOSAL / OPEN** — В какой релиз «Subscriptions APIs keys and local models» попадёт? **Рекомендация:** Предлагаемый этап: Confirmed; status не повышать, пока owner и acceptance evidence не готовы.

### 12 · Model selection fallback and quotas

**Контекст:** Explicit policy for choosing supported models/providers. **Граница:** never silently override explicit user choice. **Этап:** Future. **Основной риск:** loss of task continuity.

- **Q0276 · PROPOSAL / OPEN** — Когда выбирать дешевую модель? **Рекомендация:** Для подходящей сложности и разрешённого provider route
- **Q0277 · PROPOSAL / OPEN** — Когда переключаться на fallback? **Рекомендация:** На явную ошибку/квоту по заданной политике, не вопреки explicit model выбору
- **Q0278 · PROPOSAL / OPEN** — Как сохранять context при смене provider? **Рекомендация:** Durable canonical Task/Conversation, не гарантия native session compatibility
- **Q0279 · PROPOSAL / OPEN** — Кто ограничивает деньги? **Рекомендация:** Budget per user/Worker/provider с предупреждениями
- **Q0280 · PROPOSAL / OPEN** — Должен ли автоматический fallback повторять уже выполненное действие? **Рекомендация:** Нет, сперва проверять durable outcome и idempotency
- **Q0281 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Model selection fallback and quotas»? **Рекомендация:** Определить успех как: Explicit policy for choosing supported models/providers; не включать недоказанные обещания.
- **Q0282 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Model selection fallback and quotas»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0283 · PROPOSAL / OPEN** — Как инициируется работа «Model selection fallback and quotas»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0284 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Model selection fallback and quotas»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0285 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Model selection fallback and quotas»? **Рекомендация:** Уважать boundary: never silently override explicit user choice; не расширять ее эвристикой модели.
- **Q0286 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Model selection fallback and quotas»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0287 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Model selection fallback and quotas»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0288 · PROPOSAL / OPEN** — Что «Model selection fallback and quotas» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0289 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Model selection fallback and quotas»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0290 · PROPOSAL / OPEN** — Сколько должно жить состояние «Model selection fallback and quotas»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0291 · PROPOSAL / OPEN** — Где именно выполняется «Model selection fallback and quotas»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0292 · PROPOSAL / OPEN** — Как «Model selection fallback and quotas» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0293 · PROPOSAL / OPEN** — Как отменять или отзывать «Model selection fallback and quotas»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0294 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Model selection fallback and quotas»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0295 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Model selection fallback and quotas»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0296 · PROPOSAL / OPEN** — Как пользователю доказать результат «Model selection fallback and quotas»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0297 · PROPOSAL / OPEN** — Кто платит за «Model selection fallback and quotas» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0298 · PROPOSAL / OPEN** — Как изолировать безопасность «Model selection fallback and quotas»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: loss of task continuity.
- **Q0299 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Model selection fallback and quotas»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0300 · PROPOSAL / OPEN** — В какой релиз «Model selection fallback and quotas» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 13 · Execution Nodes and local hardware

**Контекст:** Paired outbound Nodes on user's machines. **Граница:** full access is real OS authority. **Этап:** Baseline. **Основной риск:** privileged code execution.

- **Q0301 · BASELINE** — Что именно Node исполняет? **Рекомендация:** Worker harness processes и доверенные tools под своими OS capabilities
- **Q0302 · BASELINE** — Нужно ли owner-approved pairing? **Рекомендация:** Да, каждый Node получает собственную identity
- **Q0303 · BASELINE** — Работают ли Windows/macOS/Linux одинаково? **Рекомендация:** Семантика одинакова, acceptance платформ отдельно
- **Q0304 · BASELINE** — Можно ли открыть Node через internet port? **Рекомендация:** Предпочитать outbound secure tunnel/connection
- **Q0305 · PROPOSAL / OPEN** — Важна ли видимость доступа Node? **Рекомендация:** Да, indicator/tray/status и выключение доступа
- **Q0306 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Execution Nodes and local hardware»? **Рекомендация:** Определить успех как: Paired outbound Nodes on user's machines; не включать недоказанные обещания.
- **Q0307 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Execution Nodes and local hardware»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0308 · PROPOSAL / OPEN** — Как инициируется работа «Execution Nodes and local hardware»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0309 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Execution Nodes and local hardware»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0310 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Execution Nodes and local hardware»? **Рекомендация:** Уважать boundary: full access is real OS authority; не расширять ее эвристикой модели.
- **Q0311 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Execution Nodes and local hardware»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0312 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Execution Nodes and local hardware»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0313 · PROPOSAL / OPEN** — Что «Execution Nodes and local hardware» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0314 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Execution Nodes and local hardware»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0315 · PROPOSAL / OPEN** — Сколько должно жить состояние «Execution Nodes and local hardware»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0316 · PROPOSAL / OPEN** — Где именно выполняется «Execution Nodes and local hardware»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0317 · PROPOSAL / OPEN** — Как «Execution Nodes and local hardware» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0318 · PROPOSAL / OPEN** — Как отменять или отзывать «Execution Nodes and local hardware»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0319 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Execution Nodes and local hardware»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0320 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Execution Nodes and local hardware»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0321 · PROPOSAL / OPEN** — Как пользователю доказать результат «Execution Nodes and local hardware»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0322 · PROPOSAL / OPEN** — Кто платит за «Execution Nodes and local hardware» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0323 · PROPOSAL / OPEN** — Как изолировать безопасность «Execution Nodes and local hardware»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: privileged code execution.
- **Q0324 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Execution Nodes and local hardware»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0325 · PROPOSAL / OPEN** — В какой релиз «Execution Nodes and local hardware» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 14 · Cloud microVM execution

**Контекст:** Isolated hosted Workers with explicit lifetime and capabilities. **Граница:** not all workloads are safely sandboxed. **Этап:** Future. **Основной риск:** escape and lost state.

- **Q0326 · PROPOSAL / OPEN** — Нужен ли sandbox всем self-host Users? **Рекомендация:** Нет, но managed cloud workers должны иметь явную границу изоляции
- **Q0327 · PROPOSAL / OPEN** — Должна ли microVM переживать завершение Task? **Рекомендация:** По профилю persistent/ephemeral, без фиктивного Resume после уничтожения
- **Q0328 · PROPOSAL / OPEN** — Где хранить файлы между microVM сессиями? **Рекомендация:** Exported durable Artifact references и проверяемые volumes
- **Q0329 · PROPOSAL / OPEN** — Можно ли дать чужой VM домашний SSH ключ? **Рекомендация:** Только минимальная временная capability, не неограниченная копия ключа
- **Q0330 · PROPOSAL / OPEN** — Кто оплачивает CPU/GPU время? **Рекомендация:** Конкретный владелец compute с понятными лимитами
- **Q0331 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Cloud microVM execution»? **Рекомендация:** Определить успех как: Isolated hosted Workers with explicit lifetime and capabilities; не включать недоказанные обещания.
- **Q0332 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Cloud microVM execution»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0333 · PROPOSAL / OPEN** — Как инициируется работа «Cloud microVM execution»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0334 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Cloud microVM execution»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0335 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Cloud microVM execution»? **Рекомендация:** Уважать boundary: not all workloads are safely sandboxed; не расширять ее эвристикой модели.
- **Q0336 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Cloud microVM execution»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0337 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Cloud microVM execution»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0338 · PROPOSAL / OPEN** — Что «Cloud microVM execution» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0339 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Cloud microVM execution»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0340 · PROPOSAL / OPEN** — Сколько должно жить состояние «Cloud microVM execution»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0341 · PROPOSAL / OPEN** — Где именно выполняется «Cloud microVM execution»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0342 · PROPOSAL / OPEN** — Как «Cloud microVM execution» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0343 · PROPOSAL / OPEN** — Как отменять или отзывать «Cloud microVM execution»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0344 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Cloud microVM execution»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0345 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Cloud microVM execution»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0346 · PROPOSAL / OPEN** — Как пользователю доказать результат «Cloud microVM execution»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0347 · PROPOSAL / OPEN** — Кто платит за «Cloud microVM execution» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0348 · PROPOSAL / OPEN** — Как изолировать безопасность «Cloud microVM execution»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: escape and lost state.
- **Q0349 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Cloud microVM execution»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0350 · PROPOSAL / OPEN** — В какой релиз «Cloud microVM execution» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 15 · Cloudflare Workers and Durable Objects

**Контекст:** Optional serverless control plane with persistent orchestration. **Граница:** native CLI execution stays on Nodes. **Этап:** Future. **Основной риск:** platform lock-in.

- **Q0351 · PROPOSAL / OPEN** — Может ли Secretary полностью жить в Cloudflare Workers? **Рекомендация:** Control plane да, native long-running Worker CLI выносится на Nodes
- **Q0352 · PROPOSAL / OPEN** — Для чего Durable Objects? **Рекомендация:** Owner-scoped orchestration, live connections, canonical coordination при переносимой схеме
- **Q0353 · PROPOSAL / OPEN** — Где хранить видео и документы? **Рекомендация:** Object store с серверными metadata/Artifact IDs
- **Q0354 · PROPOSAL / OPEN** — Что с WebSocket при hibernation? **Рекомендация:** Реконструкция live state из durable журналов и receipts
- **Q0355 · PROPOSAL / OPEN** — Что мешает переносу с Cloudflare? **Рекомендация:** Provider-specific primitives должны быть за adapters, export/import обязательны как цель
- **Q0356 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Cloudflare Workers and Durable Objects»? **Рекомендация:** Определить успех как: Optional serverless control plane with persistent orchestration; не включать недоказанные обещания.
- **Q0357 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Cloudflare Workers and Durable Objects»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0358 · PROPOSAL / OPEN** — Как инициируется работа «Cloudflare Workers and Durable Objects»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0359 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Cloudflare Workers and Durable Objects»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0360 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Cloudflare Workers and Durable Objects»? **Рекомендация:** Уважать boundary: native CLI execution stays on Nodes; не расширять ее эвристикой модели.
- **Q0361 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Cloudflare Workers and Durable Objects»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0362 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Cloudflare Workers and Durable Objects»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0363 · PROPOSAL / OPEN** — Что «Cloudflare Workers and Durable Objects» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0364 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Cloudflare Workers and Durable Objects»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0365 · PROPOSAL / OPEN** — Сколько должно жить состояние «Cloudflare Workers and Durable Objects»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0366 · PROPOSAL / OPEN** — Где именно выполняется «Cloudflare Workers and Durable Objects»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0367 · PROPOSAL / OPEN** — Как «Cloudflare Workers and Durable Objects» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0368 · PROPOSAL / OPEN** — Как отменять или отзывать «Cloudflare Workers and Durable Objects»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0369 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Cloudflare Workers and Durable Objects»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0370 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Cloudflare Workers and Durable Objects»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0371 · PROPOSAL / OPEN** — Как пользователю доказать результат «Cloudflare Workers and Durable Objects»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0372 · PROPOSAL / OPEN** — Кто платит за «Cloudflare Workers and Durable Objects» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0373 · PROPOSAL / OPEN** — Как изолировать безопасность «Cloudflare Workers and Durable Objects»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: platform lock-in.
- **Q0374 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Cloudflare Workers and Durable Objects»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0375 · PROPOSAL / OPEN** — В какой релиз «Cloudflare Workers and Durable Objects» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 16 · Self-host VPS hybrid topology

**Контекст:** Equivalent personal experience across supported hosting. **Граница:** verified migration not assumed. **Этап:** Confirmed. **Основной риск:** consistency and network downtime.

- **Q0376 · BASELINE** — Можно ли использовать только Mac? **Рекомендация:** Да, Server и Node могут быть рядом, но как отдельные службы
- **Q0377 · BASELINE** — Можно ли разнести Server и Node по миру? **Рекомендация:** Да, аутентифицированное исходящее соединение
- **Q0378 · CONFIRMED** — Что делать при NAT/offline Node? **Рекомендация:** Сохранять очередь/ошибку, не повторять неизвестный native action
- **Q0379 · CONFIRMED** — Потребуется ли отдельный домен? **Рекомендация:** Для локального Developer Edition нет
- **Q0380 · CONFIRMED** — Как запускать HA bridge рядом с VPS Secretary? **Рекомендация:** Через outbound home companion, без раскрытия локальной сети
- **Q0381 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Self-host VPS hybrid topology»? **Рекомендация:** Определить успех как: Equivalent personal experience across supported hosting; не включать недоказанные обещания.
- **Q0382 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Self-host VPS hybrid topology»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0383 · PROPOSAL / OPEN** — Как инициируется работа «Self-host VPS hybrid topology»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0384 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Self-host VPS hybrid topology»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0385 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Self-host VPS hybrid topology»? **Рекомендация:** Уважать boundary: verified migration not assumed; не расширять ее эвристикой модели.
- **Q0386 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Self-host VPS hybrid topology»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0387 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Self-host VPS hybrid topology»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0388 · PROPOSAL / OPEN** — Что «Self-host VPS hybrid topology» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0389 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Self-host VPS hybrid topology»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0390 · PROPOSAL / OPEN** — Сколько должно жить состояние «Self-host VPS hybrid topology»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0391 · PROPOSAL / OPEN** — Где именно выполняется «Self-host VPS hybrid topology»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0392 · PROPOSAL / OPEN** — Как «Self-host VPS hybrid topology» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0393 · PROPOSAL / OPEN** — Как отменять или отзывать «Self-host VPS hybrid topology»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0394 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Self-host VPS hybrid topology»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0395 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Self-host VPS hybrid topology»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0396 · PROPOSAL / OPEN** — Как пользователю доказать результат «Self-host VPS hybrid topology»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0397 · PROPOSAL / OPEN** — Кто платит за «Self-host VPS hybrid topology» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0398 · PROPOSAL / OPEN** — Как изолировать безопасность «Self-host VPS hybrid topology»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: consistency and network downtime.
- **Q0399 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Self-host VPS hybrid topology»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0400 · PROPOSAL / OPEN** — В какой релиз «Self-host VPS hybrid topology» попадёт? **Рекомендация:** Предлагаемый этап: Confirmed; status не повышать, пока owner и acceptance evidence не готовы.

### 17 · Backups portability and state migrations

**Контекст:** Portable durable personal state and artifacts. **Граница:** remote native sessions need separate export. **Этап:** Baseline. **Основной риск:** unrecoverable data.

- **Q0401 · BASELINE** — Что считать portable backup? **Рекомендация:** Central DB/event state, config, memory и Artifact bytes с контрольными суммами
- **Q0402 · BASELINE** — Можно ли потерять native session при переезде? **Рекомендация:** Возможна потеря; сообщить заранее и экспортировать если поддержано
- **Q0403 · PROPOSAL / OPEN** — Как выполнять schema migration без потери истории? **Рекомендация:** Версионированный, тестируемый, обратимый когда возможно процесс
- **Q0404 · PROPOSAL / OPEN** — Как восстановить из частичного snapshot? **Рекомендация:** Не подтверждать success пока данные/bytes не сверены
- **Q0405 · PROPOSAL / OPEN** — Можно ли ручное редактирование Markdown считать основной БД Cloudflare? **Рекомендация:** Нет, Markdown удобный экспорт/редактирование, durability отдельно
- **Q0406 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Backups portability and state migrations»? **Рекомендация:** Определить успех как: Portable durable personal state and artifacts; не включать недоказанные обещания.
- **Q0407 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Backups portability and state migrations»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0408 · PROPOSAL / OPEN** — Как инициируется работа «Backups portability and state migrations»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0409 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Backups portability and state migrations»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0410 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Backups portability and state migrations»? **Рекомендация:** Уважать boundary: remote native sessions need separate export; не расширять ее эвристикой модели.
- **Q0411 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Backups portability and state migrations»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0412 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Backups portability and state migrations»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0413 · PROPOSAL / OPEN** — Что «Backups portability and state migrations» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0414 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Backups portability and state migrations»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0415 · PROPOSAL / OPEN** — Сколько должно жить состояние «Backups portability and state migrations»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0416 · PROPOSAL / OPEN** — Где именно выполняется «Backups portability and state migrations»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0417 · PROPOSAL / OPEN** — Как «Backups portability and state migrations» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0418 · PROPOSAL / OPEN** — Как отменять или отзывать «Backups portability and state migrations»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0419 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Backups portability and state migrations»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0420 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Backups portability and state migrations»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0421 · PROPOSAL / OPEN** — Как пользователю доказать результат «Backups portability and state migrations»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0422 · PROPOSAL / OPEN** — Кто платит за «Backups portability and state migrations» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0423 · PROPOSAL / OPEN** — Как изолировать безопасность «Backups portability and state migrations»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: unrecoverable data.
- **Q0424 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Backups portability and state migrations»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0425 · PROPOSAL / OPEN** — В какой релиз «Backups portability and state migrations» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 18 · Security authorization audit and secrets

**Контекст:** Explicit identities approvals scopes and verifiable outcomes. **Граница:** no LLM text as source of authority. **Этап:** Baseline. **Основной риск:** dangerous side effects.

- **Q0426 · BASELINE** — Можно ли включить Worker без подтверждения доступов Node? **Рекомендация:** Только в рамках ранее осознанно выданных host permissions
- **Q0427 · PROPOSAL / OPEN** — Кто может разблокировать замок дома? **Рекомендация:** Person с соответствующей scoped capability и подтверждением
- **Q0428 · PROPOSAL / OPEN** — Как защищать Tool от prompt injection сайта? **Рекомендация:** Внешний текст не становится системной командой или правом
- **Q0429 · PROPOSAL / OPEN** — Что журналировать в денежных транзакциях? **Рекомендация:** Action ID, actor, consent, receipt, результат; секреты исключить
- **Q0430 · PROPOSAL / OPEN** — Как выглядит revoke при длительном Task? **Рекомендация:** Новые действия блокируются, active effects проверяются индивидуально
- **Q0431 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Security authorization audit and secrets»? **Рекомендация:** Определить успех как: Explicit identities approvals scopes and verifiable outcomes; не включать недоказанные обещания.
- **Q0432 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Security authorization audit and secrets»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0433 · PROPOSAL / OPEN** — Как инициируется работа «Security authorization audit and secrets»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0434 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Security authorization audit and secrets»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0435 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Security authorization audit and secrets»? **Рекомендация:** Уважать boundary: no LLM text as source of authority; не расширять ее эвристикой модели.
- **Q0436 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Security authorization audit and secrets»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0437 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Security authorization audit and secrets»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0438 · PROPOSAL / OPEN** — Что «Security authorization audit and secrets» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0439 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Security authorization audit and secrets»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0440 · PROPOSAL / OPEN** — Сколько должно жить состояние «Security authorization audit and secrets»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0441 · PROPOSAL / OPEN** — Где именно выполняется «Security authorization audit and secrets»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0442 · PROPOSAL / OPEN** — Как «Security authorization audit and secrets» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0443 · PROPOSAL / OPEN** — Как отменять или отзывать «Security authorization audit and secrets»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0444 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Security authorization audit and secrets»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0445 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Security authorization audit and secrets»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0446 · PROPOSAL / OPEN** — Как пользователю доказать результат «Security authorization audit and secrets»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0447 · PROPOSAL / OPEN** — Кто платит за «Security authorization audit and secrets» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0448 · PROPOSAL / OPEN** — Как изолировать безопасность «Security authorization audit and secrets»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: dangerous side effects.
- **Q0449 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Security authorization audit and secrets»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0450 · PROPOSAL / OPEN** — В какой релиз «Security authorization audit and secrets» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 19 · Personal memory preferences and retrieval

**Контекст:** Editable durable memory with provenance. **Граница:** automatic inferred writes outside existing v1. **Этап:** Baseline. **Основной риск:** hallucinated preference.

- **Q0451 · CONFIRMED** — Станет ли shared Circle Memory копией всей личной? **Рекомендация:** Нет, scoped summaries и согласованные общие решения
- **Q0452 · BASELINE** — Как редактировать явную память? **Рекомендация:** Пользовательские Markdown files/управляемый UI и audit изменений
- **Q0453 · BASELINE** — Нужна ли automatic memory extraction уже в v1? **Рекомендация:** Нет, только явные записи; эксперимент после safeguards
- **Q0454 · PROPOSAL / OPEN** — Что делать если preference устарело? **Рекомендация:** Сохранять provenance/даты, спрашивать при важных решениях
- **Q0455 · PROPOSAL / OPEN** — Можно ли переносить память между cloud и local? **Рекомендация:** Да, экспорт в переносимом виде с явной областью доступа
- **Q0456 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Personal memory preferences and retrieval»? **Рекомендация:** Определить успех как: Editable durable memory with provenance; не включать недоказанные обещания.
- **Q0457 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Personal memory preferences and retrieval»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0458 · PROPOSAL / OPEN** — Как инициируется работа «Personal memory preferences and retrieval»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0459 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Personal memory preferences and retrieval»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0460 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Personal memory preferences and retrieval»? **Рекомендация:** Уважать boundary: automatic inferred writes outside existing v1; не расширять ее эвристикой модели.
- **Q0461 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Personal memory preferences and retrieval»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0462 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Personal memory preferences and retrieval»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0463 · PROPOSAL / OPEN** — Что «Personal memory preferences and retrieval» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0464 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Personal memory preferences and retrieval»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0465 · PROPOSAL / OPEN** — Сколько должно жить состояние «Personal memory preferences and retrieval»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0466 · PROPOSAL / OPEN** — Где именно выполняется «Personal memory preferences and retrieval»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0467 · PROPOSAL / OPEN** — Как «Personal memory preferences and retrieval» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0468 · PROPOSAL / OPEN** — Как отменять или отзывать «Personal memory preferences and retrieval»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0469 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Personal memory preferences and retrieval»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0470 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Personal memory preferences and retrieval»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0471 · PROPOSAL / OPEN** — Как пользователю доказать результат «Personal memory preferences and retrieval»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0472 · PROPOSAL / OPEN** — Кто платит за «Personal memory preferences and retrieval» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0473 · PROPOSAL / OPEN** — Как изолировать безопасность «Personal memory preferences and retrieval»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: hallucinated preference.
- **Q0474 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Personal memory preferences and retrieval»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0475 · PROPOSAL / OPEN** — В какой релиз «Personal memory preferences and retrieval» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 20 · MCP tools skills and extensibility

**Контекст:** Common tool surface with native harness skills. **Граница:** reject unavailable capability explicitly. **Этап:** Baseline. **Основной риск:** tool overreach.

- **Q0476 · BASELINE** — Кто владеет tools? **Рекомендация:** Система с проверенными scopes, Secretary вызывает, Worker исполняет в native harness
- **Q0477 · BASELINE** — MCP — единственный поддерживаемый протокол? **Рекомендация:** Общий фасад MCP, внутренний adapter может использовать официальный SDK
- **Q0478 · BASELINE** — Где будут жить Skills? **Рекомендация:** Native harness Skills в v1; unified registry позже
- **Q0479 · PROPOSAL / OPEN** — Как выбрать tools для конкретной Node? **Рекомендация:** Capability discovery и списки разрешений per instance
- **Q0480 · PROPOSAL / OPEN** — Может ли плагин запускать сторонний бинарник? **Рекомендация:** Только в предоставленном execution environment, не в облачном control plane
- **Q0481 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «MCP tools skills and extensibility»? **Рекомендация:** Определить успех как: Common tool surface with native harness skills; не включать недоказанные обещания.
- **Q0482 · PROPOSAL / OPEN** — Кому принадлежит право активировать «MCP tools skills and extensibility»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0483 · PROPOSAL / OPEN** — Как инициируется работа «MCP tools skills and extensibility»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0484 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «MCP tools skills and extensibility»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0485 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «MCP tools skills and extensibility»? **Рекомендация:** Уважать boundary: reject unavailable capability explicitly; не расширять ее эвристикой модели.
- **Q0486 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «MCP tools skills and extensibility»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0487 · PROPOSAL / OPEN** — Как пользователь управляет настройками «MCP tools skills and extensibility»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0488 · PROPOSAL / OPEN** — Что «MCP tools skills and extensibility» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0489 · PROPOSAL / OPEN** — Где хранится каноническое состояние «MCP tools skills and extensibility»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0490 · PROPOSAL / OPEN** — Сколько должно жить состояние «MCP tools skills and extensibility»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0491 · PROPOSAL / OPEN** — Где именно выполняется «MCP tools skills and extensibility»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0492 · PROPOSAL / OPEN** — Как «MCP tools skills and extensibility» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0493 · PROPOSAL / OPEN** — Как отменять или отзывать «MCP tools skills and extensibility»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0494 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «MCP tools skills and extensibility»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0495 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «MCP tools skills and extensibility»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0496 · PROPOSAL / OPEN** — Как пользователю доказать результат «MCP tools skills and extensibility»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0497 · PROPOSAL / OPEN** — Кто платит за «MCP tools skills and extensibility» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0498 · PROPOSAL / OPEN** — Как изолировать безопасность «MCP tools skills and extensibility»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: tool overreach.
- **Q0499 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «MCP tools skills and extensibility»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0500 · PROPOSAL / OPEN** — В какой релиз «MCP tools skills and extensibility» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 21 · Artifacts files audio video and documents

**Контекст:** Canonical binary inputs outputs and references. **Граница:** no silent truncation or fabricated processing. **Этап:** Baseline. **Основной риск:** data corruption.

- **Q0501 · BASELINE** — Нужно ли сохранять оригинальный видеофайл? **Рекомендация:** Да, вместе с sampled frames и метаданными обработки
- **Q0502 · BASELINE** — 1 FPS жёстко обязателен? **Рекомендация:** Нет, конфигурируемый default
- **Q0503 · BASELINE** — Где связывать аудио и расшифровку? **Рекомендация:** Как разные связанные Artifacts со стабильными IDs
- **Q0504 · BASELINE** — Может ли Worker вернуть готовый PDF/архив? **Рекомендация:** Да, explicit publish Artifact, не scan общего диска
- **Q0505 · BASELINE** — Что делать с oversized upload? **Рекомендация:** Явный отказ без обрезания файла
- **Q0506 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Artifacts files audio video and documents»? **Рекомендация:** Определить успех как: Canonical binary inputs outputs and references; не включать недоказанные обещания.
- **Q0507 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Artifacts files audio video and documents»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0508 · PROPOSAL / OPEN** — Как инициируется работа «Artifacts files audio video and documents»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0509 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Artifacts files audio video and documents»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0510 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Artifacts files audio video and documents»? **Рекомендация:** Уважать boundary: no silent truncation or fabricated processing; не расширять ее эвристикой модели.
- **Q0511 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Artifacts files audio video and documents»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0512 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Artifacts files audio video and documents»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0513 · PROPOSAL / OPEN** — Что «Artifacts files audio video and documents» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0514 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Artifacts files audio video and documents»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0515 · PROPOSAL / OPEN** — Сколько должно жить состояние «Artifacts files audio video and documents»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0516 · PROPOSAL / OPEN** — Где именно выполняется «Artifacts files audio video and documents»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0517 · PROPOSAL / OPEN** — Как «Artifacts files audio video and documents» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0518 · PROPOSAL / OPEN** — Как отменять или отзывать «Artifacts files audio video and documents»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0519 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Artifacts files audio video and documents»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0520 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Artifacts files audio video and documents»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0521 · PROPOSAL / OPEN** — Как пользователю доказать результат «Artifacts files audio video and documents»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0522 · PROPOSAL / OPEN** — Кто платит за «Artifacts files audio video and documents» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0523 · PROPOSAL / OPEN** — Как изолировать безопасность «Artifacts files audio video and documents»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: data corruption.
- **Q0524 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Artifacts files audio video and documents»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0525 · PROPOSAL / OPEN** — В какой релиз «Artifacts files audio video and documents» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 22 · Telegram direct messages groups and topics

**Контекст:** Private DM with optional General and Worker Topics. **Граница:** mirrors cannot trigger duplicate turns. **Этап:** Baseline. **Основной риск:** public chat leakage.

- **Q0526 · BASELINE** — Можно ли работать только через DM bot? **Рекомендация:** Да, группа не обязательна
- **Q0527 · BASELINE** — Как показывать Worker's Topic? **Рекомендация:** Связать topic с одной canonical Worker identity
- **Q0528 · BASELINE** — Как избежать DM↔General echo? **Рекомендация:** Stable server-owned message IDs и cursor replay protection
- **Q0529 · BASELINE** — Участник чужой Telegram группы получает owner rights? **Рекомендация:** Нет, membership не равно pairing
- **Q0530 · CONFIRMED** — Будет ли Circle shared group видеть мои личные prompts? **Рекомендация:** Нет, только intentional shared Circle entries
- **Q0531 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Telegram direct messages groups and topics»? **Рекомендация:** Определить успех как: Private DM with optional General and Worker Topics; не включать недоказанные обещания.
- **Q0532 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Telegram direct messages groups and topics»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0533 · PROPOSAL / OPEN** — Как инициируется работа «Telegram direct messages groups and topics»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0534 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Telegram direct messages groups and topics»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0535 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Telegram direct messages groups and topics»? **Рекомендация:** Уважать boundary: mirrors cannot trigger duplicate turns; не расширять ее эвристикой модели.
- **Q0536 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Telegram direct messages groups and topics»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0537 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Telegram direct messages groups and topics»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0538 · PROPOSAL / OPEN** — Что «Telegram direct messages groups and topics» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0539 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Telegram direct messages groups and topics»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0540 · PROPOSAL / OPEN** — Сколько должно жить состояние «Telegram direct messages groups and topics»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0541 · PROPOSAL / OPEN** — Где именно выполняется «Telegram direct messages groups and topics»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0542 · PROPOSAL / OPEN** — Как «Telegram direct messages groups and topics» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0543 · PROPOSAL / OPEN** — Как отменять или отзывать «Telegram direct messages groups and topics»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0544 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Telegram direct messages groups and topics»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0545 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Telegram direct messages groups and topics»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0546 · PROPOSAL / OPEN** — Как пользователю доказать результат «Telegram direct messages groups and topics»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0547 · PROPOSAL / OPEN** — Кто платит за «Telegram direct messages groups and topics» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0548 · PROPOSAL / OPEN** — Как изолировать безопасность «Telegram direct messages groups and topics»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: public chat leakage.
- **Q0549 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Telegram direct messages groups and topics»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0550 · PROPOSAL / OPEN** — В какой релиз «Telegram direct messages groups and topics» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 23 · Web PWA and UI

**Контекст:** Common conversation and Worker observer across browser devices. **Граница:** no parallel backend required. **Этап:** Baseline. **Основной риск:** divergent state.

- **Q0551 · BASELINE** — Может ли PWA показывать один и тот же чат на Mac и телефоне? **Рекомендация:** Да, canonical projection и авторизованная session
- **Q0552 · BASELINE** — Нужно ли автономное offline execution из PWA? **Рекомендация:** Не в v1; честные offline/read-only состояния
- **Q0553 · BASELINE** — Как видеть занятых Workers? **Рекомендация:** Observer по worker identity с live events
- **Q0554 · BASELINE** — Как давать настройки пользователя? **Рекомендация:** Минимальные owner controls и явный config route
- **Q0555 · BASELINE** — Нужен ли второй frontend backend? **Рекомендация:** Нет, использовать тот же Server API
- **Q0556 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Web PWA and UI»? **Рекомендация:** Определить успех как: Common conversation and Worker observer across browser devices; не включать недоказанные обещания.
- **Q0557 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Web PWA and UI»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0558 · PROPOSAL / OPEN** — Как инициируется работа «Web PWA and UI»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0559 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Web PWA and UI»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0560 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Web PWA and UI»? **Рекомендация:** Уважать boundary: no parallel backend required; не расширять ее эвристикой модели.
- **Q0561 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Web PWA and UI»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0562 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Web PWA and UI»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0563 · PROPOSAL / OPEN** — Что «Web PWA and UI» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0564 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Web PWA and UI»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0565 · PROPOSAL / OPEN** — Сколько должно жить состояние «Web PWA and UI»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0566 · PROPOSAL / OPEN** — Где именно выполняется «Web PWA and UI»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0567 · PROPOSAL / OPEN** — Как «Web PWA and UI» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0568 · PROPOSAL / OPEN** — Как отменять или отзывать «Web PWA and UI»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0569 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Web PWA and UI»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0570 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Web PWA and UI»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0571 · PROPOSAL / OPEN** — Как пользователю доказать результат «Web PWA and UI»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0572 · PROPOSAL / OPEN** — Кто платит за «Web PWA and UI» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0573 · PROPOSAL / OPEN** — Как изолировать безопасность «Web PWA and UI»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: divergent state.
- **Q0574 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Web PWA and UI»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0575 · PROPOSAL / OPEN** — В какой релиз «Web PWA and UI» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 24 · Native desktop mobile and tray

**Контекст:** Presence and remote access in phone desktop applets. **Граница:** not mandatory for first v1. **Этап:** Future. **Основной риск:** invisible background privileges.

- **Q0576 · PROPOSAL / OPEN** — Зачем menu-bar/tray? **Рекомендация:** Видно, когда Node имеет доступ к машине, можно его отключить
- **Q0577 · PROPOSAL / OPEN** — Должно ли приложение запускаться при login? **Рекомендация:** Явный выбор пользователя при установке
- **Q0578 · PROPOSAL / OPEN** — Могут ли iOS/Android стать Execution Nodes? **Рекомендация:** Исследовать с ограничениями OS background execution и supported tools
- **Q0579 · PROPOSAL / OPEN** — Что делать без native приложения? **Рекомендация:** Web/PWA, Telegram и local CLI продолжают работать
- **Q0580 · PROPOSAL / OPEN** — Нужен ли Ghostty native takeover? **Рекомендация:** Future optional, не dual-control обязательство v1
- **Q0581 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Native desktop mobile and tray»? **Рекомендация:** Определить успех как: Presence and remote access in phone desktop applets; не включать недоказанные обещания.
- **Q0582 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Native desktop mobile and tray»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0583 · PROPOSAL / OPEN** — Как инициируется работа «Native desktop mobile and tray»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0584 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Native desktop mobile and tray»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0585 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Native desktop mobile and tray»? **Рекомендация:** Уважать boundary: not mandatory for first v1; не расширять ее эвристикой модели.
- **Q0586 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Native desktop mobile and tray»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0587 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Native desktop mobile and tray»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0588 · PROPOSAL / OPEN** — Что «Native desktop mobile and tray» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0589 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Native desktop mobile and tray»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0590 · PROPOSAL / OPEN** — Сколько должно жить состояние «Native desktop mobile and tray»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0591 · PROPOSAL / OPEN** — Где именно выполняется «Native desktop mobile and tray»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0592 · PROPOSAL / OPEN** — Как «Native desktop mobile and tray» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0593 · PROPOSAL / OPEN** — Как отменять или отзывать «Native desktop mobile and tray»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0594 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Native desktop mobile and tray»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0595 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Native desktop mobile and tray»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0596 · PROPOSAL / OPEN** — Как пользователю доказать результат «Native desktop mobile and tray»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0597 · PROPOSAL / OPEN** — Кто платит за «Native desktop mobile and tray» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0598 · PROPOSAL / OPEN** — Как изолировать безопасность «Native desktop mobile and tray»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: invisible background privileges.
- **Q0599 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Native desktop mobile and tray»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0600 · PROPOSAL / OPEN** — В какой релиз «Native desktop mobile and tray» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 25 · Voice live conversation and wake word

**Контекст:** Natural hands-free assistant with voice continuity. **Граница:** avoid claiming instant latency everywhere. **Этап:** Future. **Основной риск:** misrecognition.

- **Q0601 · PROPOSAL / OPEN** — Как инициировать голосовой turn? **Рекомендация:** Wake-word/PTT по настройке; распознавание отдельно от прав
- **Q0602 · PROPOSAL / OPEN** — Должен ли Assistant знать комнату микрофона? **Рекомендация:** Использовать paired satellite/room context, не угадывать
- **Q0603 · PROPOSAL / OPEN** — Можно ли получить низкую latency повсюду? **Рекомендация:** Цель streaming pipeline, реальные цифры через измерения
- **Q0604 · PROPOSAL / OPEN** — Как обрабатывать перебивание (barge-in)? **Рекомендация:** Cancel/suspend текущий TTS/turn с корректной state boundary
- **Q0605 · PROPOSAL / OPEN** — Что делать при распознавании нескольких людей? **Рекомендация:** Уточнить identity/адресата; sensitive actions не полагать на биометрию
- **Q0606 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Voice live conversation and wake word»? **Рекомендация:** Определить успех как: Natural hands-free assistant with voice continuity; не включать недоказанные обещания.
- **Q0607 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Voice live conversation and wake word»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0608 · PROPOSAL / OPEN** — Как инициируется работа «Voice live conversation and wake word»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0609 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Voice live conversation and wake word»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0610 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Voice live conversation and wake word»? **Рекомендация:** Уважать boundary: avoid claiming instant latency everywhere; не расширять ее эвристикой модели.
- **Q0611 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Voice live conversation and wake word»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0612 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Voice live conversation and wake word»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0613 · PROPOSAL / OPEN** — Что «Voice live conversation and wake word» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0614 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Voice live conversation and wake word»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0615 · PROPOSAL / OPEN** — Сколько должно жить состояние «Voice live conversation and wake word»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0616 · PROPOSAL / OPEN** — Где именно выполняется «Voice live conversation and wake word»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0617 · PROPOSAL / OPEN** — Как «Voice live conversation and wake word» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0618 · PROPOSAL / OPEN** — Как отменять или отзывать «Voice live conversation and wake word»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0619 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Voice live conversation and wake word»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0620 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Voice live conversation and wake word»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0621 · PROPOSAL / OPEN** — Как пользователю доказать результат «Voice live conversation and wake word»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0622 · PROPOSAL / OPEN** — Кто платит за «Voice live conversation and wake word» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0623 · PROPOSAL / OPEN** — Как изолировать безопасность «Voice live conversation and wake word»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: misrecognition.
- **Q0624 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Voice live conversation and wake word»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0625 · PROPOSAL / OPEN** — В какой релиз «Voice live conversation and wake word» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 26 · Proactive background assistant

**Контекст:** Optional scheduled suggestions and follow-ups. **Граница:** require user-defined opt-in and budget. **Этап:** Future. **Основной риск:** intrusive automation.

- **Q0626 · PROPOSAL / OPEN** — Нужен ли Secretary heartbeat с расписанием? **Рекомендация:** Только после opt-in и бюджета, не обязательный v1
- **Q0627 · PROPOSAL / OPEN** — Может ли Secretary предлагать купить билеты заранее? **Рекомендация:** Да как proposal, платёж требует отдельного согласия
- **Q0628 · PROPOSAL / OPEN** — Как не будить пользователя ночью? **Рекомендация:** Quiet hours/per-channel preferences
- **Q0629 · PROPOSAL / OPEN** — Что при недоступности Home Assistant? **Рекомендация:** Не симулировать сделанное действие, сообщить о проблеме
- **Q0630 · PROPOSAL / OPEN** — Может ли Task ждать будущего условия? **Рекомендация:** Да через durable triggers, но не бесконтрольные фоновые модельные циклы
- **Q0631 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Proactive background assistant»? **Рекомендация:** Определить успех как: Optional scheduled suggestions and follow-ups; не включать недоказанные обещания.
- **Q0632 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Proactive background assistant»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0633 · PROPOSAL / OPEN** — Как инициируется работа «Proactive background assistant»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0634 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Proactive background assistant»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0635 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Proactive background assistant»? **Рекомендация:** Уважать boundary: require user-defined opt-in and budget; не расширять ее эвристикой модели.
- **Q0636 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Proactive background assistant»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0637 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Proactive background assistant»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0638 · PROPOSAL / OPEN** — Что «Proactive background assistant» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0639 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Proactive background assistant»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0640 · PROPOSAL / OPEN** — Сколько должно жить состояние «Proactive background assistant»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0641 · PROPOSAL / OPEN** — Где именно выполняется «Proactive background assistant»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0642 · PROPOSAL / OPEN** — Как «Proactive background assistant» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0643 · PROPOSAL / OPEN** — Как отменять или отзывать «Proactive background assistant»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0644 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Proactive background assistant»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0645 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Proactive background assistant»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0646 · PROPOSAL / OPEN** — Как пользователю доказать результат «Proactive background assistant»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0647 · PROPOSAL / OPEN** — Кто платит за «Proactive background assistant» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0648 · PROPOSAL / OPEN** — Как изолировать безопасность «Proactive background assistant»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: intrusive automation.
- **Q0649 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Proactive background assistant»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0650 · PROPOSAL / OPEN** — В какой релиз «Proactive background assistant» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 27 · Browser computer use and cross-app actions

**Контекст:** Use user's tools and environment to operate software. **Граница:** constrain arbitrary action and clarify completion. **Этап:** Future. **Основной риск:** web prompt injection.

- **Q0651 · PROPOSAL / OPEN** — Можно ли поручить Secretary работу в браузере? **Рекомендация:** Да на Execution Node с дозволенным browser/desktop tool
- **Q0652 · PROPOSAL / OPEN** — Нужен ли постоянный screen capture? **Рекомендация:** Нет, запрашивать по задаче с понятным присутствием/доступом
- **Q0653 · PROPOSAL / OPEN** — Можно ли доверять инструкциям с веб-страницы? **Рекомендация:** Нет, web content — untrusted data
- **Q0654 · PROPOSAL / OPEN** — Как подтверждать онлайн-заказ? **Рекомендация:** Различать preview cart и final purchase
- **Q0655 · PROPOSAL / OPEN** — Какие ОС поддерживают computer use? **Рекомендация:** Явная capability матрица, live acceptance для каждого
- **Q0656 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Browser computer use and cross-app actions»? **Рекомендация:** Определить успех как: Use user's tools and environment to operate software; не включать недоказанные обещания.
- **Q0657 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Browser computer use and cross-app actions»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0658 · PROPOSAL / OPEN** — Как инициируется работа «Browser computer use and cross-app actions»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0659 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Browser computer use and cross-app actions»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0660 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Browser computer use and cross-app actions»? **Рекомендация:** Уважать boundary: constrain arbitrary action and clarify completion; не расширять ее эвристикой модели.
- **Q0661 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Browser computer use and cross-app actions»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0662 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Browser computer use and cross-app actions»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0663 · PROPOSAL / OPEN** — Что «Browser computer use and cross-app actions» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0664 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Browser computer use and cross-app actions»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0665 · PROPOSAL / OPEN** — Сколько должно жить состояние «Browser computer use and cross-app actions»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0666 · PROPOSAL / OPEN** — Где именно выполняется «Browser computer use and cross-app actions»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0667 · PROPOSAL / OPEN** — Как «Browser computer use and cross-app actions» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0668 · PROPOSAL / OPEN** — Как отменять или отзывать «Browser computer use and cross-app actions»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0669 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Browser computer use and cross-app actions»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0670 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Browser computer use and cross-app actions»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0671 · PROPOSAL / OPEN** — Как пользователю доказать результат «Browser computer use and cross-app actions»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0672 · PROPOSAL / OPEN** — Кто платит за «Browser computer use and cross-app actions» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0673 · PROPOSAL / OPEN** — Как изолировать безопасность «Browser computer use and cross-app actions»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: web prompt injection.
- **Q0674 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Browser computer use and cross-app actions»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0675 · PROPOSAL / OPEN** — В какой релиз «Browser computer use and cross-app actions» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 28 · Home Assistant architecture and API

**Контекст:** Native interoperable home control via Home Assistant. **Граница:** preserve local automation without cloud. **Этап:** Future. **Основной риск:** cloud dependence.

- **Q0676 · PROPOSAL / OPEN** — Нужно ли Secretary напрямую управлять Zigbee? **Рекомендация:** Нет, первый адаптер через Home Assistant API
- **Q0677 · PROPOSAL / OPEN** — Где работает Home Bridge? **Рекомендация:** Локально рядом с Home Assistant, outbound secure connection к Secretary
- **Q0678 · PROPOSAL / OPEN** — Что при отключении интернета? **Рекомендация:** Локальные HA automations продолжают работать, облачный ассистент деградирует
- **Q0679 · PROPOSAL / OPEN** — Как обнаружить доступные устройства? **Рекомендация:** HA entity registry/services, capability allowlist и room mapping
- **Q0680 · PROPOSAL / OPEN** — Можно ли назвать выключенную лампочку выключенной до подтверждения? **Рекомендация:** Нет, дождаться service/result и state verification
- **Q0681 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Home Assistant architecture and API»? **Рекомендация:** Определить успех как: Native interoperable home control via Home Assistant; не включать недоказанные обещания.
- **Q0682 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Home Assistant architecture and API»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0683 · PROPOSAL / OPEN** — Как инициируется работа «Home Assistant architecture and API»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0684 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Home Assistant architecture and API»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0685 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Home Assistant architecture and API»? **Рекомендация:** Уважать boundary: preserve local automation without cloud; не расширять ее эвристикой модели.
- **Q0686 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Home Assistant architecture and API»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0687 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Home Assistant architecture and API»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0688 · PROPOSAL / OPEN** — Что «Home Assistant architecture and API» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0689 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Home Assistant architecture and API»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0690 · PROPOSAL / OPEN** — Сколько должно жить состояние «Home Assistant architecture and API»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0691 · PROPOSAL / OPEN** — Где именно выполняется «Home Assistant architecture and API»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0692 · PROPOSAL / OPEN** — Как «Home Assistant architecture and API» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0693 · PROPOSAL / OPEN** — Как отменять или отзывать «Home Assistant architecture and API»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0694 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Home Assistant architecture and API»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0695 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Home Assistant architecture and API»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0696 · PROPOSAL / OPEN** — Как пользователю доказать результат «Home Assistant architecture and API»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0697 · PROPOSAL / OPEN** — Кто платит за «Home Assistant architecture and API» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0698 · PROPOSAL / OPEN** — Как изолировать безопасность «Home Assistant architecture and API»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: cloud dependence.
- **Q0699 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Home Assistant architecture and API»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0700 · PROPOSAL / OPEN** — В какой релиз «Home Assistant architecture and API» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 29 · Zigbee Thread Matter MQTT and devices

**Контекст:** Home device network via existing adapters and hubs. **Граница:** Secretary is not a new radio protocol. **Этап:** Future. **Основной риск:** fragmented devices.

- **Q0701 · PROPOSAL / OPEN** — Является ли Matter тем же, что Thread? **Рекомендация:** Нет, Matter application interoperability; Thread сетевой transport
- **Q0702 · PROPOSAL / OPEN** — Заменяет ли Zigbee роутер? **Рекомендация:** Нет, coordinator/bridge нужен в зависимости от сети
- **Q0703 · PROPOSAL / OPEN** — Нужно ли Secretary реализовать Zigbee2MQTT? **Рекомендация:** Нет, использовать HA/ZHA/Zigbee2MQTT, где это уместно
- **Q0704 · PROPOSAL / OPEN** — Что делать с несовместимым устройством? **Рекомендация:** Показать capability absence и setup guide
- **Q0705 · PROPOSAL / OPEN** — Нужно ли покупать конкретный hub? **Рекомендация:** Нет универсального требования, список протестированных альтернатив позже
- **Q0706 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Определить успех как: Home device network via existing adapters and hubs; не включать недоказанные обещания.
- **Q0707 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0708 · PROPOSAL / OPEN** — Как инициируется работа «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0709 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0710 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Уважать boundary: Secretary is not a new radio protocol; не расширять ее эвристикой модели.
- **Q0711 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0712 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0713 · PROPOSAL / OPEN** — Что «Zigbee Thread Matter MQTT and devices» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0714 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0715 · PROPOSAL / OPEN** — Сколько должно жить состояние «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0716 · PROPOSAL / OPEN** — Где именно выполняется «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0717 · PROPOSAL / OPEN** — Как «Zigbee Thread Matter MQTT and devices» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0718 · PROPOSAL / OPEN** — Как отменять или отзывать «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0719 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0720 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0721 · PROPOSAL / OPEN** — Как пользователю доказать результат «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0722 · PROPOSAL / OPEN** — Кто платит за «Zigbee Thread Matter MQTT and devices» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0723 · PROPOSAL / OPEN** — Как изолировать безопасность «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: fragmented devices.
- **Q0724 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Zigbee Thread Matter MQTT and devices»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0725 · PROPOSAL / OPEN** — В какой релиз «Zigbee Thread Matter MQTT and devices» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 30 · Smart home scenes rooms and guests

**Контекст:** Contextual room-aware actions and home routines. **Граница:** safety-class actions demand scoped consent. **Этап:** Future. **Основной риск:** guest overreach.

- **Q0726 · PROPOSAL / OPEN** — Как различать две лампы с именем «Свет»? **Рекомендация:** Room/entity IDs плюс запрос уточнения
- **Q0727 · PROPOSAL / OPEN** — Можно ли объединять свет/музыку/климат в сцену? **Рекомендация:** Да, HA scene/script как атомарная доменная команда где доступно
- **Q0728 · PROPOSAL / OPEN** — Можно ли гостю выключить весь дом? **Рекомендация:** Только по настройке guest capabilities
- **Q0729 · PROPOSAL / OPEN** — Чем отличаются локальная automation и AI recommendation? **Рекомендация:** HA исполняет deterministic rules, Secretary формирует намерения/сценарии
- **Q0730 · PROPOSAL / OPEN** — Что делать если термостат занят физическим переключателем? **Рекомендация:** Читать актуальное состояние и не навязывать конфликт
- **Q0731 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Smart home scenes rooms and guests»? **Рекомендация:** Определить успех как: Contextual room-aware actions and home routines; не включать недоказанные обещания.
- **Q0732 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Smart home scenes rooms and guests»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0733 · PROPOSAL / OPEN** — Как инициируется работа «Smart home scenes rooms and guests»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0734 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Smart home scenes rooms and guests»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0735 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Smart home scenes rooms and guests»? **Рекомендация:** Уважать boundary: safety-class actions demand scoped consent; не расширять ее эвристикой модели.
- **Q0736 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Smart home scenes rooms and guests»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0737 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Smart home scenes rooms and guests»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0738 · PROPOSAL / OPEN** — Что «Smart home scenes rooms and guests» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0739 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Smart home scenes rooms and guests»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0740 · PROPOSAL / OPEN** — Сколько должно жить состояние «Smart home scenes rooms and guests»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0741 · PROPOSAL / OPEN** — Где именно выполняется «Smart home scenes rooms and guests»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0742 · PROPOSAL / OPEN** — Как «Smart home scenes rooms and guests» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0743 · PROPOSAL / OPEN** — Как отменять или отзывать «Smart home scenes rooms and guests»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0744 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Smart home scenes rooms and guests»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0745 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Smart home scenes rooms and guests»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0746 · PROPOSAL / OPEN** — Как пользователю доказать результат «Smart home scenes rooms and guests»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0747 · PROPOSAL / OPEN** — Кто платит за «Smart home scenes rooms and guests» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0748 · PROPOSAL / OPEN** — Как изолировать безопасность «Smart home scenes rooms and guests»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: guest overreach.
- **Q0749 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Smart home scenes rooms and guests»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0750 · PROPOSAL / OPEN** — В какой релиз «Smart home scenes rooms and guests» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 31 · Automobile core concepts and safety

**Контекст:** Capability-based unified interface to supported cars. **Граница:** cannot promise drive-any-car universal actuator. **Этап:** Future. **Основной риск:** physical safety.

- **Q0751 · PROPOSAL / OPEN** — Означает ли «Grok отвези меня» управление рулём? **Рекомендация:** Нет; навигация и физическое вождение — разные классы действий
- **Q0752 · PROPOSAL / OPEN** — Как определить функции конкретного автомобиля? **Рекомендация:** Discover supported commands per VIN/provider/model, не hardcode для всех машин
- **Q0753 · PROPOSAL / OPEN** — Что если авто движется? **Рекомендация:** Парковочные и опасные функции блокировать согласно OEM state/политике
- **Q0754 · PROPOSAL / OPEN** — Может ли OBD-II открыть окно на любой машине? **Рекомендация:** Нет, стандартная диагностика не даёт универсального легального actuator API
- **Q0755 · PROPOSAL / OPEN** — Может ли сторонний AI управлять тормозами/рулём? **Рекомендация:** Не включать без безопасного OEM-certified пути; по умолчанию исключить
- **Q0756 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Automobile core concepts and safety»? **Рекомендация:** Определить успех как: Capability-based unified interface to supported cars; не включать недоказанные обещания.
- **Q0757 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Automobile core concepts and safety»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0758 · PROPOSAL / OPEN** — Как инициируется работа «Automobile core concepts and safety»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0759 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Automobile core concepts and safety»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0760 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Automobile core concepts and safety»? **Рекомендация:** Уважать boundary: cannot promise drive-any-car universal actuator; не расширять ее эвристикой модели.
- **Q0761 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Automobile core concepts and safety»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0762 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Automobile core concepts and safety»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0763 · PROPOSAL / OPEN** — Что «Automobile core concepts and safety» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0764 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Automobile core concepts and safety»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0765 · PROPOSAL / OPEN** — Сколько должно жить состояние «Automobile core concepts and safety»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0766 · PROPOSAL / OPEN** — Где именно выполняется «Automobile core concepts and safety»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0767 · PROPOSAL / OPEN** — Как «Automobile core concepts and safety» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0768 · PROPOSAL / OPEN** — Как отменять или отзывать «Automobile core concepts and safety»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0769 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Automobile core concepts and safety»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0770 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Automobile core concepts and safety»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0771 · PROPOSAL / OPEN** — Как пользователю доказать результат «Automobile core concepts and safety»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0772 · PROPOSAL / OPEN** — Кто платит за «Automobile core concepts and safety» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0773 · PROPOSAL / OPEN** — Как изолировать безопасность «Automobile core concepts and safety»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: physical safety.
- **Q0774 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Automobile core concepts and safety»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0775 · PROPOSAL / OPEN** — В какой релиз «Automobile core concepts and safety» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 32 · Tesla Fleet API and commands

**Контекст:** Owner-authorized Tesla connector via Fleet API. **Граница:** virtual key and command availability vary. **Этап:** Future. **Основной риск:** sensitive unlock/drive operations.

- **Q0776 · PROPOSAL / OPEN** — Как Secretary получает доступ к Tesla? **Рекомендация:** OAuth scopes/Fleet API и owner-mediated virtual-key pairing при необходимости
- **Q0777 · PROPOSAL / OPEN** — Какие Tesla-команды реалистичны? **Рекомендация:** Навигация, климат, зарядка, некоторые media/windows/locks по конкретной машине и API
- **Q0778 · PROPOSAL / OPEN** — Может ли Secretary открыть окно во время движения? **Рекомендация:** Только при подтверждённой доступности/безопасном состоянии; OEM API может не поддерживать
- **Q0779 · PROPOSAL / OPEN** — Где хранить signing key для Tesla commands? **Рекомендация:** Защищённый connector/secret store с отзывом
- **Q0780 · PROPOSAL / OPEN** — Что делать если Tesla API command возвращает receipt, но авто offline? **Рекомендация:** Состояние pending/unknown и проверка, не утверждать success
- **Q0781 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Tesla Fleet API and commands»? **Рекомендация:** Определить успех как: Owner-authorized Tesla connector via Fleet API; не включать недоказанные обещания.
- **Q0782 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Tesla Fleet API and commands»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0783 · PROPOSAL / OPEN** — Как инициируется работа «Tesla Fleet API and commands»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0784 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Tesla Fleet API and commands»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0785 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Tesla Fleet API and commands»? **Рекомендация:** Уважать boundary: virtual key and command availability vary; не расширять ее эвристикой модели.
- **Q0786 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Tesla Fleet API and commands»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0787 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Tesla Fleet API and commands»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0788 · PROPOSAL / OPEN** — Что «Tesla Fleet API and commands» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0789 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Tesla Fleet API and commands»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0790 · PROPOSAL / OPEN** — Сколько должно жить состояние «Tesla Fleet API and commands»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0791 · PROPOSAL / OPEN** — Где именно выполняется «Tesla Fleet API and commands»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0792 · PROPOSAL / OPEN** — Как «Tesla Fleet API and commands» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0793 · PROPOSAL / OPEN** — Как отменять или отзывать «Tesla Fleet API and commands»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0794 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Tesla Fleet API and commands»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0795 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Tesla Fleet API and commands»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0796 · PROPOSAL / OPEN** — Как пользователю доказать результат «Tesla Fleet API and commands»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0797 · PROPOSAL / OPEN** — Кто платит за «Tesla Fleet API and commands» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0798 · PROPOSAL / OPEN** — Как изолировать безопасность «Tesla Fleet API and commands»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: sensitive unlock/drive operations.
- **Q0799 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Tesla Fleet API and commands»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0800 · PROPOSAL / OPEN** — В какой релиз «Tesla Fleet API and commands» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 33 · Non-Tesla OEM and Smartcar connectors

**Контекст:** Vendor-dependent remote car capabilities across brands. **Граница:** no universal window/motion controls via standard API. **Этап:** Future. **Основной риск:** false equivalence.

- **Q0801 · PROPOSAL / OPEN** — Может ли Smartcar управлять любой маркой? **Рекомендация:** Нет, только участвующие бренды/машины/доступные функции
- **Q0802 · PROPOSAL / OPEN** — Как организовать per-OEM plugins? **Рекомендация:** Capability adapters плюс покрытие моделей и версий
- **Q0803 · PROPOSAL / OPEN** — Можно ли заменить отсутствующий API машиной с OBD dongle? **Рекомендация:** Для безопасного диагностического чтения возможно, не считать автоматическим actuators мостом
- **Q0804 · PROPOSAL / OPEN** — Что с оплатой стороннего vehicle API? **Рекомендация:** Показывать API costs, quota, разрешения
- **Q0805 · PROPOSAL / OPEN** — Что делать если владелец отзывает car access? **Рекомендация:** Никаких дальнейших команд, явная ошибка и tool revocation
- **Q0806 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Определить успех как: Vendor-dependent remote car capabilities across brands; не включать недоказанные обещания.
- **Q0807 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0808 · PROPOSAL / OPEN** — Как инициируется работа «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0809 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0810 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Уважать boundary: no universal window/motion controls via standard API; не расширять ее эвристикой модели.
- **Q0811 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0812 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0813 · PROPOSAL / OPEN** — Что «Non-Tesla OEM and Smartcar connectors» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0814 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0815 · PROPOSAL / OPEN** — Сколько должно жить состояние «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0816 · PROPOSAL / OPEN** — Где именно выполняется «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0817 · PROPOSAL / OPEN** — Как «Non-Tesla OEM and Smartcar connectors» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0818 · PROPOSAL / OPEN** — Как отменять или отзывать «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0819 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0820 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0821 · PROPOSAL / OPEN** — Как пользователю доказать результат «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0822 · PROPOSAL / OPEN** — Кто платит за «Non-Tesla OEM and Smartcar connectors» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0823 · PROPOSAL / OPEN** — Как изолировать безопасность «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: false equivalence.
- **Q0824 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Non-Tesla OEM and Smartcar connectors»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0825 · PROPOSAL / OPEN** — В какой релиз «Non-Tesla OEM and Smartcar connectors» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 34 · In-car voice navigation and media

**Контекст:** Hands-free car companion for navigation music cabin controls. **Граница:** driving-critical functions are out of scope without OEM support. **Этап:** Future. **Основной риск:** distraction.

- **Q0826 · PROPOSAL / OPEN** — Как отправить адрес в навигацию авто? **Рекомендация:** Через OEM command либо поддерживаемую phone mapping/navigation handoff
- **Q0827 · PROPOSAL / OPEN** — Можно ли менять музыку в любом авто? **Рекомендация:** Через доступную телефонную media session/партнёрское API, не обещать прямой доступ к head unit
- **Q0828 · PROPOSAL / OPEN** — Как распознать контекст «я за рулём»? **Рекомендация:** Надёжный car/phone context, при сомнении не расширять права
- **Q0829 · PROPOSAL / OPEN** — Нужно ли управление стеклоподъёмниками из голосового ассистента? **Рекомендация:** Только через явно документированный OEM capability + safety state
- **Q0830 · PROPOSAL / OPEN** — Должен ли Secretary сам начинать движение автомобиля? **Рекомендация:** Нет, planning/navigation без autonomous driving control
- **Q0831 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «In-car voice navigation and media»? **Рекомендация:** Определить успех как: Hands-free car companion for navigation music cabin controls; не включать недоказанные обещания.
- **Q0832 · PROPOSAL / OPEN** — Кому принадлежит право активировать «In-car voice navigation and media»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0833 · PROPOSAL / OPEN** — Как инициируется работа «In-car voice navigation and media»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0834 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «In-car voice navigation and media»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0835 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «In-car voice navigation and media»? **Рекомендация:** Уважать boundary: driving-critical functions are out of scope without OEM support; не расширять ее эвристикой модели.
- **Q0836 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «In-car voice navigation and media»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0837 · PROPOSAL / OPEN** — Как пользователь управляет настройками «In-car voice navigation and media»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0838 · PROPOSAL / OPEN** — Что «In-car voice navigation and media» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0839 · PROPOSAL / OPEN** — Где хранится каноническое состояние «In-car voice navigation and media»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0840 · PROPOSAL / OPEN** — Сколько должно жить состояние «In-car voice navigation and media»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0841 · PROPOSAL / OPEN** — Где именно выполняется «In-car voice navigation and media»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0842 · PROPOSAL / OPEN** — Как «In-car voice navigation and media» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0843 · PROPOSAL / OPEN** — Как отменять или отзывать «In-car voice navigation and media»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0844 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «In-car voice navigation and media»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0845 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «In-car voice navigation and media»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0846 · PROPOSAL / OPEN** — Как пользователю доказать результат «In-car voice navigation and media»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0847 · PROPOSAL / OPEN** — Кто платит за «In-car voice navigation and media» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0848 · PROPOSAL / OPEN** — Как изолировать безопасность «In-car voice navigation and media»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: distraction.
- **Q0849 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «In-car voice navigation and media»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0850 · PROPOSAL / OPEN** — В какой релиз «In-car voice navigation and media» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 35 · Payments transfers purchases and booking

**Контекст:** Authorized multi-person transactional workflows. **Граница:** proposals are not transfers or bookings. **Этап:** Later. **Основной риск:** partial payment failure.

- **Q0851 · CONFIRMED** — Что означает «Z бронирует за всех»? **Рекомендация:** Circle распределяет ответственных, Secretary Z исполняет подтверждённое бронирование
- **Q0852 · CONFIRMED** — Может ли Circle предлагать займ? **Рекомендация:** Да, как вариант; Z должен явно принять условия
- **Q0853 · CONFIRMED** — Кто запрашивает долю Ника и Оры? **Рекомендация:** Их персональные Secretaries по scoped request от Circle
- **Q0854 · CONFIRMED** — Можно ли выполнить платёж сразу после согласия в чате? **Рекомендация:** Только если подключённый payment tool поддерживает точную авторизацию
- **Q0855 · PROPOSAL / OPEN** — Что если платёж успешен, а бронь упала? **Рекомендация:** Durable pending/reconciliation, компенсационные варианты по новому согласию
- **Q0856 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Payments transfers purchases and booking»? **Рекомендация:** Определить успех как: Authorized multi-person transactional workflows; не включать недоказанные обещания.
- **Q0857 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Payments transfers purchases and booking»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0858 · PROPOSAL / OPEN** — Как инициируется работа «Payments transfers purchases and booking»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0859 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Payments transfers purchases and booking»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0860 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Payments transfers purchases and booking»? **Рекомендация:** Уважать boundary: proposals are not transfers or bookings; не расширять ее эвристикой модели.
- **Q0861 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Payments transfers purchases and booking»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0862 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Payments transfers purchases and booking»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0863 · PROPOSAL / OPEN** — Что «Payments transfers purchases and booking» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0864 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Payments transfers purchases and booking»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0865 · PROPOSAL / OPEN** — Сколько должно жить состояние «Payments transfers purchases and booking»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0866 · PROPOSAL / OPEN** — Где именно выполняется «Payments transfers purchases and booking»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0867 · PROPOSAL / OPEN** — Как «Payments transfers purchases and booking» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0868 · PROPOSAL / OPEN** — Как отменять или отзывать «Payments transfers purchases and booking»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0869 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Payments transfers purchases and booking»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0870 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Payments transfers purchases and booking»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0871 · PROPOSAL / OPEN** — Как пользователю доказать результат «Payments transfers purchases and booking»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0872 · PROPOSAL / OPEN** — Кто платит за «Payments transfers purchases and booking» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0873 · PROPOSAL / OPEN** — Как изолировать безопасность «Payments transfers purchases and booking»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: partial payment failure.
- **Q0874 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Payments transfers purchases and booking»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0875 · PROPOSAL / OPEN** — В какой релиз «Payments transfers purchases and booking» попадёт? **Рекомендация:** Предлагаемый этап: Later; status не повышать, пока owner и acceptance evidence не готовы.

### 36 · Calendar email travel and shared planning

**Контекст:** Personal schedule access and delegated trip coordination. **Граница:** private calendar raw events remain private. **Этап:** Later. **Основной риск:** unwanted disclosure.

- **Q0876 · PROPOSAL / OPEN** — Когда Circle может читать календарь? **Рекомендация:** Персональный Secretary выдаёт free/busy или релевантное ограничение
- **Q0877 · PROPOSAL / OPEN** — Нужен ли общий семейный календарь? **Рекомендация:** Опционально отдельно от private calendars
- **Q0878 · PROPOSAL / OPEN** — Как договариваться о датах без раскрытия подробностей встреч? **Рекомендация:** Предлагать доступные слоты, не личные названия событий
- **Q0879 · PROPOSAL / OPEN** — Может ли Secretary написать email приглашённому? **Рекомендация:** Только через разрешённую интеграцию и с явным адресатом
- **Q0880 · PROPOSAL / OPEN** — Когда бронь поездки становится общим артефактом? **Рекомендация:** После подтверждения участниками в Circle, с ограничениями доступа
- **Q0881 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Calendar email travel and shared planning»? **Рекомендация:** Определить успех как: Personal schedule access and delegated trip coordination; не включать недоказанные обещания.
- **Q0882 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Calendar email travel and shared planning»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0883 · PROPOSAL / OPEN** — Как инициируется работа «Calendar email travel and shared planning»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0884 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Calendar email travel and shared planning»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0885 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Calendar email travel and shared planning»? **Рекомендация:** Уважать boundary: private calendar raw events remain private; не расширять ее эвристикой модели.
- **Q0886 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Calendar email travel and shared planning»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0887 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Calendar email travel and shared planning»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0888 · PROPOSAL / OPEN** — Что «Calendar email travel and shared planning» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0889 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Calendar email travel and shared planning»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0890 · PROPOSAL / OPEN** — Сколько должно жить состояние «Calendar email travel and shared planning»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0891 · PROPOSAL / OPEN** — Где именно выполняется «Calendar email travel and shared planning»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0892 · PROPOSAL / OPEN** — Как «Calendar email travel and shared planning» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0893 · PROPOSAL / OPEN** — Как отменять или отзывать «Calendar email travel and shared planning»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0894 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Calendar email travel and shared planning»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0895 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Calendar email travel and shared planning»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0896 · PROPOSAL / OPEN** — Как пользователю доказать результат «Calendar email travel and shared planning»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0897 · PROPOSAL / OPEN** — Кто платит за «Calendar email travel and shared planning» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0898 · PROPOSAL / OPEN** — Как изолировать безопасность «Calendar email travel and shared planning»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: unwanted disclosure.
- **Q0899 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Calendar email travel and shared planning»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0900 · PROPOSAL / OPEN** — В какой релиз «Calendar email travel and shared planning» попадёт? **Рекомендация:** Предлагаемый этап: Later; status не повышать, пока owner и acceptance evidence не готовы.

### 37 · Integration SDK adapters and ecosystem

**Контекст:** Extensible connectors built on declared capabilities. **Граница:** no privileged arbitrary plugin by default. **Этап:** Future. **Основной риск:** supply chain risk.

- **Q0901 · PROPOSAL / OPEN** — Нужно ли каждое внешнее приложение писать в Core? **Рекомендация:** Нет, provider adapters/SDK с версионированными capabilities
- **Q0902 · PROPOSAL / OPEN** — Должен ли быть магазин интеграций в v1? **Рекомендация:** Нет, протокол сначала, marketplace позже
- **Q0903 · PROPOSAL / OPEN** — Как внешнее приложение объявляет permissions? **Рекомендация:** Типизированные scopes и explicit user grants
- **Q0904 · PROPOSAL / OPEN** — Как защитить third-party plugins? **Рекомендация:** Signed releases, provenance и изоляция по риску
- **Q0905 · PROPOSAL / OPEN** — Можно ли подключить самописный Home Bridge? **Рекомендация:** Да после проверки auth, schema и host boundaries
- **Q0906 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Integration SDK adapters and ecosystem»? **Рекомендация:** Определить успех как: Extensible connectors built on declared capabilities; не включать недоказанные обещания.
- **Q0907 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Integration SDK adapters and ecosystem»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0908 · PROPOSAL / OPEN** — Как инициируется работа «Integration SDK adapters and ecosystem»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0909 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Integration SDK adapters and ecosystem»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0910 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Integration SDK adapters and ecosystem»? **Рекомендация:** Уважать boundary: no privileged arbitrary plugin by default; не расширять ее эвристикой модели.
- **Q0911 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Integration SDK adapters and ecosystem»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0912 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Integration SDK adapters and ecosystem»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0913 · PROPOSAL / OPEN** — Что «Integration SDK adapters and ecosystem» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0914 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Integration SDK adapters and ecosystem»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0915 · PROPOSAL / OPEN** — Сколько должно жить состояние «Integration SDK adapters and ecosystem»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0916 · PROPOSAL / OPEN** — Где именно выполняется «Integration SDK adapters and ecosystem»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0917 · PROPOSAL / OPEN** — Как «Integration SDK adapters and ecosystem» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0918 · PROPOSAL / OPEN** — Как отменять или отзывать «Integration SDK adapters and ecosystem»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0919 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Integration SDK adapters and ecosystem»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0920 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Integration SDK adapters and ecosystem»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0921 · PROPOSAL / OPEN** — Как пользователю доказать результат «Integration SDK adapters and ecosystem»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0922 · PROPOSAL / OPEN** — Кто платит за «Integration SDK adapters and ecosystem» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0923 · PROPOSAL / OPEN** — Как изолировать безопасность «Integration SDK adapters and ecosystem»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: supply chain risk.
- **Q0924 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Integration SDK adapters and ecosystem»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0925 · PROPOSAL / OPEN** — В какой релиз «Integration SDK adapters and ecosystem» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 38 · Setup upgrades services and onboarding

**Контекст:** One command plus terminal setup then simple controls. **Граница:** no mandatory cloud account in dev v1. **Этап:** Baseline. **Основной риск:** fragile installation.

- **Q0926 · PROPOSAL / OPEN** — С чего начинается первая установка? **Рекомендация:** Проверка OS, harnesses/auth и нужных компонентов, затем terminal wizard
- **Q0927 · PROPOSAL / OPEN** — Нужен ли обязательный браузер? **Рекомендация:** Нет, Developer Edition terminal-first, Web опционален
- **Q0928 · PROPOSAL / OPEN** — Можно ли менять настройки без TOML вручную? **Рекомендация:** Да через secretary config TUI
- **Q0929 · PROPOSAL / OPEN** — Нужен ли Windows installer? **Рекомендация:** По платформенной release матрице, не считать поддержку доказанной наличием сборки
- **Q0930 · PROPOSAL / OPEN** — Как обновлять службу без потери Task? **Рекомендация:** Graceful quiesce, upgrade schema, native session compatibility checks
- **Q0931 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Setup upgrades services and onboarding»? **Рекомендация:** Определить успех как: One command plus terminal setup then simple controls; не включать недоказанные обещания.
- **Q0932 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Setup upgrades services and onboarding»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0933 · PROPOSAL / OPEN** — Как инициируется работа «Setup upgrades services and onboarding»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0934 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Setup upgrades services and onboarding»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0935 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Setup upgrades services and onboarding»? **Рекомендация:** Уважать boundary: no mandatory cloud account in dev v1; не расширять ее эвристикой модели.
- **Q0936 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Setup upgrades services and onboarding»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0937 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Setup upgrades services and onboarding»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0938 · PROPOSAL / OPEN** — Что «Setup upgrades services and onboarding» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0939 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Setup upgrades services and onboarding»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0940 · PROPOSAL / OPEN** — Сколько должно жить состояние «Setup upgrades services and onboarding»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0941 · PROPOSAL / OPEN** — Где именно выполняется «Setup upgrades services and onboarding»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0942 · PROPOSAL / OPEN** — Как «Setup upgrades services and onboarding» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0943 · PROPOSAL / OPEN** — Как отменять или отзывать «Setup upgrades services and onboarding»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0944 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Setup upgrades services and onboarding»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0945 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Setup upgrades services and onboarding»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0946 · PROPOSAL / OPEN** — Как пользователю доказать результат «Setup upgrades services and onboarding»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0947 · PROPOSAL / OPEN** — Кто платит за «Setup upgrades services and onboarding» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0948 · PROPOSAL / OPEN** — Как изолировать безопасность «Setup upgrades services and onboarding»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: fragile installation.
- **Q0949 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Setup upgrades services and onboarding»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0950 · PROPOSAL / OPEN** — В какой релиз «Setup upgrades services and onboarding» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

### 39 · Consumer Edition pricing and managed ops

**Контекст:** Same product core with easy managed onboarding. **Граница:** billing provider supply and isolation later. **Этап:** Future. **Основной риск:** unclear tenant isolation.

- **Q0951 · BASELINE** — Нужен ли отдельный продукт для Consumer Edition? **Рекомендация:** Нет, единое ядро с другим managed provisioning
- **Q0952 · BASELINE** — Кто оплачивает inference у consumer? **Рекомендация:** Легальный коммерческий provider/compute путь, модель не зафиксирована
- **Q0953 · PROPOSAL / OPEN** — Можно ли использовать потребительские подписки одних клиентов для других? **Рекомендация:** Нет; не считать их переносимым коммерческим supply
- **Q0954 · BASELINE** — Как изолировать разных customers? **Рекомендация:** Hard tenant isolation данных, credentials и compute с penetration/live tests
- **Q0955 · PROPOSAL / OPEN** — Должен ли signup требовать знания ACP? **Рекомендация:** Нет, consumer onboarding hides models/harness management
- **Q0956 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Consumer Edition pricing and managed ops»? **Рекомендация:** Определить успех как: Same product core with easy managed onboarding; не включать недоказанные обещания.
- **Q0957 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Consumer Edition pricing and managed ops»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0958 · PROPOSAL / OPEN** — Как инициируется работа «Consumer Edition pricing and managed ops»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0959 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Consumer Edition pricing and managed ops»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0960 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Consumer Edition pricing and managed ops»? **Рекомендация:** Уважать boundary: billing provider supply and isolation later; не расширять ее эвристикой модели.
- **Q0961 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Consumer Edition pricing and managed ops»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0962 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Consumer Edition pricing and managed ops»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0963 · PROPOSAL / OPEN** — Что «Consumer Edition pricing and managed ops» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0964 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Consumer Edition pricing and managed ops»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0965 · PROPOSAL / OPEN** — Сколько должно жить состояние «Consumer Edition pricing and managed ops»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0966 · PROPOSAL / OPEN** — Где именно выполняется «Consumer Edition pricing and managed ops»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0967 · PROPOSAL / OPEN** — Как «Consumer Edition pricing and managed ops» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0968 · PROPOSAL / OPEN** — Как отменять или отзывать «Consumer Edition pricing and managed ops»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0969 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Consumer Edition pricing and managed ops»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0970 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Consumer Edition pricing and managed ops»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0971 · PROPOSAL / OPEN** — Как пользователю доказать результат «Consumer Edition pricing and managed ops»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0972 · PROPOSAL / OPEN** — Кто платит за «Consumer Edition pricing and managed ops» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0973 · PROPOSAL / OPEN** — Как изолировать безопасность «Consumer Edition pricing and managed ops»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: unclear tenant isolation.
- **Q0974 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Consumer Edition pricing and managed ops»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q0975 · PROPOSAL / OPEN** — В какой релиз «Consumer Edition pricing and managed ops» попадёт? **Рекомендация:** Предлагаемый этап: Future; status не повышать, пока owner и acceptance evidence не готовы.

### 40 · Testing CI release acceptance and governance

**Контекст:** Evidence-driven end-to-end product delivery. **Граница:** simulated tests do not prove real native readiness. **Этап:** Baseline. **Основной риск:** misleading shipping claims.

- **Q0976 · PROPOSAL / OPEN** — Достаточны ли unit tests для заявленного v1? **Рекомендация:** Нет, black-box API + channel + Node + live native acceptance
- **Q0977 · PROPOSAL / OPEN** — Как фиксировать PR review на exact HEAD? **Рекомендация:** SHA-stable review и CI statuses для именно этого HEAD
- **Q0978 · PROPOSAL / OPEN** — Можно ли сливать draft по завершению checklist? **Рекомендация:** Нет, явное owner sign-off требуется
- **Q0979 · PROPOSAL / OPEN** — Какие failure cases включить для денежных/car/home actions? **Рекомендация:** Offline, timeout, retry, revoked permission, duplicate command и confirmed receipt
- **Q0980 · PROPOSAL / OPEN** — Можно ли объявлять feature shipped без published acceptance? **Рекомендация:** Нет, разделять target, code evidence, integration evidence и live verification
- **Q0981 · PROPOSAL / OPEN** — Каков конкретный пользовательский результат для «Testing CI release acceptance and governance»? **Рекомендация:** Определить успех как: Evidence-driven end-to-end product delivery; не включать недоказанные обещания.
- **Q0982 · PROPOSAL / OPEN** — Кому принадлежит право активировать «Testing CI release acceptance and governance»? **Рекомендация:** Только идентифицированному Person или участнику с явно делегированным scope; иной доступ запрещён.
- **Q0983 · PROPOSAL / OPEN** — Как инициируется работа «Testing CI release acceptance and governance»? **Рекомендация:** Явный запрос пользователя по умолчанию; автоматическое начало — только по отдельно согласованной политике.
- **Q0984 · PROPOSAL / OPEN** — Какие входные данные нужны перед стартом «Testing CI release acceptance and governance»? **Рекомендация:** Запрашивать только достаточный контекст и подтверждать критически недостающие параметры.
- **Q0985 · PROPOSAL / OPEN** — Какие действия заведомо вне границы «Testing CI release acceptance and governance»? **Рекомендация:** Уважать boundary: simulated tests do not prove real native readiness; не расширять ее эвристикой модели.
- **Q0986 · PROPOSAL / OPEN** — Как обнаруживаются доступные функции «Testing CI release acceptance and governance»? **Рекомендация:** Предпочитать verified capability discovery и явно показывать unsupported operations.
- **Q0987 · PROPOSAL / OPEN** — Как пользователь управляет настройками «Testing CI release acceptance and governance»? **Рекомендация:** Читаемый конфиг плюс удобный UI/CLI, версионирование и безопасные defaults.
- **Q0988 · PROPOSAL / OPEN** — Что «Testing CI release acceptance and governance» может прочитать из персональных данных? **Рекомендация:** Только необходимые поля по purpose-limited scope; чужая raw private history закрыта.
- **Q0989 · PROPOSAL / OPEN** — Где хранится каноническое состояние «Testing CI release acceptance and governance»? **Рекомендация:** В authority domain store с IDs/provenance, не в ответах модели или UI-кэше.
- **Q0990 · PROPOSAL / OPEN** — Сколько должно жить состояние «Testing CI release acceptance and governance»? **Рекомендация:** Жизненный цикл tied to task/user retention, explicit archive/delete, безопасный restore.
- **Q0991 · PROPOSAL / OPEN** — Где именно выполняется «Testing CI release acceptance and governance»? **Рекомендация:** На supported environment с явно доступными tools; Server control plane не притворяется Node.
- **Q0992 · PROPOSAL / OPEN** — Как «Testing CI release acceptance and governance» ведёт себя offline? **Рекомендация:** Честное недоступно/degraded состояние, локальная независимая автоматика — где реализована.
- **Q0993 · PROPOSAL / OPEN** — Как отменять или отзывать «Testing CI release acceptance and governance»? **Рекомендация:** Разделить pending cancel, фактическое прекращение, irreversible external effects и revoke grants.
- **Q0994 · PROPOSAL / OPEN** — Как обрабатывать одновременные запросы к «Testing CI release acceptance and governance»? **Рекомендация:** Стабильные IDs, определённая очередь/конкуренция и предотвращение конфликтующих actions.
- **Q0995 · PROPOSAL / OPEN** — Что делать после timeout или неизвестного результата «Testing CI release acceptance and governance»? **Рекомендация:** Unknown != failed != succeeded; не повторять опасную операцию без reconcile/idempotency.
- **Q0996 · PROPOSAL / OPEN** — Как пользователю доказать результат «Testing CI release acceptance and governance»? **Рекомендация:** Показать проверенный receipt/state, кто инициировал и что подтвердил provider.
- **Q0997 · PROPOSAL / OPEN** — Кто платит за «Testing CI release acceptance and governance» и как ограничить расход? **Рекомендация:** Привязать usage к owner/delegation, caps, quotas, понятная статистика и отказ сверх лимита.
- **Q0998 · PROPOSAL / OPEN** — Как изолировать безопасность «Testing CI release acceptance and governance»? **Рекомендация:** Scoped credentials, least necessary capabilities для интеграций, логирование без секретов; риск: misleading shipping claims.
- **Q0999 · PROPOSAL / OPEN** — Какие проверки должны блокировать release «Testing CI release acceptance and governance»? **Рекомендация:** Автоматизированные failure/idempotency/privacy тесты и live E2E по доступному физическому/native seam.
- **Q1000 · PROPOSAL / OPEN** — В какой релиз «Testing CI release acceptance and governance» попадёт? **Рекомендация:** Предлагаемый этап: Baseline; status не повышать, пока owner и acceptance evidence не готовы.

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
- Answer IDs `Q0001`–`Q1000` and the original Q4h–Q4t are stable review references.
- Every follow-up proposal must say what dependency it requires; ask one question at a time when grilling resumes.
- A feature can be a dream, a proposed requirement, a baseline goal, an implemented behavior, or live-accepted. These are **different** statuses.
- No external integration is declared universally supported solely because an API exists.
- No actual purchases, vehicle commands, smart-home changes, billing, or releases are authorized by this spec.


## Review protocol

- Every question gets a numbered identifier, a proposed answer, a phase and a non-approval status. The owner can respond with changed IDs; only explicit approval changes statuses.
- Resolved terms update `GLOSSARY.md` at the appropriate point. ADRs are reserved for surprising, hard-to-reverse trade-offs with genuine alternatives.
- The owner can ask to expand any branch, and new prerequisites invalidate only dependent decisions.
- Final sign-off is required before treating the new vision as canonical requirements or merging PR #212. No code changes are implied.
