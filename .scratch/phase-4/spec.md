# Secretary Phase 4: One Secretary across machines

Status: approved

## 1. Purpose

Phase 4 превращает основу Phase 2 и Phase 3 в продукт, которым можно пользоваться каждый день.

Secretary должен ощущаться как один постоянный личный AI, расположенный выше компьютеров, Projects и agent harnesses. Пользователь не обязан знать, где находится нужный репозиторий, какой компьютер сейчас доступен и какой harness лучше выбрать. Он говорит Secretary, что нужно сделать. Secretary сохраняет намерение, выбирает исполнителя, следит за работой и возвращает результат.

Phase 4 deployment остаётся self-hosted и private-network-first. Это ограничение первой поставки, а не обязательное свойство будущей hosted-версии продукта.

## 2. Product statement

> Secretary is a persistent personal AI above computers and agent harnesses. It owns user intent, sends the right Worker to the right Project and Node, and keeps track of the work until the Result is delivered.

Фундаментальный принцип:

```text
Secretary owns intent. Worker owns execution.
```

Secretary не является ещё одним coding agent и не должен заменять Claude Code, Codex, Pi или `fx`. Его задача состоит в том, чтобы пользователь обращался к одной сущности, а execution выполнялся подходящим агентом на подходящем компьютере.

## 3. User benefit

Пользователь получает:

- одну точку входа для всех задач;
- возможность поручить работу без ручного выбора машины и harness;
- немедленное подтверждение, что просьба сохранена и получила владельца;
- выполнение в фоне, пока пользователь занят или отключён;
- один список Workers вместо набора разрозненных agent sessions;
- продолжение работы того же Worker после уточнения или перезапуска;
- результат, который не исчезает вместе с терминалом, процессом или браузером;
- управление из любого доверенного Client;
- явную ошибку вместо потерянной задачи или silent fallback.

Пользователь должен думать:

```text
Secretary, fix the mobile header in the frontend project.
```

а не:

```text
Which agent should I open, on which computer, with which workspace?
```

Главное ощущение продукта: пользователь передаёт работу, а не запускает очередной чат.

## 4. Core principles

### 4.1. Один Secretary и одна Conversation

В Phase 4 один Person владеет одним основным Secretary и одной Personal Conversation. Multi-Secretary, personas, team accounts и shared workspaces не входят в MVP.

### 4.2. Server является source of truth

Secretary server владеет Conversation, Workers, Turns, Attempts, AttemptOutcomes, Results, Projects, Nodes, Clients, Approvals и delivery state. Clients не владеют локальной копией состояния и не решают, выполнена ли работа.

### 4.3. Secretary владеет намерением

Secretary знает пользователя, Conversation, `user.md`, Projects и доступные HarnessInstances. Он решает:

- ответить напрямую;
- задать необходимое уточнение;
- продолжить существующий Worker;
- создать Worker;
- поставить Worker в очередь;
- сообщить об Approval, failure или offline Node;
- закрыть Worker.

Модель не выбирает произвольный канал доставки. Канал определяется source metadata и server routing.

### 4.4. Worker владеет выполнением

Worker получает intent, Project, Node, HarnessInstance, workspace policy и completion contract. Он пользуется возможностями своего harness и может запускать внутренние subagents. Каждый Attempt завершается AttemptOutcome. После завершения Turn server создаёт один пользовательский Result.

Внутренние subagents harness не являются Secretary Workers. Для Secretary server они являются activity одного Worker.

### 4.5. Actionable request получает владельца сразу

Для любой выполнимой просьбы Secretary первым существенным действием создаёт Worker либо направляет сообщение существующему Worker. Он не должен долго выполнять работу сам, а затем решать, нужен ли Worker.

"Сразу" означает:

1. сообщение сохранено после durable commit;
2. Client получил подтверждение сохранения;
3. Worker создан, поставлен в очередь или причина ожидания стала видимой;
4. работа выполняется асинхронно.

Это не означает немедленное выполнение опасного действия. Policy и Approval остаются обязательными.

### 4.6. Worker является основной product entity

Пользователь видит Worker, его Project, Node, harness, текущий статус и историю Turns. Отдельная сущность Task не является частью целевой модели.

Phase 3 использовал Task как внутреннюю запись. В Phase 4 эта запись считается legacy migration data и не появляется в пользовательском API, UI или Secretary tools.

### 4.7. Identity не равна runtime session

Secretary identity и Worker identity должны переживать замену или потерю native runtime session.

Runtime session является локальной деталью harness и cache/optimization. Она не является частью cloud domain model и не используется как публичный идентификатор.

### 4.8. Никакого silent fallback

Недоступный harness, Node, workspace или runtime session создаёт явное состояние и ошибку. Нельзя незаметно заменить Claude Code на `fx`, создать новую session вместо восстановления или сообщить о success до terminal Result.

### 4.9. At-least-once delivery с idempotency

Сбой сети может привести к повторной доставке, но не к повторному Conversation entry, Worker Turn, Attempt или Result. Намеренный retry создаёт новый Attempt того же Turn и не считается дубликатом. Retry выполняется только внутренней server operation после terminal AttemptOutcome с явной классификацией `retryable`; uncertain execution никогда не retry-ится автоматически и получает `interrupted`. `retry_attempt` не входит в Secretary tools или Client API. Side-effecting operations получают idempotency key и durable outcome.

### 4.10. Client и Node имеют разные роли

Client разговаривает с Secretary. Node запускает Worker с доступом к компьютеру. Один компьютер может одновременно быть Client и Node, но credentials, pairing и revoke для них независимы.

## 5. Terminology

Термины соответствуют `CONTEXT.md`.

### Person

Владелец Secretary. В Phase 4 используется один configured owner.

### Secretary server

Долгоживущий server, который хранит durable state, принимает сообщения, управляет Secretary identity и синхронизирует Clients и Nodes.

### Secretary identity

Постоянная личность Secretary, связанная с Person и Personal Conversation. Она сохраняется независимо от того, какая native runtime session сейчас обслуживает очередной turn.

### Secretary runtime

Текущая runtime session, через которую Secretary обрабатывает намерение. Это заменяемая реализация Secretary identity, а не сама identity.

### Secretary turn

