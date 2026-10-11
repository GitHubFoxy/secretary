# Ошибки прямого сообщения Worker и восстановление после лимита

Дата: 11 октября 2026. Исследование текущего кода Secretary V2, JSON-RPC и документированного поведения T3 Code. Исходный код и конфигурация не менялись; живому Worker сообщения не отправлялись. Термины: [GLOSSARY](../../GLOSSARY.md); обязательства: [runtime contracts](../architecture/runtime-contracts.md). Каталог `docs/adr/` отсутствует.

## Дополнение после проверки реального инцидента

Gemini нашёл production Result и соответствующий native turn на `omarchy`: причина — `usage_limit_exceeded`. Основной агент независимо повторил чтение обеих записей. См. [проверку инцидента](worker-acp-incident-gemini-verification.md). Ниже сохранены результаты первоначального исследования, когда production trace ещё не была найдена.

## Вывод

**Подтверждена потеря дополнительных деталей ACP-ошибки, но причина конкретного сбоя пользователя не установлена.** `RPCError` хранит только `code` и `message`: поле `error.data` отбрасывается при чтении JSON. Если harness прислал общий `message: Internal error`, пользователь закономерно получает `acp rpc -32603: Internal error`, даже когда `data` содержало полезную причину. Это воспроизведено синтетическим тестом на настоящих типах проекта. [jsonl.go:43](../../internal/acp/jsonl.go#L43), [jsonl.go:237](../../internal/acp/jsonl.go#L237).

Код `-32603` обозначает внутреннюю JSON-RPC-ошибку, а не лимит использования. Спецификация допускает дополнительные детали произвольной формы в необязательном `data`. Классифицировать любую такую ошибку как исчерпание лимита нельзя. [JSON-RPC 2.0, §5.1](https://www.jsonrpc.org/specification#error_object).

Рекомендуется сначала сохранить и нормализовать доступное объяснение harness/provider и показывать его в Worker observer. Автоматическое продолжение требует отдельного server-owned состояния ожидания, достоверного времени reset и защиты от повторного выполнения. Ни наличие произвольного RPC error, ни транспортное принятие сообщения не дают основания повторять работу. [runtime contracts:32](../architecture/runtime-contracts.md#L32), [runtime contracts:76](../architecture/runtime-contracts.md#L76).

## Подтверждённый путь сообщения и точки потери информации

1. Web отправляет POST `/v1/workers/{worker_ref}/message` с текстом и устойчивым для этой попытки `Idempotency-Key`. Неуспешный ответ превращается в JS Error из `body.error`, тела ответа либо HTTP status text; catch записывает этот текст в `observerError`. При ошибке текст поля ввода не очищается. [App.svelte:67](../../web/src/App.svelte#L67), [App.svelte:387](../../web/src/App.svelte#L387).
2. HTTP `/message` и `/follow-up` вызывают один `WorkerService.MessageWorker`; ошибка идёт в `writeClientMutationError`, который возвращает `err.Error()` текстом, по умолчанию с HTTP 400. Здесь нет структурированной категории provider error или информации о reset. [observer.go:454](../../internal/webapi/observer.go#L454), [idempotency.go:36](../../internal/webapi/idempotency.go#L36).
3. `MessageWorker` учитывает текущее состояние: `/q` сохраняет Queued message; `working` вызывает steering; `needs_input` требует `request_id`; `idle` создаёт новый Turn/Attempt и вызывает Dispatch с прежним Worker binding. Поэтому «написал Worker напрямую» само по себе не определяет, какой native RPC завершился ошибкой. [worker_lifecycle.go:553](../../internal/ctl/worker_lifecycle.go#L553), [worker_lifecycle.go:660](../../internal/ctl/worker_lifecycle.go#L660), [worker_lifecycle.go:707](../../internal/ctl/worker_lifecycle.go#L707).
4. ACP steering вызывает `_session/steering`; RPC error возвращается напрямую. Start/Resume также возвращают ошибку `session/new`, `session/load` либо настройки session. Указанная строка может возникнуть на разных этапах, а не только на `session/prompt`. [acp_runtime.go:587](../../internal/node/acp_runtime.go#L587), [acp_runtime.go:74](../../internal/node/acp_runtime.go#L74), [acp_runtime.go:130](../../internal/node/acp_runtime.go#L130).
5. При `session/prompt` runtime собирает assistant text, но ветка RPC error отдаёт `Result{Status: "failed", Summary: err.Error()}`. Собранный текст в этой ветке не используется как terminal report. Следовательно объяснение, пришедшее только text delta до ошибки, не становится Summary автоматически. Это не доказывает, что такое объяснение было в конкретном сбое. [acp_runtime.go:1397](../../internal/node/acp_runtime.go#L1397), [acp_runtime.go:1434](../../internal/node/acp_runtime.go#L1434).
6. Execution node преобразует runtime Result в `OutcomeFinal`; `ErrorCode` получает общий `result.Status` (например, `failed`), а Summary остаётся строкой. Публичные Result DTO уже содержат Summary и FailureCode, но reset/limit category в них не представлены. [execution_node.go:664](../../internal/node/execution_node.go#L664), [public_worker.go:32](../../internal/webapi/public_worker.go#L32).
7. ACP process запускается с stdin/stdout pipe, без присоединения stderr. Дополнительная ошибка, выведенная только в stderr, не становится runtime diagnostic. Результат `process.Wait` в reader также не преобразуется в содержательное описание завершения. Это относится к ACP path; не следует переносить вывод на отдельный native Claude adapter. [jsonl.go:142](../../internal/acp/jsonl.go#L142), [jsonl.go:291](../../internal/acp/jsonl.go#L291).
8. Включённый ACP raw log преднамеренно редактирует error до `{code, message: "[redacted]"}` и удаляет `data`; остальные payload также скрываются. Web diagnostics parser работает с `params`, а чистый error response без params/method не выдаёт в details. Поэтому existing redacted logs могут подтвердить RPC code и этап, но не восстановить утраченное объяснение provider. Не следует решать UX простым отключением редактирования. [acp_runtime.go:337](../../internal/node/acp_runtime.go#L337), [acp_runtime.go:418](../../internal/node/acp_runtime.go#L418), [diagnostics.go:65](../../internal/webapi/diagnostics.go#L65).

## Что известно о конкретном инциденте

От пользователя есть строка `acp rpc -32603: Internal error` и гипотеза об исчерпанном лимите. Не получены актуальные Worker reference, Turn/Attempt, harness/provider/version, время ошибки, исходный error payload и reset time. Поэтому **usage limit остаётся гипотезой**.

Во время локальной проверки родительский агент не обнаружил работающий локальный `secretaryd`: найден отдельный `secretary-node`; локальная `secretary.db` имеет старую дату сентября и не признана трассой текущего случая. Проверка локальной машины не исключает активный Secretary server на другой машине. Новые model turns ради исследования не запускались.

В предыдущем исследовании native Claude зафиксирован HTTP403 от настроенного provider и отсутствие принятого model tool-use. Это другой probe, другой transport и другой момент времени: он не подтверждает причину текущего ACP `-32603`. [claude-interactive-runtime-20261010.md:23](../../.scratch/phase-5/reports/claude-interactive-runtime-20261010.md#L23).

Чтобы установить причину, требуется безопасная трасса именно проблемного Turn/Attempt: метод RPC, code, очищенное объяснение, подтверждённая provider category, provider instance и reset/retry-after при наличии. Полные prompts, auth headers, tokens и необработанный stderr для этого не нужны.

## Что подтверждено в T3 Code

Документация T3 Code описывает `Limited` как остановку из-за usage/rate limit с сохранением объяснения provider. Есть ручное Resume; если provider сообщает reset time, можно запланировать продолжение или включить автоматическое планирование. План сохраняется после restart и запускается при поднятом environment; новое сообщение, archive или settle предотвращают запуск. Без reset time предлагается ручной retry. Очередь остаётся сохранённой и выполняется после продолжения. Snooze независимо от resume только возвращает видимость thread. Это подтверждение документированного продуктового поведения, а не аудит реализации scheduler. [T3 Code: Working with threads, Inspect agent work](https://github.com/pingdotgg/t3code/blob/main/docs/user/thread-sidebar.md#inspect-agent-work).

## Рекомендации для двух задач

Созданы отдельные задачи: [показывать причину ошибки Worker](../../.scratch/worker-error-diagnostics/issues/01-expose-worker-error-cause.md) и [автоматически продолжать после лимита](../../.scratch/usage-limit-auto-resume/issues/01-auto-resume-after-usage-limit.md).

### Показывать содержательную ошибку

- Сохранить `error.data` внутри ACP boundary, не предполагая единой схемы всех providers. Нормализовать проверенные поля в typed failure: категория, безопасное объяснение, native RPC code/method, provider и при наличии reset/retry-after. Не интерпретировать `-32603` как limit.
- Выбирать для UI проверенное объяснение provider; сохранять полезный `error.message`; при отсутствии деталей честно писать, что harness не передал причину, и показывать идентификатор диагностики. Общая строка RPC может оставаться раскрываемой технической деталью.
- Показывать ошибку у отправленного сообщения и в terminal Result/Worker observer, отличать «сообщение принято» от «работа завершена». Для известного лимита показывать время восстановления в локальном времени пользователя и доступные действия; при неизвестном reset не обещать автозапуск.
- Сделать ограниченный, очищаемый stderr diagnostic для ACP, если проверено, что выбранный harness сообщает причину там. Не публиковать всю stderr/data без фильтрации.
- Добавить регрессионные fixture tests для вложенного data, содержательного message без data, неизвестной схемы/неизвестного reset, non-limit `-32603`, очистки секретов и одинакового объяснения в transport error и terminal Result. Текущий UI уже передаёт text body пользователю, поэтому одна замена catch не исправит раннюю потерю информации.

### Автоматически продолжать после восстановления лимита

Имеющийся `FinishAttempt` умеет атомарно завершить retryable Attempt и создать следующую Attempt по durable retry command; это полезная база, но не scheduler до reset. В текущем Execution node runtime terminal всегда final; типизированный лимит и deadline необходимо довести до Secretary server. [phase4_types.go:321](../../internal/core/phase4_types.go#L321), [phase4_store.go:1007](../../internal/core/phase4_store.go#L1007), [execution_node.go:671](../../internal/node/execution_node.go#L671).

Рекомендуемый контракт: opt-in настройка автоматического продолжения, сохранённый deadline для конкретной Worker/Turn/Attempt и provider instance, явное состояние ожидания с отменой, идемпотентный claim после reset, продолжение прежней native session, восстановление просроченного плана после restart. Закрытие Task, Cancel, замена работы новым пользовательским указанием и уже начавшееся ручное продолжение должны блокировать устаревший запуск. При повторном подтверждённом лимите deadline обновляется; без достоверного reset остаётся ручное продолжение. Нельзя автоматически переключать provider или повторять выполненные инструменты.

Требует проектного решения: сохраняется ли один Turn при новой Attempt или создаётся Follow-up; что означает Cancel для сохранённой очереди; приоритет `/q` перед продолжением; что делать при offline Node, отсутствующей native session, смене provider credentials и reset, который не снял ограничение. Это пока открытые вопросы, а не реализованное поведение.

Такое изменение должно **добавить явное исключение для подтверждённого лимита** в текущие runtime contracts: там запрещены автоматический replay активной/неизвестно принятой Attempt и скрытый retry после Dispatch failure. Обычные сетевые/внутренние ошибки, неизвестное принятие и restart без подтверждённого лимита должны сохранить существующие гарантии. [runtime contracts:76](../architecture/runtime-contracts.md#L76), [runtime contracts:82](../architecture/runtime-contracts.md#L82), [runtime contracts:89](../architecture/runtime-contracts.md#L89).

## Выполненные проверки

1. `go test ./internal/acp ./internal/node -run 'Test(Request|ACPLegacyStopReasonsErrorsAndSummaryExtension|ACPNativeTerminalOutcomeCarriesTypedFailClosedEvidence)' -count=1` — PASS для обоих packages. В node выполнены синтетические проверки RPC error → failed Result и terminal contract; это не воспроизведение ошибки живого provider.
2. `go test ./internal/core -run '^TestPhase4RetryHasOneOutcomePerAttemptAndOneResultPerTurn$' -count=1` — PASS. Подтверждает существующий lifecycle fixture, а не auto-resume scheduler.
3. Временный Go overlay вне repo: `go test -overlay /tmp/<temporary>/overlay.json ./internal/acp -run TestResearchRPCErrorDropsProviderData -v -count=1` — PASS. Это узкий unit signal потери деталей, не полная end-to-end проверка. Временные файлы удалены; исходники не изменялись. Fixture передавал настоящий `acp.Message` такой JSON:

```json
{"jsonrpc":"2.0","id":1,"error":{"code":-32603,"message":"Internal error","data":{"message":"Synthetic usage limit hit","resetAt":"2030-01-01T00:00:00Z"}}}
```

Тест проверил `message.Error.Error() == "acp rpc -32603: Internal error"` и отсутствие data при обратном marshal. Получено:

```text
formatted="acp rpc -32603: Internal error"
remarshal={"code":-32603,"message":"Internal error"}
--- PASS: TestResearchRPCErrorDropsProviderData
```

Этот signal доказывает потерю детали из синтетического payload. Он не доказывает, что пользовательский harness передал `data`, и не устанавливает usage limit как root cause.
