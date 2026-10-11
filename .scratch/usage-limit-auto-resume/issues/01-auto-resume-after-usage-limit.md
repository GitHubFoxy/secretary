# 01: Автоматически продолжать Worker после сброса лимита через официальный Codex API

Type: task
Status: ready-for-agent
Blocked by: ../../worker-error-diagnostics/issues/01-expose-worker-error-cause.md

## Результат и границы задачи

Worker, остановленный подтверждённым usage limit, видимо ожидает восстановления лимита. При включённом `auto_resume_on_limit` (default true) и надёжном времени сброса server продолжает прежнюю native session автоматически. Пользователь может продолжить вручную или отменить ожидание. Исследование и решения завершены; реализация ещё не выполнена.

Эта задача охватывает Worker Codex, Web observer, Telegram topic и durable recovery. Для проверенного usage-limit сигнала другого harness доступен manual hold без выдуманного timestamp. Диалоговый Secretary, смена провайдера/аккаунта, покупка кредитов и расход earned reset credits не входят в задачу. Реализовать после [понятных ошибок Worker](../../worker-error-diagnostics/issues/01-expose-worker-error-cause.md). Спецификация готова; зависимость задаёт порядок реализации.

## Исследование внутри задачи

### Реальный инцидент

11 октября 2026 в 09:24:33Z на `omarchy` Result `res_cee355ff10419cbf0263bff2a9ea6997` сохранил `acp rpc -32603: Internal error`. Соответствующий native turn Codex `01a12a47-2945-7053-be25-f599b729f549` вернул typed `usage_limit_exceeded`. Это проверили Gemini и основной агент независимо. ACP `RPCError` потерял `error.data`, а terminal runtime path проигнорировал объяснение из activity; это исправляется первой задачей.

В транскрипте этого инцидента `token_count` имеет `limit_id=premium`, `primary=null`, `secondary=null`; абсолютного reset timestamp нет. Текст «4:37 PM» не содержит даты/таймзоны. Из него нельзя строить расписание. Отсутствие timestamp в транскрипте не доказывает его отсутствие в свежем официальном API response. Полный [отчёт инцидента](../../../docs/research/worker-acp-incident-gemini-verification.md).

### Официальный источник лимитов