Один последовательный шаг Secretary runtime над Personal Conversation. Он имеет собственный stream и не является Worker Turn. Одновременно выполняется только один Secretary turn.

### Client

Доверенный пользовательский интерфейс: Web, Telegram adapter, Pi client или CLI. Client отправляет сообщения, показывает Conversation и вызывает разрешённые действия.

### Execution Node

Отдельный процесс на компьютере, который устанавливает outbound connection к Secretary server, сообщает HarnessInstances и запускает Workers.

### Project

Именованная рабочая область, доступная на одном или нескольких Nodes. Project содержит path mappings и execution policy.

### Worker

Основная пользовательская сущность execution. Worker создан для намерения пользователя, привязан к Project, Node и HarnessInstance и сохраняется до explicit close.

### Turn

Одно направление работы Worker: initial intent или Follow-up пользователя. Turn сохраняет входной текст, context snapshot, status, Attempts и один terminal Result после завершения.

### Attempt

Один конкретный запуск harness для Turn. Retry или восстановление может создать новый Attempt того же Turn. Retry остаётся внутренностью Turn и не создаёт новый пользовательский Result. Server хранит только lifecycle metadata, а не native runtime session id.

### AttemptOutcome

Terminal outcome конкретного Attempt. Он хранит status `succeeded`, `failed`, `canceled` или `interrupted`, error или failure code, классификацию `retryable` или `final`, timestamps и диагностические данные. `retryable` допустим только при явном доказательстве adapter/server policy, что повтор безопасен. `final` закрывает Turn с соответствующим status и создаёт его единственный Result; `retryable` оставляет Turn открытым для internal `retry_attempt`. Uncertain execution всегда классифицируется как `final`/`interrupted`, а не как retryable. AttemptOutcome не является отдельной записью в Personal Conversation.

### Result

Один пользовательский terminal outcome для одного Turn. Result создаётся после финального состояния Turn, независимо от того, сколько Attempts было до него. Он сохраняется в Conversation и доставляется в исходный пользовательский context.

### HarnessInstance

Наблюдаемая комбинация Node и harness, например `macbook/claude` или `home-server/fx`. Она сообщает version, authentication, capabilities, models и reasoning support.

### Approval

Durable запрос разрешения, который идёт от Worker или harness через Node и server к Clients владельца.

### Delivery

Попытка доставить Conversation entry, Result, Approval или важное событие во внешний Client или канал.

## 6. Target deployment

Целевая схема Phase 4:

```text
                         ┌────────────────────┐
 Web / Pi / Telegram ───▶│    Secretary       │
                         │  server + identity │
                         └─────────┬──────────┘
                                   │ outbound Node protocol
                     ┌─────────────┴─────────────┐
                     ▼                           ▼
              MacBook Node                 Home server Node
              fx / Claude / Codex           fx / Claude / Codex
```

Server и Node могут жить на одном MacBook в local setup. Это не отменяет разделение ролей и протокола.

### 6.1. Secretary server

`secretaryd` отвечает за:

- Person, Secretary identity и Personal Conversation;
- context reconstruction Secretary runtime;
- Secretary turn queue и live stream;
- Worker, Turn, Attempt, AttemptOutcome и Result lifecycle;
- Projects и Nodes registry;
- HarnessInstance inventory;
- Client authentication и delivery outbox;
- Approval routing;
- durable event log;
- Web API и WebSocket subscriptions.

### 6.2. Execution Node

`secretary-node` отвечает за:

- enrollment и heartbeat;
- local workspace mappings;
- HarnessInstance discovery;
- запуск и остановку Worker runtime;
- activity и permission events;
- локальное сохранение native runtime sessions;
- безопасный reconnect.

Node не хранит Personal Conversation как source of truth и не решает, куда направить Result.

### 6.3. Network

Удалённый Node устанавливает outbound connection к server. На первом этапе deployment использует private network, например Tailscale. Публичный входящий порт на Node не требуется.

Hosted Secretary и public deployment возможны позже, но не являются частью Phase 4 infrastructure proof.

## 7. Product scope

### 7.1. Phase 4 MVP

MVP должен включать:

- один Person и один Secretary;
- один Secretary server;
- минимум два зарегистрированных Nodes;
- одну Personal Conversation для всех Clients;
- полный live stream Secretary turn;
- один active Secretary turn с durable queue для входящих сообщений;
- Web Client;
- Telegram text adapter с Topics для Workers;
- manual Projects registry;
- простой editable `user.md`;
- `fx` как default Worker harness;
- Claude Code как обязательный harness;
- Codex как обязательный harness;
- существующий OpenCode adapter как дополнительный compatibility target, но не как замена Claude Code;
- HarnessInstance discovery с observed models и reasoning capabilities;
- Workers как основную сущность пользователя;
- Turns и Attempts без отдельной product Task entity;
- activity одного Worker, включая activity его внутренних subagents;
- cancel, steering, Follow-up, resume и explicit close;
- Worker-originated Approval flow;
- важные notifications: accepted, started, approval, input required, finished, failed, canceled и Node offline;
- durable delivery и idempotency;
- restart и recovery без silent duplicate execution;
- private pairing для Clients и Nodes;
- acceptance flow с двумя машинами.

### 7.2. Pi Client

Pi Client входит в целевую поверхность Phase 4, но подключается после стабилизации общего Client API. Он не должен требовать отдельного server-side состояния или нового domain model.

Сохранённый в `phase-1/` Pi snapshot является базой для Client integration, а не заменой Server/Node contract.

### 7.3. Не входит в MVP

- SaaS account system, billing и hosted provisioning;
- несколько владельцев, team accounts и multi-Secretary;
- auto-discovery всех репозиториев;
- сложная vector, dream или automatic learning memory;
- голосовые сообщения и полноценные media attachments;
- дополнительные каналы кроме Web и Telegram;
- sandboxing как полноценная security boundary;
- automatic merge, publish и GitHub App credential brokering;
- automatic retry execution после uncertain crash;
- public unauthenticated deployment;
- Pixel-perfect копирование TUI конкретного harness;
- server-side child Workers и child Task tree;
- Pi как отдельный Worker harness.

## 8. Fixed product decisions

Следующие решения считаются принятыми для Phase 4 и не должны заново открываться при разбиении на tickets:

1. Worker является основной product entity. Task не является частью пользовательской модели.
2. Worker сохраняется до explicit close. Каждый Attempt завершается AttemptOutcome, а каждый Turn получает один пользовательский Result. `final` outcome закрывает Turn; новый Attempt создаётся только внутренней server operation после доказанного terminal `retryable` outcome. Uncertain execution не retry-ится автоматически.
3. Лимит применяется только к active Attempts. Idle Workers не расходуют execution capacity.
4. Child Workers, parent Task и child Task отсутствуют в Secretary core. Subagents harness являются activity одного Worker.
5. Server Worker record не хранит native runtime session id, а Worker/harness не получает server callback capability. Session mapping принадлежит Node; terminal events идут через authenticated Node connection.
6. После создания Worker его Node и HarnessInstance binding неизменяемы. Если Node недоступен, Worker ждёт на этой привязке. Выполнение того же intent на другой машине или harness требует нового Worker.
7. Secretary runtime harness и default Worker harness выбираются независимыми policy: `secretary.harness` для Secretary и `worker_policy.default_harness` для Workers. Они могут обе указывать на `fx`, но это не одна настройка.
8. Secretary является persistent identity. Native runtime session можно пересоздать из canonical context.
9. `HarnessInstance` и Node inventory являются источником observed models, versions и capabilities. Legacy aliases `fast`, `smart` и `cheap` не входят в Phase 4 core.
10. `fx` является default/fallback только по явной policy. Fallback никогда не бывает silent.
11. Claude Code, Codex и `fx` обязательны для MVP acceptance. OpenCode сохраняется, но не заменяет Claude Code.
12. Telegram входит в первый product MVP.
13. Для Telegram General chat является Secretary, а Topic является Worker.
14. Pi Client подключается после стабилизации core Client API.
15. Approval приходит от Worker/harness через Node и server. Secretary не вызывает `request_approval` для собственных действий.
16. Secretary обрабатывает один turn за раз. Входящие сообщения во время его работы сохраняются в порядке поступления и ждут очереди; Workers при этом работают параллельно.
17. Phase 4 deployment является self-hosted/private-network-first. Hosted service откладывается.
18. Команда `sex` и команда `sex setup` сохраняются как текущий исторический CLI contract.

## 9. Secretary context reconstruction

Secretary runtime session не является долговременным хранилищем памяти.

При старте, reload или recovery server собирает context из canonical sources:

```text
Secretary context
├── Secretary identity
├── user.md
├── durable Conversation summary
├── recent Conversation entries
├── unseen Worker Results
├── open Workers and current statuses
├── Projects
├── Nodes and HarnessInstances
├── active Approvals
└── current policy and Profile snapshot
```

Правила:

- Conversation остаётся полной durable историей;
- summary является server-owned projection и может быть пересоздан;
- recent messages нужны для локального контекста текущего разговора;
- unseen Worker Results обязательно попадают в следующий Secretary context;
- native runtime session используется как оптимизация, если она доступна;
- отсутствие native session не меняет Secretary identity;
- новый runtime не должен молча продолжать старую работу без сохранённого Worker/Turn state.

Secretary не получает автоматический shell access к Node. Для execution он использует server-owned lifecycle tools.

`user.md` остаётся external Markdown file в user data directory. Server имеет durable revision и атомарный validated write; следующий Secretary turn читает актуальную revision. Изменение `user.md` не меняет уже сохранённые Worker policy snapshots.

## 10. Secretary behavior policy

Phase 4 Secretary Profile должна следовать таким правилам:

1. Сохраняй полное намерение пользователя, а не только последнюю строку.
2. Для actionable request первым существенным действием вызови `spawn_worker` или `message_worker`.
3. Не выполняй существенную работу сам, если она относится к Project, файлам, shell, web research или другому Execution environment.
4. Сразу сообщай, что работа сохранена, кому назначена и где будет выполняться.
5. Передавай Worker Project, Node preference, harness policy, ограничения и completion contract.
6. Не создавай новый Worker для обычного Follow-up существующего Worker.
7. Не выбирай канал доставки сам.
8. Не объявляй success до terminal Result.
9. Задавай короткое уточнение только тогда, когда без него нельзя безопасно выбрать Project, Node или действие.
10. Не скрывай offline, approval, input required или failure state.

Прямой ответ разрешён для:

- приветствия и короткой беседы;
- простого ответа без execution;
- status и help;
- управления существующим Worker;
- необходимого уточнения;
- объяснения уже сохранённого Result.

### 10.1. Secretary turn and stream

Secretary runtime обрабатывает ровно один model turn за раз. Обычные входящие сообщения во время его работы сохраняются в durable ordered queue и обрабатываются последовательно после завершения текущего turn. Control commands не становятся отдельными model turns. Workers и их Attempts при этом продолжают работать параллельно с Secretary и друг с другом.

Каждый Secretary turn имеет собственный live stream в Personal Conversation. Минимальный набор событий:

```text
secretary.turn.queued
secretary.turn.started
secretary.text_delta
secretary.thinking_summary
secretary.tool_call
secretary.tool_result
secretary.turn.finished
```

`thinking_summary` означает короткое безопасное резюме текущего шага, а не raw chain-of-thought. `tool_call` и `tool_result` показывают вызовы Secretary tools и их исходы. `secretary.turn.finished` содержит terminal state и error, если turn завершился неуспешно. Stream является server-owned и доступен подключённым Clients с обычными replay и idempotency правилами.

В основном разговоре Secretary stream показывается полностью, а Worker отображается компактным статусом:

```text
Secretary
● Working...

→ list_workers
← frontend · MacBook

→ spawn_worker
  Claude Code · MacBook
```

Полный activity stream Worker открывается отдельно в Worker observer.

### 10.2. Secretary tools

Минимальный lifecycle tool surface:

```text
list_nodes
list_projects
list_workers
get_worker
spawn_worker
message_worker
cancel_worker
close_worker
```

Семантика:

- `spawn_worker` создаёт Worker и первый Turn из полного user intent;
- `message_worker` направляет Steering, если Worker active;
- `message_worker` отвечает на pending `needs_input` через generic `respond_worker`;
- `message_worker` создаёт новый Follow-up Turn для idle Worker;
- `message_worker` пытается resume исходного Worker после `interrupted`;
- `cancel_worker` останавливает active Attempt, но не закрывает Worker;
- `close_worker` закрывает Worker после безопасной остановки;
- `list_*` и `get_worker` возвращают только server-owned state.

