# Issue #63: ответы Worker пользователю

## Краткое резюме

В текущем коде terminal `Result` Worker уже становится `Conversation entry`, попадает в контекст следующего Secretary turn и доставляется в Telegram-тему Worker и общий чат. В Web та же запись скрывалась из хронологической ленты, когда карточка Worker показывала совпадающее summary. Исправление #63 оставляет Result в общей последовательности и убирает его повтор из компактной карточки.

Отдельный путь — обычный текст Worker (`assistant_text_delta`): он сохраняется только как `attempt.activity`, виден в Web Worker observer и не доставляется Telegram. Issue #63 после исследования ограничен terminal `Result`; Activity до завершения Attempt не публикуется как ответ пользователю, поскольку это может быть progress и привести к дубликату с Result.

Если потребуется показывать Worker reply до завершения Attempt, нужен отдельный типизированный контракт с явным пользовательским назначением и серверной input identity. Нельзя считать каждый текстовый Activity ответом пользователю или переиспользовать `Result`, который по glossary является terminal report одной Attempt.

Исследование выполнено в worktree `/Users/beruseruko/projects/secretary-v2/.worktrees/issue-63-worker-replies`, ветка `codex/issue-63-worker-replies`, исходный код не менялся. Тесты и проверки описаны ниже. В этой ревизии нет каталога `docs/adr/`, применимых ADR нет.

## Scope и терминология