[OpenAI Codex App Server, раздел Rate limits](https://learn.chatgpt.com/docs/app-server) документирует JSON-RPC `account/rateLimits/read`, notifications `account/rateLimits/updated`, `rateLimitsByLimitId`, `primary/secondary.usedPercent` и `resetsAt` в Unix-секундах. Это выбранный operational source; внутренние JSONL-файлы используются только для исследования инцидента.

Основной агент проверил также схемы установленного `codex-cli 0.162.1` командой `codex app-server generate-ts --out <temporary-directory>`: `ClientRequest`, `InitializeParams`, `v2/GetAccountRateLimitsResponse`, `RateLimitSnapshot`, `RateLimitWindow`, `RateLimitReachedType`, `AccountRateLimitsUpdatedNotification`. Схема включает nullable `normalModelSlug`, `ordinaryUsageAllowed`, `accountId`. Notification является sparse update: nullable account metadata не очищает прежнее значение, нужен merge с полным read либо новый read. Наличие поля в 0.162.1 не означает обязательность в Node 0.162.0: использовать optional decode и capability fallback.

### Почему требуется отдельное подключение

`internal/node/codex_runtime.go` оборачивает `ACPRuntime`; `codex-acp` владеет своей native app-server connection. `account/rateLimits/read` относится к native Codex protocol, его нельзя отправлять в ACP `session/*` stream. Нельзя перехватывать stdin уже запущенного процесса. Выбран короткоживущий official app-server client на том же Execution node, с тем же выбранным Codex binary, effective native account/config environment и workspace; никаких новых model turns.

Проверенные существующие seams: `CodexRuntime.configured/Start/Resume`, `internal/acp/jsonl.go` generic JSON-RPC `StartWithLogExactEnvDir/Request/Notify/Close`, `ExecutionNode.watchSession`, `NewStoreEventSinkWithTrustedLocalApproval`, `RecordNodeAttemptOutcome`, `FinishAttempt/finishRetryableAttempt`, `RecoverPhase4Attempts`, `RunQueuedWorkerMessages`, `NodeRuntime.Resume`. Последний сейчас игнорирует аргумент текста и отправляет original Turn input: для limit continuation нужен отдельный immutable prompt. `finishRetryableAttempt` уже даёт next Attempt того же Turn без Result, но пока не создаёт schedule/checkpoint/command intent.

## Принятые решения

### 1. Короткоживущий official API client на Node

Добавить `internal/node/codex_limits.go` с helper `readCodexRateLimits(ctx, effectiveNativeConfig)`; он вызывается после terminal **подтверждённого** usage limit и завершается за 5 секунд вместе с cleanup. Использовать выбранный native `CodexCommand`, `app-server --listen stdio://` и generic JSON-RPC plumbing из `internal/acp`, без ACP initialization.

Порядок: `initialize` с `clientInfo` (name/title/version Secretary) и capabilities по сгенерированной совместимой схеме → дождаться response → notification `initialized` → `account/rateLimits/read` без params → decode allowlist → `Close` и bounded wait/cancel процесса. Drain notifications одновременно с request, иначе stream может заблокироваться. Отключить raw payload log; stderr bounded/sanitized. Никаких `thread/start`, `thread/resume`, `turn/start`, login/logout, config writes, email/reset-credit methods. Unsupported method, timeout, auth failure, malformed response или невозможность подтвердить тот же native account context возвращают unknown reset, не создают новую session и не меняют исходную ошибку.

Сохранить **effective native launch context** при `CodexRuntime.configured`: выбранный executable, allowlisted env, effective HOME/CODEX_HOME, cwd и native config overrides, которые `codex-acp` передаёт Codex. Сам native binary не обязан читать `CODEX_CONFIG`: необходимые overrides преобразовать в его поддерживаемые `-c key=value`, не копировать credentials в argv. Применить те же account/provider selectors и native store; query отдельного default account запрещён. Managed Worker MCP secrets/prompt/callback capabilities query-процессу не нужны, исключить их. Не читать auth.json для копирования секретов; Codex использует свой штатный credential store.

Если существующий generic process helper не гарантирует 5s cleanup, расширить его bounded termination/WaitDelay без изменения протокола обычных ACP sessions; process fixture должен подтвердить отсутствие зависшего query subprocess. Не добавлять новый daemon или зависимость.

### 2. Scoped выбор времени

Сохранять только выбранный `limitId`, timestamp observation, relevant windows, typed reached state и opaque внутреннюю account scope identity (если API её сообщает); accountId/email и raw response в публичный DTO не отправлять. Query всегда на исходном Node/HarnessInstance с исходным model/account launch context.

Выбирать bucket из `rateLimitsByLimitId` по подтверждённому limit ID ошибки или однозначному `normalModelSlug` текущей effective model. Единственный bucket допустим лишь при однозначном совпадении scope. Backward-compatible `rateLimits` использовать только если multi-bucket view отсутствует и single bucket однозначен. Не назначать premium/codex bucket по догадке. Неоднозначное соответствие → manual hold.

Для подтверждённого usage limit: выбрать все exhausted окна (`usedPercent >= 100`); если хотя бы одному exhausted окну не хватает валидного `resetsAt`, время неизвестно. Иначе выбрать максимум resets, включая weekly secondary. Никаких caps 24h/2 retries. Если выбранное время не в будущем либо не позже предыдущего deadline, manual hold. Если exhausted windows нет, category не меняется на billing: reset неизвестен.

`workspace_*_credits_depleted` и явно заданные spend-control blockers не являются автоматически сбрасываемым usage window. Их объяснение показать; автоматического retry нет. Ambiguous Claude proxy403 «quota or missing billing» также не входит в scheduler. Ноль credits в snapshot сам по себе не отменяет включённый rolling usage entitlement.

Snapshot — основание **времени следующей попытки**, не гарантия восстановленного доступа. `ordinaryUsageAllowed=false` запрещает automatic continuation; `null`/отсутствие не доказывает recovery. При новом confirmed future reset повторно ждать; при unknown/no-progress → manual hold. Автоматический query выполняется один раз после подтверждённого limit; постоянного секундного account polling и чтения транскриптов нет. Scheduled resume — одна новая попытка; если она снова получает лимит, заново читается официальный API и сохраняется новое ожидание. Не объявлять лимит снятым до успешной работы.

### 3. Состояния и атомарная запись

Node сообщает failed terminal observation с optional `WorkerFailure`/UTC `ResetsAt`/scoped quota evidence; server выбирает retry policy после immutable Node/HarnessInstance/Worker/Turn/Attempt validation. Подтверждённый usage limit даёт `OutcomeRetryable` даже без timestamp; другие ошибки сохраняют обычный final path.

Одной SQL transaction в store сохранить source failed outcome, next `AttemptScheduled` того же Turn, `TurnSuspended`, `WorkerLimited`, schedule, immutable continuation checkpoint и unclaimed resume command intent. Retry identity детерминирована source Attempt; replay не создаёт next Attempt повторно. Расписание ссылается на next Attempt и source Attempt; unique constraints для обоих.

Таблица `phase4_attempt_schedules`: `id`, `worker_id`, `turn_id`, `source_attempt_id UNIQUE`, `attempt_id UNIQUE`, nullable UTC `scheduled_at`, `state` (`pending`, `manual_hold`, `claimed`, `completed`, `canceled`), bounded failure metadata, quota scope/deadline evidence, immutable `continuation_prompt`, timestamps. Все identities/FKs принадлежат прежнему Worker binding. Сериализация deadline должна соответствовать store UTC timestamp; сравнивать через переданный clock, не смешивать SQLite `datetime('now')` и RFC3339 strings.

При `auto_resume_on_limit=true` и валидном будущем времени — pending; unknown/past time, permission blocker либо policy off — manual_hold. Пока Turn suspended, terminal Result отсутствует, FIFO `/q` сохраняется и не продвигается. Публикуется durable `worker.limited` event с причиной, режимом и deadline. Ровно один Result появляется после успешного/failed/canceled/interrupted завершения всего Turn.

### 4. Запуск и восстановление

Расширить существующий секундный `RunQueuedWorkerMessages`, обрабатывать schedules до FIFO; второго ticker не создавать. Readiness false/ошибка — pending остаётся нетронутым. Local lifecycle gate + SQL CAS дают защиту нескольких Store/process. Claim schedule и prepare command атомарны; command handoff использует действующий durable command claim/send/receipt path.

До handoff проверяется актуальная policy. `completed` schedule означает принятый resume command, а не выполненную Task. Native session загружается прежняя; если её нет, visible `runtime_session_unavailable`, новой session молча нет. `ContinuationPrompt = "Continue previous work from the point where it was interrupted by usage limit."` хранится в checkpoint и передаётся только для limit resume вместо original Turn input. Остальные recovery/queue resume остаются по прежней семантике.

Startup recovery сохраняет scheduled pending/manual_hold, не считает их active interrupted. Claimed schedule с ещё unclaimed command возвращается в prepared pending/manual_hold с прежними identities. Claimed command с unknown execution не отправляется снова: final interrupted Result с объяснением, расписание больше не работает. Accepted receipt/replay связывают schedule и Attempt без повторного native Resume. Не обещать exactly-once для неизвестного внешнего tool side effect.

### 5. Ручные действия, отмена и каналы

Owner API `POST /v1/workers/{ref}/resume` с Idempotency-Key вызывает `WorkerService.ResumeWorker`: общий gate/CAS для pending и manual_hold, тот же command identity. Offline не claim; гонка manual/automatic даёт один handoff. Web «Возобновить сейчас» и Telegram `/resume` только в Worker topic вызывают этот сервис. `/resume` вне topic не запускает работу; это не ответ pending Approval, request_id routing сохраняется.

Cancel/Close/новый direct input отменяют unclaimed schedule и intent, финализируют next scheduled Attempt через final canceled outcome с одним Result на suspended Turn. Новый input создаёт новый Turn после durable cancellation, с исходной idempotency identity. Уже claimed execution использует прежнее active cancel/steering/unknown handling; никаких скрытых retries. Close отменяет FIFO по прежнему контракту. Cancel сохраняет binding и обычное существующее разрешение FIFO после terminal.

Одна настройка `worker_policy.auto_resume_on_limit`, default true. Выключение переводит pending в manual_hold, не завершает Turn; проверять policy также перед claim. Повторное включение разрешает pending только с ещё валидным future deadline. Каналы получают limited/resume/cancel события без зависания до terminal Result; public Worker DTO содержит режим, deadline, can_resume_now и action identity. Опциональные поля совместимы со старыми peers.

## План реализации

1. **Первый ticket:** внедрить bounded typed failure в ACP/result/command receipt/wire/store/public DTO. Без него scheduler не классифицирует generic errors.
2. **Official API:** `internal/node/codex_limits.go` (новый helper), `codex_runtime.go` (effective native query context и Session wrapper), `internal/acp/jsonl.go` (reuse generic RPC, bounded cleanup при необходимости). Схемы брать из выбранной версии CLI, не переписывать весь generated protocol.
3. **Wire:** `node.go`, `protocol_types.go`, `execution_node.go`, `local_store.go`, `core/harness_contract.go` — optional timestamp/scope evidence, encoding/validation и outbox replay; старые envelopes без новых полей поддерживаются.
4. **Server/store:** `server_event_sink.go` и production wiring `cmd/secretaryd` передают effective worker policy. В `RecordNodeAttemptOutcome` перед обычным final path обработать confirmed limit через атомарное расширение `FinishAttempt/finishRetryableAttempt`; `phase4_types.go`, `core/store.go` migrations/read models и соответствующий migration backup contract получают новые states/table. Node не назначает server policy.
5. **Recovery:** `core/store.go:RecoverPhase4Attempts`, `phase4_store.go` и `node/phase4_recovery.go` сохраняют prepared scheduled work, checkpoint и command intent; terminal/replay/unknown predicates покрыть fixtures.
6. **Resume/очередь:** `ctl/worker_queue.go` существующий pump; `worker_lifecycle.go` lifecycle gates/cancel/direct input/manual resume; `node_runtime.go` и Node envelope/checkpoint path передают ContinuationPrompt. Additive APIs в `webapi/observer.go` и `public_worker.go`; scopes/idempotency сохраняются.
7. **Policy/каналы:** `internal/config` parsing/default config/effective worker policy; Web `App.svelte`/Worker observer read model, Telegram topic command parser/adapter/event renderer/durable delivery. Ограниченные metadata доступны до Result.
8. **Документация:** обновить `docs/architecture/runtime-contracts.md` для suspended Turn/next Attempt и explicit exception confirmed limit; старые запреты active/unknown replay сохранить.

## Критерии приёмки

- [ ] Official account RPC используется на правильном Node/account/config, без model turn, login, сторонних auth headers и raw transcript parsing.
- [ ] Fixtures проверяют handshake, full read, scopes, multi-bucket/legacy single fallback, weekly max, malformed/null/past/missing windows и explicit permission/credit blockers; category не меняется по отсутствию окна.
- [ ] Confirmed limit атомарно создаёт один outcome/next Attempt/checkpoint/unclaimed intent/schedule, без premature Result; replay сохраняет identities.
- [ ] Unknown reset/policy off видимы как manual_hold; FIFO и Approval routing сохраняются.
- [ ] Future deadline запускает одну continuation прежней session с новым Attempt того же Turn; original input повторно не отправляется.
- [ ] Restart/reopen/overdue/offline и prepared/claimed/accepted crash boundaries не теряют work и не повторяют unknown execution.
- [ ] Manual/automatic race, два Store connections, Cancel/Close/new direct message/policy toggle дают согласованный Result и command identity.
- [ ] Repeated verified future resets/weekly ожидание работают без caps; no-progress и query failure не запускают hot loop.
- [ ] Web/Telegram показывают причину, automatic/manual mode, deadline и controls; replay не дублирует уведомление.
- [ ] Старые peers/legacy bindings и обычные Follow-up/queue recovery остаются совместимыми; required package/race/Web checks проходят.

## Проверка: уже выполнено и к реализации

**Выполнено в исследовании:** Gemini подтвердил инцидент и исследовал lifecycle; основной агент повторил Result/native error и проверил local CLI 0.162.1 generated schema. Ранее существующие tests ACP/node terminal и core same-Turn retry прошли; это не тест новой функции. Live official account read и новые model turns не запускались. Gemini round3 остановился на собственном quota до изменения документов; официальный API plan завершил основной агент.

**Написать deterministic fixtures:** subprocess initialize/read/timeout-cleanup без model methods; latest version/old optional fields; scoped windows; server transaction reopen/replay; failed→schedule→resume receipt→one Result на публичном Node seam; exact crash stages; two-Store CAS/manual race; cancellation/policy/Approval/FIFO; Web/Telegram controls. Clock должен передаваться в schedule processing, ждать реальных часов в тестах нельзя.

Команды для исполнителя после добавления regression tests:

```sh
go test ./internal/acp ./internal/node -run 'TestCodexRateLimits|Test.*Limit.*Wire' -count=1
go test ./internal/core ./internal/ctl -run 'Test.*(UsageLimit|ScheduledResume|ManualResume|LimitedWorker)' -count=1
go test -race ./internal/core ./internal/ctl ./internal/node ./internal/webapi ./internal/telegram
go test ./...
npm test --prefix web
npm run build --prefix web
```

Новые тесты должны реально присутствовать: отсутствие matching tests не засчитывается как PASS. Build проверяет Svelte; generated web bundles/deploy не входят в подготовку ticket.

## Comments


- **2026-10-11 (Initial request)**: Пользователь запросил отдельную задачу по примеру T3 Code: автоматически продолжать работу после восстановления лимитов. Создан черновик.
- **2026-10-11 (Research & Design Complete)**: Проведено исследование первичных источников Codex и Claude. Выявлено структурированное поле `resets_at` в `token_count.rate_limits.primary`. Зафиксирован контракт: нативное продолжение в рамках одного Turn через `OutcomeRetryable` без дублирования Result, server-owned расписание, интеграция с FIFO очередью, fail-closed при крашах, ручной fallback при отсутствии точного времени сброса.
- **2026-10-11 (Round 2 Triage Closure)**: Устранены замечания родительского агента:
  1. Reset transport: спроектирован Bounded Exact-Session JSONL Reader на ноде, прописан сквозной путь wire fields `Session.Result` -> `ExecutionNode` -> `LocalStore` -> `ServerManager` -> `FinishAttempt`.
  2. Windows selection: закреплено правило `used_percent >= 100.0`, выбор `max(resets_at)`, блокировка tight retry при `resets_at <= now`, явное разделение `billing_credits` и запрет угадывания времени из локальных строк.
  3. Убраны произвольные ограничения 24h и 2 retries; обеспечена поддержка недельных квот и повторного ожидания при новом future reset.
  4. Скоуп честно ограничен Worker.
  5. Полностью согласована семантика состояний для неизвестного сброса (manual hold), explicit hold для `/q`, единого отменённого результата при новом вводе или `/cancel`.
  6. Спроектировано восстановление при сбоях: атомарная миграция `phase4_attempt_schedules`, статус `AttemptScheduled`, проверка offline readiness до CAS claim, fail-closed.
  7. Устранён баг повторной отправки `turn.Input` через внедрение `ContinuationPrompt`.
  8. Статус Claude Code честно зафиксирован как manual hold из-за отсутствия нормализованного timestamp в текущем адаптере.
  9. Разграничены существующие швы и запланированные тесты. Задача переведена в статус `ready-for-agent`.


2026-10-11: по явному уточнению пользователя operational источник заменён на официальный `account/rateLimits/read`. JSONL-reader и ограничения 24h/2 retries исключены из действующего плана. Gemini подготовил исследования в раундах 1–2; раунд 3 остановлен собственным quota. Основной агент завершил официальный API plan по documentation/generated schema и проверил согласованность.