В Phase 4 нет Secretary tools `create_task`, `retry_dispatch`, `retry_attempt`, `send_worker_message`, `queue_worker_message`, `request_approval` или child-worker tools. `retry_attempt` существует только как server-internal operation с idempotency и не доступен Secretary или Client.

`remember` и `search_history` остаются будущими extensions. `user.md`, summary и recent history являются MVP context sources без сложной retrieval system.

## 11. Domain model

### 11.1. Person и Secretary identity

`Person` имеет одну Personal Conversation и одну Secretary identity. Перезапуск, смена модели или замена harness не создают новую identity.

### 11.2. Worker

Worker хранит:

- публичный `worker_ref`;
- title и user intent;
- Project reference;
- selected Node;
- selected HarnessInstance;
- Profile/policy snapshot;
- immutable execution binding;
- current Worker status;
- current Turn reference;
- last Result summary;
- created, updated и closed timestamps;
- archived flag.

Target Worker statuses:

```text
queued
starting
working
waiting_approval
needs_input
offline
idle
closed
```

Terminal state отдельного Turn не закрывает Worker. Follow-up возвращается к тому же `worker_ref` до explicit close.

После создания Worker его Project, Node, HarnessInstance и execution policy snapshot не меняются. Offline или draining Node не вызывают переназначение. Worker остаётся видимым и ждёт восстановления своей привязки; тот же intent на другом Node или harness является новым Worker.

### 11.3. Turn

Turn хранит:

- `turn_id`;
- Worker reference;
- user input;
- normalized intent;
- relevant context snapshot;
- created and updated timestamps;
- pending request id and kind, если Turn ждёт Approval или input;
- Attempts и current Attempt reference;
- один terminal Result после завершения Turn.

Target Turn states:

```text
queued
starting
active
waiting_approval
needs_input
succeeded
failed
canceled
interrupted
```

Один Worker может иметь только один active Turn. Retry создаёт новые Attempts внутри этого Turn. Внутренняя parallelism harness не превращается в несколько Secretary Workers.

### 11.4. Attempt

Attempt хранит только server lifecycle metadata:

- `attempt_id`;
- Worker и Turn reference;
- Node reference;
- HarnessInstance reference;
- state;
- AttemptOutcome, если Attempt завершён;
- timestamps;
- correlation id.

Attempt не хранит native runtime session id. Node локально связывает `worker_ref + turn_id + attempt_id` с session identifier harness.

### 11.5. Result

Result обязан содержать:

- Worker reference;
- Turn reference;
- status: `succeeded`, `failed`, `canceled` или `interrupted`;
- summary;
- created_at;
- optional artifact references;
- failure code, если status не `succeeded`;
- correlation id цепочки Turn.

Один Turn может иметь только один Result. Промежуточные AttemptOutcomes не попадают в Personal Conversation.

Result принимается idempotently, добавляется в Personal Conversation и доставляется во все нужные Client contexts.

### 11.6. Project

Project хранит:

- стабильное имя и id;
- description;
- mappings `Node -> path`;
- разрешённые HarnessInstances или harness kinds;
- default Node policy;
- workspace root и execution policy;
- optional repository metadata.

Project registry добавляется вручную. Server не сканирует весь диск и не создаёт registry из случайных директорий.

### 11.7. Node

Node хранит:

- identity и display name;
- status: `pairing`, `online`, `offline`, `draining`, `revoked`;
- capabilities;
- HarnessInstance inventory;
- active Attempt count и capacity;
- last heartbeat;
- workspace mappings;
- credential hash и enrollment metadata.

### 11.8. HarnessInstance

HarnessInstance является observed inventory record, например:

```text
id: macbook/claude
node: macbook
harness: claude_code
version: 1.x
authenticated: true
status: ready
capabilities: [shell, edit, cancel, approvals, steering]
models: [model IDs reported by adapter]
reasoning: [levels reported by adapter]
```

Другие примеры:

```text
macbook/codex
home-server/fx
home-server/opencode
```

Node сообщает фактическое состояние. `capabilities` HarnessInstance включают не только execution actions, но и доступные normalized activity types. Harness может не поддерживать `thinking_summary`, `tool_call`, `tool_result` или internal subagent events, и server не должен их выдумывать.

Server policy хранит только предпочтения и ограничения:

- default harness;
- preferred harness order;
- allowed harness kinds per Project;
- required capabilities;
- model or reasoning pin, если владелец задал его явно.

Model ID считается доступным только после подтверждения соответствующим adapter. Server не поддерживает универсальный список `fast`, `smart`, `cheap`.

### 11.9. Client

Client хранит:

- device identity;
- display name и platform;
- last seen;
- scopes;
- credential hash;
- status;
- created and revoked timestamps.

Client credentials никогда не используются как Node credentials.

### 11.10. Approval

Approval хранит:

- `request_id` для generic Worker response;
- Worker, Turn и Attempt;
- Node и Project;
- typed action summary;
- risk or policy category;
- request timestamp;
- expiration, если задана;
- state `pending`, `approved`, `denied`, `expired`;
- resolving Client;
- audit event.

### 11.11. Delivery and outbox

Для важных outbound сообщений server хранит delivery row:

- event or entry id;
- target Client или channel;
- idempotency key;
- state `pending`, `delivered`, `failed`;
- retry count и last error;
- timestamps.

## 12. Worker envelope

Worker получает не только последнюю строку пользователя:

```text
Worker envelope
├── worker_ref
├── turn_id
├── original_user_intent
├── normalized_goal
├── completion_contract
├── relevant Conversation context
├── user.md projection, если разрешено policy
├── Project
│   ├── name
│   ├── Node
│   ├── workspace
│   └── repository metadata
├── selected HarnessInstance
├── model and reasoning policy
├── allowed tools
└── constraints and approval policy
```

Worker или harness не знает адрес Secretary server и не получает callback capability. Terminal events идут от harness в Node adapter, а затем через authenticated Node connection в Secretary server. Worker не получает Client credentials, Node credentials, Secretary capability или чужие Workers.

Внутренний subagent harness может иметь свой context и session, но его lifecycle отображается как activity выбранного Worker. Server не создаёт для него `child_worker`, `parent_task` или отдельный Conversation route.