[Issue #63](https://github.com/GitHubFoxy/secretary/issues/63) требует сохранять Worker-ответ в общей переписке, давать его Secretary и доставлять через интерфейсы. В самом issue смешаны два возможных контракта: terminal `Result` и ответ Worker до завершения Attempt. Текущая реализация уже поддерживает первый, но не второй.

В [Issue #61](https://github.com/GitHubFoxy/secretary/issues/61) речь о противоположном направлении: сообщение Secretary → Worker должно появляться в Telegram-теме только после доставки Worker, а queued message не должен выглядеть как полученный. Исправление этого входного пути не доставит Worker reply пользователю. Комментарий к #61 также фиксирует, что web-only отображение `turns[].input` не покрывает требование про Telegram-тему.

Термины взяты из [`GLOSSARY.md`](../../GLOSSARY.md): `Personal Conversation`, `Conversation entry`, `Worker`, `Result`, `Worker activity`, `Steering message`, `Queued message`, `Worker origin link`. Runtime-контракт отдельно указывает, что `Result` является terminal report, сохраняется verbatim в Origin Conversation и сам по себе не запускает Secretary turn: [`runtime-contracts.md`, раздел Attempt, Result и закрытие Task](../architecture/runtime-contracts.md#attempt-result-и-закрытие-task).

## Трассировка прямого сообщения пользователя Worker

### Входное сообщение

Telegram различает общий чат и Worker topic. В Worker topic `Adapter.deliverTextUpdate` вызывает `ServerClient.SendWorkerMessage` с `WorkerRef`, текстом и внешним Telegram update ID. [`HTTPServerClient.SendWorkerMessage`](../../internal/telegram/http.go#L304) отправляет `POST /v1/workers/{ref}/message`; внешний ID превращается только в `Idempotency-Key`, в теле остаются `text` и необязательный `request_id`. Вход не проходит через `POST /v1/messages` и `Store.AppendInbound`. См. [`deliverTextUpdate`](../../internal/telegram/adapter.go#L615).

Web Worker observer вызывает тот же `MessageWorker` через `/v1/workers/{ref}/message` или `/follow-up`; обработчик передаёт `text`, `request_id` и idempotency key, но не создаёт входную `Conversation entry`: [`phase4WorkerRoute`](../../internal/webapi/observer.go#L405), [`WorkerService.MessageWorker`](../../internal/ctl/worker_lifecycle.go#L615). Это отличается от сообщения Secretary: `Server.message` вызывает `Store.AppendInbound`, а тот создаёт `EntryUser`, общий `conversation.entry` event и уведомляет Web subscriber: [`Server.message`](../../internal/webapi/server.go#L336), [`AppendInbound`](../../internal/core/store.go#L840).

Дальше `WorkerService.MessageWorker` выбирает lifecycle path:

- При `/q` сервер создаёт `Queued message`, сохраняет `worker.message.queued` и связанные события состояний. До promotion в Attempt текст находится в очереди, а не в общей переписке. [`MessageWorker`](../../internal/ctl/worker_lifecycle.go#L628), [`EnqueueWorkerMessage`](../../internal/core/worker_queue.go#L57).
- Для активного Worker сервер вызывает runtime `Steer`. Код не создаёт `Conversation entry` и не сохраняет текст как отдельный серверный input; command record содержит lifecycle state, а не текст Steering message. [`MessageWorker`](../../internal/ctl/worker_lifecycle.go#L826), схема [`phase4_worker_commands`](../../internal/core/phase4_store.go#L57).
- Для Worker, ожидающего `Approval`/input, ответ передаётся runtime и фиксируется как resolution конкретной Approval. Это не отдельная Conversation entry. [`MessageWorker`](../../internal/ctl/worker_lifecycle.go#L680), [`CommitApprovalResolution`](../../internal/core/approval.go#L160).
- Для idle/queued/offline Worker создаётся `Turn` с `turn.Input`, затем `turn.created` и `attempt.started`. `CreateTurn` не вызывает `appendEntry`/`notifyEntry`; `turn.created` имеет `AggregateType="turn"` и не содержит `WorkerRef`. [`MessageWorker`](../../internal/ctl/worker_lifecycle.go#L875), [`Store.createTurn`](../../internal/core/phase4_store.go#L732).

Итого прямой ввод пользователя либо находится только в `Turn.Input`, Approval, либо передан runtime как Steering. У общего потока нет унифицированной server-issued identity этого пользовательского ввода. `Idempotency-Key` помогает повторить transport mutation, но не заменяет input identity для связывания ответа с конкретным вводом.

### Текстовый ответ Worker во время Attempt

Harness выдаёт обычный текст как `ActivityText`; Execution node нормализует его в `ActivityAssistantTextDelta`. Store сохраняет его как `attempt.activity` с `WorkerRef`, `TurnID` и `AttemptID`, но не создаёт `Conversation entry`: [`RecordNodeActivity`](../../internal/core/phase4_store.go#L2222). Событие поэтому доступно через Worker-scoped event stream.

Web API отдаёт такие events из `/v1/workers/{ref}/activity` и WebSocket activity stream: [`workerActivityReplayJSON` и `workerEventsWithContext`](../../internal/webapi/observer.go#L499). Клиент подключается к этому stream при открытии Worker observer, а `WorkerActivity` рендерит текст там, отдельно от Personal Conversation: [`openWorker`/`connectActivity`](../../web/src/App.svelte#L317), [`WorkerActivity.svelte`](../../web/src/WorkerActivity.svelte#L1). Secretary context не читает этот event stream.

Для Telegram `telegramEvent` преобразует `attempt.activity` с типом `assistant_text_delta` в `worker.activity`, не в пользовательское сообщение: [`telegramEvent`](../../cmd/secretaryd/telegram.go#L226). `importantWorkerEvent` не включает `worker.activity`, поэтому `Adapter.handleEvent` ничего не отправляет: [`importantWorkerEvent`](../../internal/telegram/adapter.go#L1715). Это конкретное место, где текстовый ответ теряет общий канал доставки.

### Terminal Result Worker

`ExecutionNode.watchSession` читает `Session.Result()`, строит terminal `AttemptOutcomeEnvelope` с неизменяемой связью Node/HarnessInstance/Worker/Turn/Attempt и отдаёт его в Node event sink: [`watchSession`](../../internal/node/execution_node.go#L672). `NewStoreEventSink` принимает `attempt.outcome` и вызывает `Store.RecordNodeAttemptOutcome`; Store сверяет binding и передаёт результат в `RecordAttemptOutcome`: [`NewStoreEventSinkWithTrustedLocalApproval`](../../internal/node/server_event_sink.go#L50), [`RecordNodeAttemptOutcome`](../../internal/core/phase4_store.go#L2173).

`Store.RecordAttemptOutcome` в одной транзакции сохраняет `AttemptOutcome` и `Phase4Result`, добавляет `attempt.outcome_recorded` и `result.accepted`, обновляет Turn и Worker, затем создаёт `EntryWorkerResult` с `WorkerRef`, `TurnID`, `ResultID`. После commit entry передаётся через `notifyEntry`: [`RecordAttemptOutcome`](../../internal/core/phase4_store.go#L1181).

`appendEntryWithIdentity` назначает общий `seq`, сохраняет `conversation_entries`, пишет `conversation.entry` event и delivery identity. [`appendEntryWithIdentity`](../../internal/core/store.go#L1284). В Web `SetEntryObserver` подключает `publishEntry`; `/v1/conversation` читает те же entries, а WebSocket доставляет новые записи по общей последовательности: [`webapi.New`](../../internal/webapi/server.go#L142), [`Server.conversation`](../../internal/webapi/server.go#L273), [`publishEntry`](../../internal/webapi/server.go#L511).

Для Telegram `bridgeTelegramEventsOnce` читает durable events по cursor; `result.accepted` переводится в `worker.completed` с `TerminalIdentity`. [`bridgeTelegramEventsOnce` и `telegramEvent`](../../cmd/secretaryd/telegram.go#L134). Telegram adapter отправляет terminal body в соответствующую Worker topic и дублирует в owner general chat; send identity и event cursor защищают повторную доставку: [`Adapter.handleEvent`](../../internal/telegram/adapter.go#L898). Есть регрессия для зеркалирования в оба Telegram назначения и дедупликации: [`TestTerminalResultMirrorsToGeneral`](../../internal/telegram/adapter_test.go#L221).

Следующий Secretary context включает `RecentEntries` и `UnseenWorkerResults`; последние извлекаются из всех Results Personal Conversation с учётом claim/seen state. [`ReconstructSecretaryContext`](../../internal/core/secretary_context.go#L105), [`unseenWorkerResultsQuery`](../../internal/core/secretary_context.go#L299). `Result` не запускает Secretary автоматически: это прямо проверяется в [`TestTerminalWorkerResultIsImmediateAndUnseenForNextSecretaryContext`](../../internal/core/secretary_context_test.go#L803). Когда пользователь задаёт новый вопрос Secretary, результат доступен модели для адресованного ответа.

Для действий, вызванных Secretary, `Worker origin link` связывает конкретную пару Secretary turn/input с Worker Turn и его canonical Result. Эта связка создаётся через `secretary_worker_command_origins` и материализуется для принятого Result: [`bindSecretaryWorkerCommandOriginTx`](../../internal/core/secretary_reply.go#L89), [`recordSecretaryOriginResultsForWorkerTurnTx`](../../internal/core/secretary_reply.go#L464). Прямое сообщение пользователя Worker не имеет такой Secretary origin identity.

## Root cause

Root cause не в том, что Store или адаптеры не умеют доставлять terminal `Result`. Этот путь уже реализован и проверен. Пробел в контракте между Worker runtime Activity и пользовательской перепиской:

1. Worker assistant text сохраняется как ephemeral `Worker activity`, а не как `Result` или отдельная `Conversation entry`.
2. Web показывает activity только в Worker observer. Главная Personal Conversation его не читает.
3. Telegram bridge намеренно не отправляет generic `worker.activity`.
4. Secretary context потребляет Conversation entries и terminal `UnseenWorkerResults`, но не Worker activity.
5. Входные сообщения Worker не имеют общего server-issued input identity. Поэтому нельзя надёжно определить, какой ответ относится к конкретному Steering message, особенно если несколько сообщений направлены в одну активную Attempt.

Если под «ответом Worker» подразумевается terminal report, этот root cause к нему не относится: Result уже публикуется. У Web есть отдельный presentation нюанс: `visibleConversationEntries` скрывает `worker_result`, когда карточка Worker отображает тот же Result; compact card показывает только актуальную summary, поэтому запись может не быть видна на хронологическом месте в Conversation, хотя Result присутствует в UI. [`visibleConversationEntries`](../../web/src/ui-model.js#L190), рендер карточки и ленты [`App.svelte`](../../web/src/App.svelte#L510).

## Минимальный исправный дизайн

Сначала зафиксировать в Issue #63, требуется ли terminal `Result` или ответ до завершения Attempt. Для первого варианта не нужен новый серверный контракт: сохранить существующий путь и добавить end-to-end проверку, при необходимости изменить только представление Web, чтобы Result был виден в хронологии.

Если требуется прямой ответ до завершения Attempt:

1. Добавить явный Worker-to-user reply contract, например server-owned `reply_to_user`, а не публиковать каждый `assistant_text_delta`. Нативные Activity могут быть commentary/progress; автоматическая пересылка рискует раскрыть reasoning или технический вывод.
2. Идентифицировать адресата серверно: `WorkerRef`, `TurnID`/`AttemptID` и server-issued identity пользовательского ввода. Для прямых Worker сообщений эта identity сейчас отсутствует; `RequestID` применим только к pending Approval, а transport idempotency key не является адресацией.
3. Store должен проверить Worker binding и input/Attempt association, идемпотентно сохранить ровно один Worker reply и добавить отдельный тип Conversation entry. Не переиспользовать `EntryWorkerResult`/`Phase4Result` для промежуточного ответа, поскольку `Result` по контракту terminal и уникален на Attempt.
4. В одной транзакции записать entry и durable `conversation.entry` event с canonical sequence. Web уже читает общий entry stream; Telegram bridge должен явно маппить новый entry kind в Worker topic и owner general chat с детерминированной delivery identity. Сейчас Telegram игнорирует `conversation.entry`, кроме Secretary entries, поэтому новое событие нельзя считать автоматически подключённым.
5. Secretary должен получать эту запись в хронологическом контексте, а terminal Result остаться отдельной записью. Не создавать автоматический Secretary turn и не дублировать один текст как Worker reply и terminal Result без отдельного контракта.

Такой путь сохраняет Secretary server как authority и использует общую Personal Conversation. Новая запись должна иметь собственную kind/identity и потребует обновления glossary и runtime contracts. Текущий `Addressed reply v1` описывает reply от Secretary и не включает автоматически Worker reply.

## Проверки

### Уже существующее покрытие

- `TestWorkerServiceMessageLifecycleMatrix` покрывает active Steering, needs-input response, idle Follow-up и resume, но не проверяет сохранение direct input в общей Conversation: [`worker_lifecycle_test.go`](../../internal/ctl/worker_lifecycle_test.go#L185).
- `TestTerminalWorkerResultIsImmediateAndUnseenForNextSecretaryContext` доказывает единственную `EntryWorkerResult`, доступность следующему Secretary context и отсутствие автоматического Secretary turn: [`secretary_context_test.go`](../../internal/core/secretary_context_test.go#L803).
- `TestTerminalResultMirrorsToGeneral` проверяет topic + general delivery и replay без дублей: [`adapter_test.go`](../../internal/telegram/adapter_test.go#L221).
- `TestTelegramEventBridgeMapsTerminalOutcomeOnceAndSkipsRetryable` проверяет mapping terminal events и общий `TerminalIdentity`: [`telegram_test.go`](../../cmd/secretaryd/telegram_test.go#L565).
- Web уже проверяет replay boundary для Conversation stream: [`TestConversationReplayBoundaryDeduplicatesDelayedNotify`](../../internal/webapi/client_api_test.go#L355).

Эти четыре набора точечных Go-проверок выполнены в заданном worktree и прошли:

```text
go test ./internal/core -run 'TestTerminalWorkerResultIsImmediateAndUnseenForNextSecretaryContext|TestTerminalConversationEntriesCarryResultIdentityPerTurn' -count=1
go test ./internal/ctl -run 'TestWorkerServiceMessageLifecycleMatrix|TestWorkerServiceSteeringDedupeKeepsDistinctMessages' -count=1
go test ./internal/telegram -run 'TestTerminalResultMirrorsToGeneral|TestGeneralMirrorFollowUpTurnPreservesSameRule' -count=1
go test ./cmd/secretaryd ./internal/webapi -run 'TestTelegramEventBridgeMapsTerminalOutcomeOnceAndSkipsRetryable|TestConversationReplayBoundaryDeduplicatesDelayedNotify' -count=1
```

Эти тесты подтверждают terminal Result path, но не проверяют отсутствующий пользовательский Worker reply из Activity.

### Регрессии для реализации

- Store: один явный Worker reply создаёт одну entry с правильными `WorkerRef`, `TurnID`, input identity и `seq`; повтор с той же identity возвращает прежнюю entry, другой текст конфликтует; unrelated Worker/Attempt не может присвоить reply.
- Lifecycle/API: проверить одинаковый контракт при direct message через Telegram endpoint и Web observer; отдельно idle Turn, active Steering, Approval response, `/q` pending/delivered и несколько прямых вводов в одной Attempt.
- Secretary context: новая Conversation entry видна следующему Secretary turn в правильном порядке; модель может процитировать ответ; повторный fetch или повтор операции не дублирует его; terminal Result не переотправляется.
- Telegram bridge: reply доставлен в topic правильного `WorkerRef` и owner general; replay/restart использует прежний identity; delivery error не двигает cursor; один ответ не отправляется дважды вместе с terminal Result.
- Web: `/v1/conversation` history и WebSocket возвращают ту же entry и sequence; основная Conversation отображает reply хронологически, Worker observer остаётся отдельным; sanitizer не раскрывает внутренние поля.
- Безопасность: тесты на reasoning/analysis, tool output и untrusted Worker text должны доказывать, что только явный пользовательский reply уходит в Personal Conversation. Raw `ActivityAssistantTextDelta` автоматически не публиковать.

## Соседние риски и пересечение с #61

- **Не смешивать направления.** #61 про Secretary → Worker topic и видимость только уже доставленного Follow-up. #63 про Worker → пользовательские интерфейсы. Не маппить `turn.created` в Telegram как Worker reply: у события нет `WorkerRef`, а `turn.input` не доказывает доставку runtime.
- **Queue semantics.** `worker.message.queued` доказывает только постановку в очередь. Оно не является сообщением, уже полученным Worker, и не должно отображаться как direct reply или delivered input.
- **Result uniqueness.** Уникальность `phase4_results(attempt_id)` и Result terminal semantics запрещают использовать один Result для нескольких частичных ответов. Также Telegram видит `attempt.outcome_recorded` и `result.accepted` с общим terminal identity; новый reply identity должен быть отдельным, чтобы не ломать дедупликацию.
- **Input correlation.** Для `WorkerWorking` несколько Steering messages адресуются одной Attempt. Привязка результата только по Turn/Attempt не доказывает, на какое пользовательское сообщение Worker ответил. Не использовать совпадение текста, ACP event ID или adapter-generated key вместо server-issued input identity.
- **Web chronology.** Сейчас `worker_result` может быть скрыт в ленте, если он представлен Worker card. Если критерий #63 требует запись именно в хронологическом Conversation, нужно изменить UI assertion и не считать карточку достаточным отображением.
- **Legacy и внешние Profiles.** Изменение Worker runtime tool contract нужно согласовать с native Codex/Claude/OpenCode harnesses и актуальными внешними Worker profiles. Внешние Profiles сами не обновятся от изменения embedded defaults.

## Ссылки

- [Issue #63: Deliver Worker replies to the user through Secretary interfaces](https://github.com/GitHubFoxy/secretary/issues/63)
- [Issue #61: Show Secretary follow-up messages in the Worker Telegram thread](https://github.com/GitHubFoxy/secretary/issues/61)
- [`GLOSSARY.md`](../../GLOSSARY.md)
- [`docs/architecture/runtime-contracts.md`](../architecture/runtime-contracts.md)
