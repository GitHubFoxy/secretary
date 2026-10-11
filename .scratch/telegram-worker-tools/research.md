# Исследование: tool events в Telegram topic Worker

Ticket: [01 — отсутствуют вызовы инструментов](https://github.com/GitHubFoxy/secretary/issues/30). Спецификация: [spec.md](spec.md).

## Краткий вывод

В коде нет общего несовпадения имён событий на пути от нормализованной Worker activity до Telegram: `attempt.activity` с `kind=tool_call`/`tool_result` имеет явное отображение в `worker.tool_started`/`worker.tool_finished`/`worker.tool_failed`; Telegram допускает эти типы и рендерит название и короткий preview. При заполненных полях и существующем `WorkerRef` обычный путь до topic согласован.

Production-причина не установлена: ticket не называет Worker, harness/version, пример вызова или наблюдения на границах цепочки; live Telegram/production я не проверял. Наиболее важная условная причина находится раньше Telegram: OpenCode-совместимый harness в текущем контракте **не заявляет** tool activity, а нормализатор отбрасывает неподдержанные или не имеющие явной identity события. Для Claude Code найдено отдельное подтверждённое ограничение: runtime передаёт только начало вызова, не `tool_result`; кроме того, его очередь activity может молча терять события при заполнении. Ни одно из этих условий нельзя приписать Worker из ticket без уточнения harness.

Также обнаружен условный replay-риск на Telegram-границе: tool-сообщения не получают durable identity события. При определённом порядке восстановления outbox и replay одно сообщение может отправиться повторно. Это отдельный риск для критерия replay/restart, не объяснение отсутствия tool events.

## Подтверждённые факты с источниками

### 1. Native runtime → Execution node

- ACP runtime принимает native `session/update` с `tool_call`, `tool_call_update` и `tool_result` ([`internal/node/acp_runtime.go:1585`](../../internal/node/acp_runtime.go#L1585)). Tracker использует явное имя из `name`/`tool_name`/`toolName`/`tool`; для MCP допускает fallback только при `_meta.is_mcp_tool_call=true` и наличии `rawInput.server/tool`. Имя из `title` не выводится. Sparse update связывается по `toolCallId`; ambiguous id-less update отбрасывается, повторные старты/завершения не создают новые Activity ([`acp_runtime.go:1618–1758`](../../internal/node/acp_runtime.go#L1618)). Завершённый первый кадр даёт один `tool_result`, а не выдуманный start.
- Это соответствует осторожной интерпретации официального [ACP Tool Calls](https://agentclientprotocol.com/protocol/tool-calls): `toolCallId` обязателен, `title` — человекочитаемое описание, а `tool_call_update` может быть sparse patch. Отдельный [ACP RFD Tool Call Name](https://agentclientprotocol.com/rfds/tool-call-name) объясняет, что `title`/`kind` сами по себе не идентифицируют инструмент, и предлагает отдельное поле `name`. Значит, title-only native кадры могут быть валидным отображаемым прогрессом ACP, но недостаточны для безопасной идентификации инструмента в этом контракте.
- Runtime выдаёт внутренние `Activity` с `Kind=tool_call` или `tool_result`, `Tool`, аргументами/результатом и статусом. Execution node нормализует их в `core.Activity`: проверяет объявленные `HarnessCapabilities`, санитизирует аргументы/результат, вычисляет компактный `Preview` и не принимает пустое имя ([`internal/node/execution_node.go:690–820`](../../internal/node/execution_node.go#L690)). `core.Activity` сериализует нормализованный payload как `kind`, `tool_call.name/preview` или `tool_result.name/preview/status` ([`internal/core/harness_contract.go:291–433`](../../internal/core/harness_contract.go#L291)).
- Важное различие: native event names (`tool_call`, `tool_call_update`, `tool_result`) не являются именами Telegram-событий. Между ними есть нормализованный слой `core.Activity`. `tool_call` означает старт; завершение передаётся отдельным `tool_result` со статусом.
- Runtime contracts отдельно фиксируют эти правила ACP: explicit native `name` либо подтверждённая MCP provenance; отсутствие synthetic start, защита от duplicate/id-less ambiguity и исключение title из identity/telemetry ([`docs/architecture/runtime-contracts.md:63–64`](../../docs/architecture/runtime-contracts.md#L63)).

### 2. Execution node → Secretary server

- `watchSession` читает каналы `Session.Activity()` и `Session.Result()`; activity проходит через `publishRuntimeActivity`, затем `LocalStore.QueueActivity` сериализует `NodeEvent{kind:"activity", activity:...}` в durable outbox ([`execution_node.go:638–706`](../../internal/node/execution_node.go#L638); [`internal/node/local_store.go:440–506`](../../internal/node/local_store.go#L440)). Node sequence/outbox сохраняются до отправки, а `AckThrough` удаляет записи только после ACK ([`local_store.go:472–535`](../../internal/node/local_store.go#L472)).
- Server protocol валидирует событие и вызывает `NewStoreEventSink`; sink перед ACK передаёт activity в `RecordNodeActivityReplay` ([`internal/node/server_event_sink.go:54–90`](../../internal/node/server_event_sink.go#L54)). Store проверяет неизменяемые Node/HarnessInstance/Worker/Turn/Attempt bindings, dedupe по Node `EventID`, состояние Attempt и атомарно сохраняет событие `attempt.activity` с `WorkerRef` и исходным normalized payload ([`internal/core/phase4_store.go:1956–2017`](../../internal/core/phase4_store.go#L1956)). Повтор после terminal ACK-ится без новой activity; неизвестная/неверно связанная Attempt не становится событием.
- Поэтому серверный durable event имеет outer `kind=attempt.activity`; tool kind находится внутри `payload.kind`. `WorkerRef` хранится и в metadata activity, и в верхнем поле server Event. Это не Conversation entry и не Result.

### 3. Secretary server → Telegram adapter → topic

- `attachProductionTelegram` запускает Telegram adapter и отдельный durable event bridge. Bridge читает общий журнал по глобальному `EventsAfterSeq(cursor, 500)`, передаёт каждый Event через `telegramEvent`, вызывает `HandleDurableEvent`, а после 500 событий продолжает следующую страницу ([`cmd/secretaryd/telegram.go:77–157`](../../cmd/secretaryd/telegram.go#L77)). Это не Web observer stream.
- `telegramEvent` берёт `WorkerRef` из Event (fallback на `AggregateID` только для `aggregate_type=worker`). Для `attempt.activity` читает вложенный `payload.kind`; `tool_call` → `worker.tool_started`, `tool_result` → `worker.tool_finished`, а `failed/error/canceled/cancelled` или ненулевой `error` → `worker.tool_failed`. Название и preview берутся из `tool_call.name/preview` либо `tool_result.name/preview` ([`cmd/secretaryd/telegram.go:159–250`](../../cmd/secretaryd/telegram.go#L159)). Если tool name пуст, событие отбрасывается, а не угадывается по title.
- Telegram `Event` имеет отдельные поля `Tool`/`ToolPreview`; `importantWorkerEvent` включает все три tool-типа, а `renderWorkerEvent` выдаёт `▶`, `✓` или `✗`, название и безопасно обработанный preview; при пустом названии renderer возвращает пустую строку ([`internal/telegram/adapter.go:112–123`](../../internal/telegram/adapter.go#L112); [`adapter.go:1585–1615`](../../internal/telegram/adapter.go#L1585)). Tool output и сырые аргументы в сообщение не передаются.
- Адаптер адресует Worker activity в topic по `WorkerRef`: `ensureTopic` использует сохранённый `TopicMapping`, а отправка использует его `ChatID` и `ThreadID`. Ветка обычной tool activity не создаёт General mirror; такой mirror предусмотрен для terminal Result. Адаптер пропускает Worker events без `WorkerRef` ([`adapter.go:721–850`](../../internal/telegram/adapter.go#L721)). `BotAPITransport.SendMessage` отправляет `message_thread_id` из ThreadID ([`internal/telegram/http.go:147–190`](../../internal/telegram/http.go#L147)).
- На Telegram-границе фильтр и renderer не выглядят причиной полного исчезновения события, если на вход поступил корректный `worker.tool_*` с заполненным `Tool` и `WorkerRef`. Ошибка отправки не маскируется: она возвращается bridge, который повторяет чтение с прежнего cursor.

### 4. Harness-specific ограничения и поля

- `DefaultOpenCodeProbeSpec` комментирует, что OpenCode ACP v2.0.22 переводит native tools в title/kind/locations/rawInput без явной identity; поэтому `tool_call`/`tool_result` не добавлены в его `ActivityCapabilities` ([`internal/node/harness_probe.go:681–689`](../../internal/node/harness_probe.go#L681)). Даже если ACP runtime увидел кадр, нормализатор проверяет capability и не публикует неподдержанный tool event. Для Worker на таком HarnessInstance отсутствие карточек инструментов — ожидаемый текущий контракт, а не дефект Telegram renderer. Это условная причина: ticket не указывает OpenCode.
- Claude Code `handleContent` преобразует `tool_use` только в `Activity{Kind:ActivityTool, Text:name}` и не извлекает typed result ([`internal/node/claude_runtime.go:535–545`](../../internal/node/claude_runtime.go#L535)). Claude capability использует только `observedRuntimeActivity` (`tool_call`, но не `tool_result`), тогда как ACP/Codex список включает оба ([`harness_probe.go:643–678`](../../internal/node/harness_probe.go#L643)). Общий нормализатор преобразует `ActivityTool` в start без preview; окончания/ошибки от этого пути нет.
- У Claude activity channel ёмкостью 64; `emitActivity` делает non-blocking send с `default`, то есть при заполнении silently drops text/tool activity ([`internal/node/claude_runtime.go:254–255,553–560`](../../internal/node/claude_runtime.go#L254)). Это подтверждённая возможность потери под backpressure, но частота/наличие такого режима в заявленном инциденте неизвестны.
- Для Codex/ACP есть regression native MCP: явная provenance позволяет идентифицировать MCP инструмент даже без `name`; tests проверяют MCP start/result, sparse completion и отсутствие утечки сырого preview/input/output ([`internal/node/acp_native_mcp_activity_test.go:14–30,103–150`](../../internal/node/acp_native_mcp_activity_test.go#L14)). В репозитории также есть исторический отчёт о native Codex + Web observer, но он не является live-проверкой текущего Telegram topic: [native MCP report](../phase-5/reports/native-mcp-tool-activity-fix-20261010.md), [Mac acceptance](../phase-5/reports/mac-native-acceptance-20261010.md).

### 5. Cursor/replay и обнаруженный риск

- Здесь несколько независимых sequence: Node outbox sequence для доставки и server Event `Seq` для глобального журнала; Telegram сохраняет `LastEventSeq` в state и читает server события строго после него. Web observer использует тот же server journal, но Worker-scoped фильтр и собственный `after_seq`.
- `HandleDurableEvent` сохраняет `LastEventSeq` **после** успешного `handleEvent`; при ошибке доставки sequence остаётся прежним. Для tool events `handleEvent` не задаёт `OutgoingMessage.Identity`; `outgoingMessageIdentity` тогда хэширует `ChatID:ThreadID:Text`. Outbox dedupe существует только пока такое сообщение pending; после успешной отправки запись удаляется. Durable delivered ledger по tool Event ID нет ([`adapter.go:677–710,793–835`](../../internal/telegram/adapter.go#L677); [`adapter.go:1076–1082,1120–1195`](../../internal/telegram/adapter.go#L1076)).
- В production запускаются параллельно `adapter.Run` и durable bridge. `PollOnce` в конце вызывает `Flush`, который дренирует сохранённый outbox. Возможен порядок: предыдущий tool send завершился ошибкой и оставил сообщение в outbox; после restart `Flush` успел его доставить и удалить, пока cursor ещё указывает перед тем же событием; bridge затем повторно обрабатывает событие и отправляет его вновь. Это **кодовый replay-риск при определённом interleaving**, не наблюдённый production-дубликат. Он не объясняет отсутствие tool calls, но текущие тесты не закрывают именно этот interleaving. Событийная identity/дедупликация должна опираться на durable event ID/seq, а не только на текст.

## Гипотезы и неопределённость

1. **Если проблемный Worker — OpenCode 2.0.22:** вероятная точка потери — до server journal: native tool payload без explicit name/provenance плюс отсутствие `tool_call/tool_result` capability приводит к отбрасыванию. Подтверждён текущий кодовый контракт, но не harness проблемного Worker.
2. **Если Worker — Claude Code:** start может публиковаться как `ActivityTool`; finish/failure по текущему адаптеру не поддержан. Отсутствие вообще всех start-сообщений этим не объясняется без дополнительной причины (например, отсутствие native `tool_use`, переполнение канала, binding/capability или ошибка доставки).
3. **Если ACP кадры конкретного Worker содержат только `title`/`kind`:** tracker специально их отбрасывает без подмены identity. Официальная документация объясняет, почему title не годится как programmatic name; на сервере/в Web они тоже не обязаны появиться.
4. **Если нормализованный `attempt.activity` присутствует в БД, но сообщение отсутствует в Telegram:** проверить `WorkerRef`, topic mapping, сохранённый `LastEventSeq`, outbox и ошибку Bot API. Кодовая цепочка в нормальном случае отображает это событие, но live журнал/Telegram receipt отсутствуют.
5. **Пограничная гонка terminal/activity:** `watchSession` выбирает между готовыми каналами `Activity` и `Result`; если оба готовы, Go `select` может обработать Result первым и завершить watcher до чтения буферизованной activity. Это может потерять последние события при конкретном расписании, но не объясняет все пропавшие вызовы и не установлено как причина ticket.

Не подтверждены: выбранный harness и его версия; наличие native tool кадра; точка, где он пропадает; состояние Telegram topic mapping; факт вызова Bot API или его ответ; production-частота replay-гонки.

## Варианты и рекомендуемый минимальный подход

1. **Сначала локализовать потерю на одном воспроизводимом Worker**, ничего не меняя. Зафиксировать только безопасные поля и число событий на границах: native kind/name/provenance/call ID/status → Node Activity → normalized `core.Activity` → server `attempt.activity` → mapped Telegram Event → Telegram `ChatID/ThreadID`/результат отправки. Не записывать сырой input/output, secrets, reasoning или session IDs.
2. Если событие отсутствует до server:
   - OpenCode — решать поддержку в Harness adapter/capability только если фактический источник предоставляет доказуемую identity. Не синтезировать tool name из `title`, `kind` или arguments.
   - Claude — отдельно определить совместимый источник terminal result и добавить typed result/capability только после проверки native stream shape; устранить silent drop или сделать потерю наблюдаемой.
   - ACP name-less MCP — использовать только подтверждённую provenance/semantic `server/tool` и стабильную связь по `toolCallId`, как уже делает tracker.
3. Если `attempt.activity` в server есть, минимальный срез — regression через реальный durable public path (`Store → bridgeTelegramEventsOnce → HandleDurableEvent → fake Bot API`) и исправление конкретного mapper/topic/send участка, если тест воспроизводит потерю.
4. Отдельно закрыть replay-риск durable event identity: проверить crash/restart и ситуацию, где outbox дренируется до replay исходного sequence. Не расширять scope фикса доставки Result или topic mappings без свидетельств.

Рекомендация: не начинать с изменения renderer. Сначала получить sample Worker/harness и определить первую границу, где отсутствует `tool_call` или `tool_result`; затем выбрать одно локальное исправление и отдельный интеграционный regression.

## План проверки

1. Запросить Worker reference, Harness kind/version, Node и ожидаемый инструмент; выяснить, показывается ли activity для того же Worker в Web observer.
2. В контролируемом тестовом запуске собрать безопасные counts/event IDs и поля `kind`, `Tool`, `Preview`, `WorkerRef`, `TurnID`, `AttemptID`, `Seq` на native → Node → store → mapper. Сопоставить server `attempt.activity` по `WorkerRef` и `AttemptID`; не использовать title как identity.
3. Добавить public-path интеграционный тест: создать Worker topic, записать `attempt.activity` в Store, прогнать `bridgeTelegramEventsOnce`; проверить start/success/failure в правильном thread, корректный tool name/краткий preview, отсутствие raw args/output и отсутствие сообщений tool activity в General/другом Worker.
4. Добавить restart/fault-injection для tool send: транспортный отказ, сохранённый outbox, cursor до/после recovery и конкурентный `Flush`; доказать отсутствие повторной нотификации и потери сообщения.
5. После фикса провести live-проверку с настоящим Worker и Telegram topic под контролем оператора. Сохранить версию harness, sanitized event trace, thread destination, результат Bot API и ограничения. Без этих свидетельств production-причину и исправление не считать подтверждёнными.

## Открытые вопросы

- Какой конкретно Worker и Harness/version использовались в исходном инциденте? Есть ли старые Worker bindings на OpenCode/Claude?
- Web observer того же Worker показывает tool activity? Если нет — искать до Secretary event store; если да — сравнить `WorkerRef`, server `Seq`, `telegram.LastEventSeq` и Bot API.
- Есть ли в журнале `attempt.activity` для соответствующих `AttemptID`; какие точные normalized `kind`, tool name, preview и status?
- Существует ли topic mapping для этого `WorkerRef`; совпадают ли `ChatID`/`ThreadID` с ожидаемым topic? Были ли ошибки отправки/перезапуск при pending outbox?
- Acceptance требует start и terminal. Принимается ли для используемого harness отсутствие preview, или нужен краткий preview и для tool result?

## Проведённые проверки и ограничения

Прошли целевые тесты текущего checkout:

- `go test ./internal/node -run '^(TestACPRuntimeShowsNativeCodexMCPActivity|TestACPRuntimeNativeToolIdentityAndLifecycle|TestExecutionNodeStopsActivityAfterTerminal|TestNormalizeRuntimeActivitySanitizesNestedToolPayloads)$' -count=1`
- `go test ./internal/webapi -run '^TestWorkerObserverReplaysBurstActivityFromDurableStore$' -count=1`
- `go test ./internal/telegram -run '^(TestWorkerToolEventsRenderActualNamePreviewAndLifecycle|TestImportantWorkerStatesAndRestartReplay|TestOutboxSurvivesTransportFailureAndRestart|TestWorkerTopicPromptPrecedesActivityAndSurvivesReplay)$' -count=1`
- `go test ./cmd/secretaryd -run '^(TestTelegramBridgeDelegationAndLongResultDoNotBlockReplay|TestTelegramEventBridgeMapsToolActivityWithoutArgumentsOrOutput|TestTelegramEventBridgeDropsToolEventsWithoutRealIdentity)$' -count=1`

Эти тесты подтверждают отдельные mapper/renderer/Node/Web contracts, но не сквозную доставку tool activity из server event store в конкретный Telegram topic и не live Telegram. Текущий mapper test вручную создаёт `core.Event` и вызывает `HandleEvent`; durable bridge test не содержит `attempt.activity`. `docs/adr/` в checkout отсутствует.

Во время исследования `git status` уже показывал изменённые `spec/THE spec.md`, `spec/memory.md` и untracked `.scratch/response-url-policy/`, `.scratch/telegram-voice-asr/`, `.scratch/telegram-worker-tools/`. Эти файлы и git state не изменялись. Live/production, deploy, profiles и конфигурация не затрагивались.