## 13. Node protocol

Node protocol является outbound WebSocket с typed messages, identity signatures и sequence numbers.

### 13.1. Handshake

Node отправляет:

- node identity;
- protocol version;
- capabilities;
- HarnessInstance inventory;
- workspace inventory;
- nonce signature;
- last acknowledged event sequence.

Server отвечает accepted identity, current policy snapshot и replay boundary.

### 13.2. Heartbeat

Heartbeat передаёт:

- online state;
- active Workers и Attempts;
- current capacity;
- HarnessInstance health;
- last processed command;
- optional resource summary.

Пропавший heartbeat переводит Node в `offline`, но не запускает новую работу автоматически.

### 13.3. Dispatch

Server отправляет Worker envelope и выбранную HarnessInstance. Node отвечает `accepted` только после:

1. проверки Project mapping;
2. подготовки workspace;
3. проверки harness authentication и version;
4. готовности native runtime session;
5. записи локального session mapping;
6. отправки readiness confirmation.

Server сохраняет Worker, Turn и Attempt metadata без native session id.

### 13.4. Activity

Node отправляет normalized activity:

- session started;
- thinking summary;
- assistant text delta;
- tool call;
- tool result;
- internal subagent started/completed;
- permission request с `request_id`;
- user input request с `request_id`;
- progress/status;
- attempt terminal с AttemptOutcome.

Каждый normalized activity type capability-dependent. Node публикует только события, которые реально поддержаны и наблюдены adapter-ом. Не поддерживаемый harness-ом тип не подменяется пустым или синтетическим событием.

Raw ACP или harness JSONL остаётся в per-Worker rotating logs и не отправляется всем Clients.

### 13.5. Commands

- Cancel является отдельной idempotent command.
- Steering направляется active Turn на safe boundary.
- Resume использует исходный Worker и Turn mapping на Node.
- `respond_worker` является одной generic command для ответа на запрос Worker:

```text
respond_worker {
  request_id,
  response
}
```

`response` может разрешить или отклонить Approval либо передать ответ на `needs_input`. Server направляет эту command выбранному Node, а Node передаёт её нужному harness runtime. Отдельные команды для каждого типа ответа не создаются.
- Если native runtime session отсутствует, Node возвращает explicit `runtime_session_unavailable`.

### 13.6. Node delivery and command dedupe

Node имеет durable local outbox для normalized activity и terminal events. Unsent events сохраняются до подтверждения server и отправляются после reconnect. Terminal event не удаляется только потому, что сеть оборвалась после завершения harness.

Каждая server→Node command содержит `command_id`. До запуска process Node атомарно durable-сохраняет claim по `command_id`, состояние обработки и outcome. Повтор той же команды возвращает сохранённый outcome и не запускает второй process или второй harness Attempt. После restart Node восстанавливает эту таблицу и сначала проверяет уже обработанные commands.

Для команды с состоянием `running` после сбоя Node сначала проверяет существующий process или доказанную local session mapping. При отсутствии доказательства он возвращает explicit interruption, а не запускает команду повторно.

## 14. Client contract

Все Clients используют общий server contract и не имеют собственной domain logic.

### 14.1. Основные операции

```text
send message
list conversation
subscribe conversation events
list workers
show worker
list turns for worker
observe worker activity
message worker
cancel worker
close worker
approve or deny request
list nodes
list projects
manage connected clients
```

### 14.2. Conversation synchronization

Client подключается с последним `entry_seq`. Server атомарно:

1. определяет replay boundary;
2. отдаёт пропущенные entries в порядке `seq`;
3. начинает live delivery после boundary.

Reconnect не должен дублировать entries, Turns или Workers.

### 14.3. Web Client

Web является visual control center:

- Personal Conversation;
- Workers и Turns;
- Node и HarnessInstance status;
- Projects;
- Approval requests;
- Worker observer;
- connected Client management.

Для Phase 4 используется owner session и private pairing. Email, Google login и hosted account system откладываются.

### 14.4. Telegram adapter

Telegram подключается из Web через одноразовый code или deep link. Пользователь не вводит server token в обычном чате.

Требования:

- allowlist одного владельца в MVP;
- idempotency по Telegram update id;
- polling в первом варианте, без публичного webhook;
- General chat для Secretary;
- отдельный Topic для каждого Worker;
- важные events в General и соответствующий Worker Topic;
- activity stream фильтруется до удобного текста;
- rich tool cards остаются в Web и Pi;
- Secretary text deltas и tool events агрегируются и throttle-ятся, а не создают отдельное Telegram message на каждый event;
- Worker Topic получает compact status и readable activity без raw event spam;
- Approval, failure, completion и offline Node не теряются;
- follow-up внутри Worker Topic направляется тому же Worker.

### 14.5. Pi Client

Pi подключается как Client, а не как Node.

Требования:

- browser pairing выдаёт Client credential;
- Pi не получает Node token;
- Pi видит одну Personal Conversation;
- Worker viewer открывается по `worker_ref`;
- activity, message, cancel и Approval используют server state;
- reconnect возвращает выбранный Worker без повторного запуска;
- extensions Pi остаются presentation/client capabilities, а не новым Secretary core.

## 15. Approval and execution policy

Approval идёт по цепочке:

```text
Worker or harness
      ↓
Execution Node
      ↓
Secretary server
      ↓
Web / Telegram / Pi Clients
```

Ответ владельца идёт обратно по цепочке `Client → Secretary server → Node.respond_worker → Worker/harness`. Approval и `needs_input` используют один typed response contract.

Secretary сам не вызывает `request_approval` для своих действий и не выполняет machine operation вместо Worker.

### 15.1. Interactive mode

Для удалённого или policy-restricted Node permission request останавливает Attempt и переводит Worker в `waiting_approval`. Владелец видит конкретное действие и выбирает Approve или Deny.

### 15.2. Trusted local mode

Для доверенного local Node можно разрешить auto-approval. Эта policy должна быть явной, видимой в Settings и записываться как audit event. Она не должна влиять на remote Node.

### 15.3. Resolution

Первый durable resolution побеждает. Повторный ответ возвращает уже установленное состояние и не повторяет действие.

### 15.4. Secret handling

- Client credentials, Node credentials, Secretary capabilities и channel tokens разделены;
- токены не попадают в Profiles, Worker envelope, Conversation или activity;
- Node token не используется Pi или Telegram;
- raw logs и diagnostic export редактируют secrets;
- revoked Client больше не может читать или менять state;
- revoked Node больше не может принимать Dispatch.

## 16. UX requirements

### 16.1. Secretary stream

Personal Conversation показывает полный live stream текущего Secretary turn: text deltas, thinking summaries, tool calls и tool results. Это не raw chain-of-thought, а наблюдаемые события Secretary runtime.

Worker в основном разговоре представлен compact status, acknowledgement и terminal Result. Его полный activity stream открывается отдельно в Worker observer.

```text
Secretary
● Working...

→ list_workers
← frontend · MacBook

→ spawn_worker
  Claude Code · MacBook
```

### 16.2. Worker как главный экранный объект

Основной список показывает Workers, а не Tasks:

```text
Secretary
├── Fix mobile header       working   MacBook · Claude Code
├── Research trip           idle      Home server · fx
└── Build game              blocked   MacBook · Codex
```

Turn history открывается внутри Worker. Attempt details доступны в diagnostic view.

### 16.3. Message states

Пользователь должен различать:

```text
Saved
Accepted
Queued
Working
Waiting for approval
Needs input
Succeeded
Failed
Canceled
Interrupted
```

`Sending...` является коротким transport state. `Message received` появляется только после server acknowledgement. Spinner не является доказательством работы модели без durable state или live event.

### 16.4. Worker card

Worker card показывает:

- title;
- Worker status;
- current Turn status;
- Node;
- Project;
- HarnessInstance;
- last activity time;
- compact progress summary;
- actions Message, Stop, Approve, Follow-up и Close.

### 16.5. Notifications

По умолчанию доставляются только важные events:

- Worker accepted;
- Worker started;
- Approval required;
- user input required;
- Worker finished;
- Worker failed;
- Worker canceled;
- Node offline, если он блокирует Worker.

`Read file`, `Running tests` и подобные события остаются в Worker observer.

### 16.6. Source display

Source channel сохраняется в metadata, но в обычной Conversation показывается только когда это помогает понять контекст. Control Room и audit view показывают полный source, Client, Node и timestamps.

### 16.7. No fake progress

Если server не знает, что Attempt active, UI показывает состояние `unknown`, `queued`, `blocked` или `offline`, а не бесконечный `working` spinner.

## 17. API surface

Названия ниже являются target contract. Они могут быть адаптированы к существующим `/v1/*` endpoint-ам без изменения семантики.

### 17.1. Client authentication

```text
POST /v1/clients/pair
POST /v1/clients/{id}/revoke
GET  /v1/clients
```

Pairing создаёт pending Client, который владелец подтверждает через уже доверенный Web Client.

### 17.2. Conversation

```text
POST /v1/messages
GET  /v1/conversation?after_seq=N
GET  /v1/conversation/ws?after_seq=N
```

`POST /v1/messages` возвращает:

```json
{
  "entry": {},
  "message_id": "...",
  "entry_seq": 42,
  "state": "saved",
  "worker_ref": "",
  "turn_id": "",
  "duplicate": false
}
```

Если Worker уже создан до ответа, response содержит `worker_ref` и `turn_id`. Если нет, последующий durable event связывает сообщение с Worker.

User context API:

```text
GET /v1/user
PUT /v1/user
```

`PUT /v1/user` валидирует и атомарно сохраняет external `user.md`, возвращая новую revision. Следующий Secretary turn обязан использовать эту revision в context reconstruction.

### 17.3. Workers

```text
GET  /v1/workers
GET  /v1/workers/{ref}
GET  /v1/workers/{ref}/turns
GET  /v1/workers/{ref}/activity?after_seq=N
GET  /v1/workers/{ref}/activity/ws?after_seq=N
POST /v1/workers/{ref}/message
POST /v1/workers/{ref}/cancel
POST /v1/workers/{ref}/close
```

`message` выбирает steer, Follow-up или resume по текущему Worker state. Клиент не вызывает отдельный `retry_dispatch`.

### 17.4. Approvals

```text
GET  /v1/approvals
POST /v1/approvals/{id}/approve
POST /v1/approvals/{id}/deny
```

Ответ на уже resolved Approval не запускает повторное действие.

### 17.5. Projects и Nodes

```text
GET  /v1/projects
POST /v1/projects
PUT  /v1/projects/{id}
DELETE /v1/projects/{id}

GET  /v1/nodes
POST /v1/nodes/pair
POST /v1/nodes/{id}/drain
POST /v1/nodes/{id}/revoke
```

### 17.6. Node connection

```text
GET /v1/node/ws
```

Node protocol использует отдельный authentication mode и не принимает Web Client credentials.

### 17.7. Legacy Task endpoints

Phase 3 `/v1/tasks` и `task_id` могут временно существовать только для migration и diagnostic compatibility. Новые Clients, Secretary tools и Phase 4 UI не используют их.

## 18. Event contract

Нормализованные events содержат:

- `event_id`;
- monotonic sequence;
- event type;
- aggregate type и id;
- source;
- timestamp;
- structured payload;
- correlation id;
- causation id.

Минимальные types:

```text
message.saved
message.accepted
message.processing_failed

secretary.turn.queued
secretary.turn.started
secretary.text_delta
secretary.thinking_summary
secretary.tool_call
secretary.tool_result
secretary.turn.finished

action.requested
action.accepted
action.failed

worker.spawned
worker.queued
worker.started
worker.steered
worker.cancel_requested
worker.closed
worker.offline

turn.created
turn.started
turn.waiting_approval
turn.needs_input
turn.succeeded
turn.failed
turn.canceled
turn.interrupted

attempt.started
attempt.activity
attempt.outcome_recorded

approval.requested
approval.approved
approval.denied
approval.expired

node.paired
node.online
node.offline
node.revoked
harness.discovered
harness.health_changed

client.paired
client.connected
client.revoked

result.accepted
result.delivered
result.delivery_failed
```

`attempt.outcome_recorded` содержит status и error конкретного Attempt. `result.accepted` публикуется только один раз для финального Result всего Turn.

Internal subagent events имеют Worker activity scope и не создают отдельный Secretary aggregate:

```text
worker.activity.subagent_started
worker.activity.subagent_progress
worker.activity.subagent_completed
```

Raw harness events остаются в per-Worker rotating files и связываются с normalized events через correlation metadata. Набор activity events зависит от capabilities конкретного HarnessInstance.

## 19. Reliability and recovery

### 19.1. Inbound message

Server сначала сохраняет Conversation entry и dedupe key. Если Secretary turn уже active, обычное сообщение получает durable queue position и событие `secretary.turn.queued`. Если Secretary runtime недоступен, entry остаётся durable и получает явный processing failure или pending state. Пользователь видит, что произошло, и может повторить действие.

### 19.2. Worker creation

`spawn_worker` считается accepted только после создания Worker, Turn и Attempt metadata и подтверждения readiness выбранной HarnessInstance на Node. Если подходящего Node нет, Worker остаётся `queued`.

### 19.3. AttemptOutcome and Result

Повторный terminal event для одного Attempt idempotent по `attempt_id` и возвращает тот же AttemptOutcome. `final` outcome закрывает Turn и создаёт Result; `retryable` outcome оставляет Turn открытым для internal `retry_attempt`. Внутренняя `retry_attempt` operation разрешена только после terminal `retryable` outcome и сама idempotent. Финальный Result idempotent по `(worker_ref, turn_id)`: повторная доставка не создаёт новый Result или Conversation entry. Intermediate AttemptOutcomes не публикуются как Results.

### 19.4. Server restart

Server восстанавливает identity, Conversation, Workers, Turns, Approvals, outbox и event sequence. Native Secretary runtime session может быть пересоздана через context reconstruction.

Active Attempts, состояние которых нельзя доказать, получают AttemptOutcome `interrupted`. Server не запускает их повторно автоматически. Если новый Attempt не создаётся по явной retry policy, Turn получает `interrupted`, после чего server создаёт его единственный Result.

### 19.5. Node restart

Node не повторяет active Attempt автоматически и не меняет Worker binding. Он отправляет inventory и local session mappings. Server либо подтверждает восстановление исходного Attempt на том же Node и HarnessInstance, либо получает explicit `runtime_session_unavailable` и сохраняет Worker на исходной привязке.

### 19.6. Outbox

Важные события повторяются до bounded retry limit. После исчерпания попыток Delivery остаётся `failed` и доступна для ручного retry. Новый Client получает пропущенные события через replay.

### 19.7. Capacity

Лимит применяется к active Attempts на Node и глобально. Idle Workers не занимают execution slot. Внутренние subagents учитываются policy конкретного harness и Node, но не превращаются в child Workers Secretary.

## 20. Observability

### 20.1. User view

Пользователь видит:

- что принято;
- какой Worker выполняет работу;
- на каком Node и в каком Project;
- что сейчас требуется;
- чем закончился Turn;
- какое действие доступно дальше.

### 20.2. Control Room

Debug-only Control Room показывает:

- Nodes и heartbeat;
- Clients и revoke;
- Projects и path mappings;
- HarnessInstances и observed capabilities;
- Workers и Turns;
- Attempt transitions;
- Approval audit;
- normalized Events;
- raw harness logs;
- Profile and configuration versions;
- export diagnostics.

### 20.3. Privacy

Raw model reasoning не является пользовательским activity. UI может показывать короткий thinking summary, но не должен выдавать скрытое chain-of-thought.

## 21. Configuration and policy

Config хранит policy, а не выдуманный глобальный каталог моделей:

```toml
[server]
listen = "127.0.0.1:8081"

[secretary]
harness = "fx"
model = "gpt-5.6-luna"
reasoning = "default"

[worker_policy]
default_harness = "fx"
preferred_harnesses = ["fx", "claude_code", "codex"]
active_attempts = 4

[security]
require_client_pairing = true
require_node_pairing = true
trusted_local_auto_approve = true

[notifications]
important_only = true

[telegram]
enabled = false
polling = true
```

`secretary.harness`, `secretary.model` и `secretary.reasoning` выбирают runtime самого Secretary. `worker_policy.default_harness` и `preferred_harnesses` выбирают Worker. Эти policy независимы, даже если сейчас обе используют `fx`.

Значения `model` являются policy pins, а не глобальным каталогом. Для Secretary выбранный runtime adapter подтверждает поддерживаемый model ID; для Worker список моделей и reasoning levels для конкретного Node приходит от HarnessInstance adapter. Если заданный model ID отсутствует в observed inventory, Dispatch завершается explicit error.

External Profiles остаются Markdown files. Profile reload атомарен. Уже созданные Workers сохраняют policy snapshot и не меняются от reload.

## 22. Migration from Phase 3

Миграция не должна удалять или пересоздавать пользовательское состояние.

1. Существующие Person, Secretary identity и Personal Conversation сохраняются.
2. Каждый Phase 3 Task с Worker binding преобразуется в Worker + Turn history.
3. Legacy Task row может остаться read-only для migration и diagnostics, но не используется новым API.
4. `task_id` исчезает из новых Conversation, API, UI и Secretary tools.
5. `runtime_session_id` не переносится в server Worker record. Если Node может доказать session mapping, он сохраняет его локально. Иначе Attempt получает `interrupted`.
6. Исторические child Task records сохраняются только как legacy data и не создаются снова.
7. `child_worker` Profile перестаёт быть частью нового Secretary core.
8. `fast`, `smart` и `cheap` aliases мигрируют в явные model pins или adapter defaults. Они не появляются в Phase 4 contract.
9. Текущий local Node получает Node identity и HarnessInstance inventory.
10. Новые records `clients`, `nodes`, `projects`, `harness_instances`, `approvals` и `deliveries` создаются миграцией.
11. Перед миграцией `secretary.db` автоматически архивируется.
12. Невалидная новая конфигурация не заменяет активный snapshot.
13. `phase-1/` остаётся сохранённым Pi source snapshot и не включается автоматически в Go build.
14. Команда `sex setup` сохраняется без переименования.

## 23. Security boundary

Phase 4 phone access и remote Nodes увеличивают поверхность атаки. Обязательные правила:

- default listener остаётся loopback;
- remote access идёт через private network, например Tailscale;
- bootstrap URL не публикуется и не хранится в Git;
- Telegram allowlist ограничивает владельца;
- каждый Client проходит pairing и имеет отдельный revoke;
- каждый Node проходит отдельное pairing и имеет отдельный revoke;
- Secretary capability не используется внешним Client;
- Node token не используется Pi или Telegram;
- channel bot token не попадает в Worker environment;
- Project path и workspace проверяются Node policy;
- full access явно показывается при setup и в Settings;
- interactive Approval доступен для policy-restricted Nodes;
- raw logs и diagnostic export редактируют secrets;
- server не принимает unauthenticated control requests.

Sandbox и multi-user isolation остаются будущими security boundaries. В Phase 4 trusted full-access Node считается полностью доверенным компьютером владельца.

## 24. Acceptance gate

Phase 4 считается готовой после прохождения следующего сценария на чистой конфигурации и после restart:

1. Запущен Secretary server.
2. Подключены MacBook Node и home server Node.
3. Каждый Node сообщает несколько HarnessInstances.
4. Вручную зарегистрирован Project с разными path mappings.
5. Web и Telegram видят одну Personal Conversation.
6. Владелец отправляет задачу без указания harness.
7. Без override Secretary выбирает `fx` как default Worker harness, независимо от собственного Secretary harness.
8. Явный выбор Claude Code направляет Worker в Claude Code на MacBook.
9. Явный model ID, которого нет в selected HarnessInstance inventory, даёт visible error и не переключается на `fx`.
10. Создаётся один Worker и первый Turn. Отдельный Task entity не появляется в Client API.
11. Пользователь меняет external `user.md`, и следующий Secretary turn видит новую preference через context reconstruction.
12. Пользователь видит acknowledgement до completion.
13. Personal Conversation получает live Secretary events `secretary.turn.started`, text deltas, tool calls, tool results и `secretary.turn.finished`.
14. Во время active Secretary turn второе обычное сообщение сохраняется в durable ordered queue и обрабатывается после текущего turn. Workers продолжают работать параллельно.
15. Worker показывает только те normalized activity types, которые объявлены и реально поддержаны его HarnessInstance.
16. Worker запрашивает Approval через Node и harness.
17. Approval и `needs_input` разрешаются из другого Client через generic `respond_worker { request_id, response }`.
18. Для одного Turn выполняются несколько Attempts: промежуточные AttemptOutcomes сохраняются для диагностики, но в Personal Conversation появляется только один Result по финальному состоянию Turn.
19. Terminal Worker Result попадает напрямую в Personal Conversation и Worker Topic без дополнительного Secretary model turn.
20. Следующий Secretary turn получает этот Result через `unseen Worker Results` в context reconstruction.
21. Владелец отправляет Follow-up, и тот же Worker получает новый Turn.
22. Владелец отправляет явную задачу для Codex на home server.
23. Server перезапускается во время active Attempt.
24. Node теряет сеть после завершения harness, но до подтверждения terminal event; после reconnect Node доставляет buffered activity и AttemptOutcome.
25. Повторная server→Node command с тем же `command_id` возвращает сохранённый outcome и не запускает второй process или Attempt.
26. Attempt получает корректное `interrupted` или восстанавливает доказанную native session на Node.
27. Владелец через `message_worker` выбирает resume или создаёт Follow-up.
28. Idle Worker не расходует active Attempt capacity.
29. Worker остаётся привязан к исходному Node и HarnessInstance при offline или draining Node. Выполнение того же intent на другом Node создаётся как новый Worker.
30. Internal subagent harness отображается как activity Worker, без child Worker в server state.
31. Внутренняя `retry_attempt` создаёт новый Attempt только после terminal `retryable` AttemptOutcome; uncertain execution не retry-ится автоматически.
32. Повторные inbound, action, terminal event и Result events не создают дубликаты.
33. Revoked Client больше не может читать или менять state.
34. Revoked Node больше не может принимать Dispatch.
35. Offline Node оставляет Worker видимым и понятным.
36. Telegram Topic соответствует правильному Worker и не получает отдельное сообщение на каждый Secretary delta или raw Worker event.
37. `go test ./...`, `go test -race ./...`, `go vet ./...` и frontend checks проходят.
38. Реальные acceptance tests выполняются для `fx`, Claude Code и Codex. OpenCode проверяется отдельно, если заявлен установленным compatibility target.

Pi Client считается принятым после прохождения общего Client API, pairing, Conversation replay, Worker observe, message, cancel и Approval flow.

## 25. Product language

Пользовательское описание:

> One AI you talk to. It sends the right agent to the right computer and keeps track of everything.

Более точное внутреннее описание:

> Secretary is a persistent personal AI above computers and agent harnesses. It owns intent, assigns Workers to Projects and Nodes, and delivers Results across trusted Clients.

Не использовать как основной pitch:

- "multi-agent framework";
- "orchestrator for orchestrators";
- "remote coding agent";
- "chatbot with tools".

Эти выражения могут описывать внутреннюю механику, но не пользовательскую пользу.

## 26. Definition of done for the spec

Спек считается готовым к разбиению на implementation tickets, когда подтверждены:

- product statement;
- Worker-first domain model;
- AttemptOutcome на каждый Attempt и ровно один Result на Turn;
- отсутствие child Workers в Secretary core;
- отсутствие native runtime session id в server Worker record;
- immutable Worker binding к Node и HarnessInstance;
- context reconstruction Secretary identity и durable `user.md` revision;
- полный Secretary stream, один активный Secretary turn и durable input queue;
- HarnessInstance inventory, model policy и capability-dependent activity;
- независимые Secretary и Worker harness policies;
- internal retry rule для terminal `retryable` AttemptOutcome без public retry tool;
- Node `respond_worker`, durable event outbox и command dedupe;
- обязательные MVP harnesses: `fx`, Claude Code и Codex;
- Web и Telegram MVP;
- Pi Client после стабилизации общего API;
- Projects и простой `user.md`;
- Worker-originated Approval flow;
- delivery, idempotency и recovery semantics;
- security boundary;
- acceptance scenario;
- fixed decisions из раздела 8.

Этот документ намеренно не содержит implementation tickets. Tickets вынесены в отдельные файлы `.scratch/phase-4/issues/` и должны следовать только подтверждённой модели. Новые функции не добавляются только потому, что они есть в Hermes, OpenClaw или другом агентском продукте.
