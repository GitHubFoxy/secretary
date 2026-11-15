# Может ли Pi Durable стать harness внутри Secretary v2?

**Тип:** read-only архитектурная оценка  
**Проверка:** 5 октября 2026 года  
**Вывод:** технически — да, как отдельный экспериментальный runtime за границей Execution Node; сейчас — не вместо OpenCode и не вместо серверного журнала Secretary.

Исследование не меняло существующие файлы и не запускало тесты. Все ранее изменённые и неотслеживаемые файлы считались пользовательской работой. Отчёт создан в единственном разрешённом новом пути.

## Краткий вывод

Pi Durable похож на то, что Secretary уже ожидает от внутреннего harness: отдельные persistent conversations, model/tool loop, сохранённые промежуточные состояния и восстановление после перезапуска. Его можно было бы подключить за `internal/node.Runtime`/`Session`, сохранив Go-сервер владельцем Worker, Turn, Attempt, Approval и Result.

Но это **не готовая замена ACP-адаптеру**. `@earendil-works/pi-durable` — библиотека TypeScript/Node.js, а не Go SDK и не готовый ACP server. Между текущим Go `secretary-node` и библиотекой понадобится собственный sidecar и протокол. У него появятся отдельное хранилище, идентификаторы разговоров, схема tool events, lifecycle и модель provider credentials. Всё это надо явно согласовать с durable-командами и outbox Secretary.

**Рекомендация:** не менять активный OpenCode v2 путь в ticket 31. После завершения его release/Telegram gates рассмотреть небольшой изолированный PoC: один Worker, один Pi Durable conversation, отдельная SQLite БД, поддельная модель и один безвредный инструмент. Считать Pi Durable внутренним execution runtime, а не system of record. Не добавлять его в production defaults, пока PoC не пройдёт crash/replay, approvals, identity и security acceptance ниже.

| Вариант | Оценка |
|---|---|
| 1. Go core остаётся авторитетным; Pi Durable работает за Node Runtime/Session | **Лучший вариант для PoC.** Хорошо сочетается с существующей границей Node, но требует собственного sidecar и второго durable слоя. |
| 2. Pi Durable становится авторитетом execution state | **Не рекомендую.** Пересекается с уже реализованными Server Worker/Turn/Attempt/Outcome/Result и Node outbox; общего транзакционного commit между ними нет. |
| 3. Отложить Pi и продолжить с ACP/OpenCode | **Лучший вариант на сейчас.** Это согласуется с текущим контрактом и уже имеет локальную и private native проверку; остаются явно записанные rollout gates. |

## Что существует в Secretary сейчас

### Источник истины — не runtime session

- `go.mod` задаёт Go 1.27.1, SQLite и обычные Go-зависимости; Pi SDK или Node.js библиотек в Go dependencies нет. См. [`go.mod`](../../go.mod).
- В [`internal/core/phase4_types.go`](../../internal/core/phase4_types.go) есть самостоятельные `Worker`, `Turn`, `AttemptOutcome`, `Approval` и Result-модель. Worker сохраняет неизменяемую привязку к Node и `HarnessInstance`; серверные записи намеренно не содержат native runtime session ID.
- [`internal/core/phase4_store.go`](../../internal/core/phase4_store.go) хранит Attempt/Outcome/Result/Approval в серверном SQLite. Есть уникальность одного Outcome на Attempt и одного Result на Turn; `FinishAttempt` атомарно записывает retryable Outcome, следующий Attempt и команду retry. `RecordNodeAttemptOutcome` проверяет Worker/Node/Harness binding и идемпотентно принимает терминальное событие.
- [`internal/core/harness_contract.go`](../../internal/core/harness_contract.go) задаёт `HarnessInstance`, observed модели, reasoning и раздельные execution/activity capabilities. В текущем `HarnessKind` есть `opencode`, `fx`, `claude_code`, `codex`, но нет `pi_durable`.
- Нормативный Phase 4 spec объявляет Server источником истины, а native runtime IDs — Node-local данными; clean-install default сейчас OpenCode v2. См. [`spec.md`](../../.scratch/.archived/phase-4/spec.md), разделы 4, 6–8.

### Node уже имеет подходящую точку расширения

[`internal/node/node.go`](../../internal/node/node.go) определяет узкий Go-контракт:

- `Runtime.Start(ctx, StartRequest) (Session, error)`;
- необязательный `Resumer.Resume(ctx, StartRequest, runtimeSessionID)`;
- `Session.ID`, `Prompt`, `Steer`, `Cancel`, `Activity`, `Result`, `Close`.

`ExecutionNode.dispatch`, `resume` и `sessionForCommand` в [`internal/node/execution_node.go`](../../internal/node/execution_node.go) уже связывают эти вызовы с одной Attempt. [`internal/node/local_store.go`](../../internal/node/local_store.go) сохраняет Node-only `sessionMapping` с `WorkerRef`, `TurnID`, `AttemptID`, workspace и `RuntimeSessionID`; там же находятся дедупликация command ID и durable activity/outcome outbox. ID не отправляется в Server Worker record.

Роутинг выполняет [`internal/node/runtime_router.go`](../../internal/node/runtime_router.go): он выбирает адаптер по immutable `HarnessInstance.Kind` и возвращает видимую ошибку, если адаптер отсутствует. Следовательно, runtime, который сам управляет моделью, инструментами и persistent conversation, нельзя честно выдать за `opencode`; для него понадобятся явный kind, discovery/probe, capabilities и product decision.

### Фактический текущий кандидат — ACP/OpenCode

[`internal/node/opencode_runtime.go`](../../internal/node/opencode_runtime.go) готовит изолированный OpenCode v2 config, передаёт Profile/model/reasoning, устанавливает native data home и затем использует общий [`internal/node/acp_runtime.go`](../../internal/node/acp_runtime.go). ACP адаптер уже реализует Start/Resume, prompt/steer/cancel, stream activity, result, reconnect и typed permission/input requests.

Текущая dirty-реализация ticket 31 — не просто переключение настройки. В [`cmd/secretary-node/main.go`](../../cmd/secretary-node/main.go) OpenCode выбран default, есть отдельный native store, discovery и runtime. [`docs/configuration.md`](../../docs/configuration.md) и [`docs/node-deployment.md`](../../docs/node-deployment.md) описывают эти границы.

Важно не завышать evidence:

- `internal/node/opencode_persistence_e2e_test.go::TestOpenCodeNativeProfilePersistence` — opt-in native regression для managed profile, разрешённых/запрещённых инструментов, Follow-up и Resume того же session ID. `TestOpenCodeAuthenticatedWorkerReadAndResume` также opt-in.
- Последние записи ticket 31 фиксируют успешные private native/live прогоны на OpenCode v2.0.22 в ранее выбранном авторизованном store. Это сильнее mock ACP fixture, но не production rollout.
- Более поздняя [`docs/phase4-release-gate.md`](../../docs/phase4-release-gate.md) и ticket 33 отделяют новые изолированные stores: в них прошли private synthetic/unpaid persistence и missing-auth проверки, но login владельца, authenticated inventory/Worker acceptance в новых stores, production restart и legacy migration ещё не закрыты.
- Ticket 31 остаётся `claimed`; реальный Telegram acceptance, production deployment и существующий fx Worker Follow-up/restart остаются вне завершённых gates. Его текущее состояние подробно записано в [`31-make-opencode-v2-default-harness.md`](../../.scratch/.archived/phase-4/issues/31-make-opencode-v2-default-harness.md). В рамках этого исследования тесты не запускались.

OpenCode ACP v2.0.22 имеет известный предел: native source посылает progress без явного tool identity. Текущая документация поэтому не объявляет для него normalized tool-call/result capabilities и не угадывает имя из title. См. ticket 31 и `docs/node-deployment.md`. Pi Durable потенциально лучше в этой узкой точке, но только если его event API подключён и проверен.

## Что подтверждают первоисточники Pi Durable

### Отдельный продукт, не Pi CLI

Официальное объявление Earendil от 1 октября 2026 описывает Pi Durable как новый экспериментальный framework для приложений с длительными агентами и прямо говорит, что он **не заменяет** Pi coding agent: [анонс Pi Durable](https://earendil.com/posts/pi-durable/).

Это разные пакеты:

- [`@earendil-works/pi-durable`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/package.json) описан как durable conversation/task/document runtime;
- [`@earendil-works/pi-coding-agent`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/coding-agent/package.json) поставляет CLI-бинарник `pi`.

npm metadata для точного пакета Durable сообщает версию `1.0.2`, MIT, Node `>=22.19.0`, зависимости `@earendil-works/chord` и `@earendil-works/pi-ai` `^1.0.2`, а также exports для Node SQLite, JSONL и Memory storage: [npm metadata `pi-durable@1.0.2`](https://registry.npmjs.org/@earendil-works/pi-durable/1.0.2), [npm `latest`](https://registry.npmjs.org/@earendil-works/pi-durable/latest). Полная npm metadata указывает `dist-tags.latest = 1.0.2`.

README тега `v1.0.2` оставляет предупреждение **“Experimental. The API changes without notice between releases.”** Это прямо подтверждает риск из локального отчёта, несмотря на номер `1.0.2`: [опубликованный README](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/README.md). Для PoC нужна точная версия и lockfile; диапазона `^1.0.2` недостаточно как политика обновления экспериментального API.

Небольшая оговорка по сверке истории: npm-полная metadata coding-agent также показывает `latest = 1.0.2` и отдельная точная версия `1.0.2` существует, однако один запрос к сокращённому `/pi-coding-agent/latest` вернул `0.99.1`. Поэтому сравнение версий в этом отчёте основано на точных версиях и полной metadata, а не на одном алиасе `/latest`. Это не меняет главное: названия пакетов, публикуемые артефакты и назначение разные.

### API — библиотека agent runtime, не ACP protocol

В README `Harness.open(storage, { models, registry }, context)` открывает работу над Storage; `harness.root(...)` получает Conversation; `conversation.submit(...)` записывает input и возвращает `Submission`, у которой есть `wait()`. Durable entries и task checkpoints сохраняются до публикации; README обещает подхват незавершённой работы после повторного открытия storage. Публичные exports можно проверить в [`src/index.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/index.ts), типы API — в [`src/harness/types.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/types.ts), а описание концепций — в [README v1.0.2](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/README.md).

Эта модель достаточно близка к `Session`, но не совпадает с ней один к одному:

| Secretary `Session` | Возможный Pi Durable bridge | Разрыв/проверка |
|---|---|---|
| `Start` | Открыть выбранный durable storage, создать/открыть Conversation, применить Agent config | Нужны Node sidecar и стабильная связь Worker → Pi Conversation. |
| `Prompt` | `Conversation.submit({ type: "input", content, requestId })`, ждать `Submission` | Зафиксировать соответствие одного Go Attempt одному Pi run; не считать обычный model turn отдельным Attempt. |
| `Steer` / `Queue` | `whenBusy: "steer"` / `"followUp"` в `SubmissionDraft` | Проверить, что границы подачи совпадают с Secretary policy и что повтор команды не создаёт второй ввод. |
| `Cancel` | `Conversation.abort(context)` | Отмена только Context ожидания не отменяет исполнение; `Submission.abort()` лишь отзывает ещё не размещённый input. Нужен именно проверенный abort текущих tasks. |
| `Activity` | Подписка на conversation agent events/watch | `AgentEvent` и `watchEvents` помечены experimental. Добавить snapshot/replay, sequence и dedupe в Node protocol. |
| `Result` | Settled `Submission` и сохранённая assistant entry | Только Go server создаёт канонический Result и terminal AttemptOutcome. |
| `Resume(id)` | Повторно открыть storage и Conversation по сохранённому ID | ID принадлежит Node runtime и должен остаться в `LocalStore`, не в Server envelope. |

`SubmissionDraft` содержит опциональный `requestId`, но его наличие само по себе не доказывает exactly-once между Go `command_id`, Node claims, повторной отправкой и Pi submission. Этот стык нужно проверить тестом. Источники: `SubmissionDraft`, `Submission`, `ConversationAbortOptions` в [`harness/types.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/types.ts), admission/abort в [`harness/submissions.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/submissions.ts).

Официальный опубликованный API работает внутри JavaScript/TypeScript и использует Chord `Context`; upstream не предоставляет Go `Runtime` adapter и не документирует готовый Pi Durable ACP server. Значит, practical path — **отдельный Node.js процесс на машине Secretary Execution Node** и приватный request/event bridge, например framed JSONL по stdin/stdout. Это не «Node» как Secretary domain entity, а дополнительный Node.js runtime на host. Размещение TS библиотеки «в Go core» потребовало бы собственного JS runtime/FFI или IPC и всё равно не стало бы нативной Go dependency; разумной встроенной Go-интеграции upstream не предлагает.

### Persistence не означает system of record или exactly-once

README говорит, что `Harness` принимает Storage и возвращает работу после повторного открытия. Пакет публикует Memory, SQLite и JSONL implementations. Node-specific SQLite adapter использует `node:sqlite`/`DatabaseSync`, WAL checkpoint и busy-timeout options: [`src/storage/sqlite/node.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/storage/sqlite/node.ts). Это самостоятельные Pi Durable tables/documents/tasks, а не schema `modernc.org/sqlite` из Secretary Server.

Особенно полезен `ToolTask` в [`src/harness/tool.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/tool.ts): он durable-checkpoint-ит аргументы и `replay` policy перед выполнением. После recovery tool перезапускается только если policy, сохранённая перед запуском, **и** текущая регистрация tool обе говорят `safe`; иначе автоматический повтор запрещён. Это существенно снижает риск дублирования, но не делает Pi и Go core одной транзакцией и не подтверждает end-to-end идемпотентность произвольного side effect.

Pi Durable использует собственную задачу `pi.generation`, которая вызывает модель, создаёт `pi.tool` tasks и обрабатывает их. В [`src/harness/agent.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/agent.ts) default retry policy включена и допускает до трёх retries. Эти retries останутся внутренними Pi внутри одной Secretary Attempt, если Go adapter специально не классифицирует их иначе. Для side effects надо задавать строгую `safe/unsafe` policy и иметь idempotency key на границе инструмента; нельзя переводить неоднозначный crash в новую Go Attempt.

### Activity и approvals

Публичный тип `AgentEvent` в [`src/harness/events.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/events.ts) содержит `tool_execution_start/update/end`, `toolCallId`, `toolName`, аргументы и tool progress/result; также есть generation/message events. Это хороший потенциальный источник настоящего tool identity для `core.ActivityToolCall`/`ToolResult` — лучше, чем текущие OpenCode ACP title-only updates. Однако этот event stream экспериментальный, а args/output/details могут содержать содержимое workspace, секреты или prompt data. Следовательно, Node должен применять нынешние allowlist/sanitization правила из `internal/node/execution_node.go::normalizeRuntimeActivity`, сохранять только безопасные поля и не публиковать thinking/reasoning или raw tool payload.

В inspected public exports не обнаружен готовый эквивалент Secretary `Approval` record / `respond_worker` протокола. Pi предоставляет свои tool hooks/control и отмену, но Owner approval в Secretary должен оставаться серверной сущностью из `internal/core/approval.go`. Для адаптации понадобится blocking tool wrapper: он создаёт нормализованный Node permission/input event, ждёт ответа через существующий `Responder`/`CommandRespondWorker`, сохраняет `request_id` на Node и после reconnect переисполняет ответ с прежней identity. Эта custom path должна fail-closed; tool side effect не должен начинаться до durable approval. Это ещё не проверено на Pi Durable.

### Model, Profile и credentials

Pi Durable строится на `pi-ai`; README создаёт `models`, явно подключает `openaiProvider()` и отмечает чтение `OPENAI_API_KEY`. Типы содержат `ModelRef { provider, modelId }`, thinking level, Agent settings, instructions/extensions и набор tools: [README](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/README.md), [`harness/types.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/types.ts), [`harness/agent.ts`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/agent.ts).

Значит, Profile и allowlist можно перевести в Pi Agent конфигурацию, а имена/схемы зарегистрированных Pi tools — использовать для фактических `tool_call` events. Но в опубликованном API нет автоматического отображения OpenCode MCP config из `StartRequest.MCPServers` в Pi Durable. MCP/Secretary tools надо отдельно регистрировать и безопасно связывать с существующими server-owned MCP operations. Текущие exact model/reasoning pins и observed inventory тоже нельзя просто перенести: потребуется Pi-specific provider/model probe, exact model/thinking mapping и ошибка без fallback.

`pi-ai` provider setup нельзя считать эквивалентом существующего OpenCode native auth store. README показывает явный provider instance и API-key env; в репозитории OpenCode уже есть выделенные stores и scrub ambient credentials. Новый путь должен получить собственный owner-approved credential flow либо явно выданный credential, не читать и не копировать OpenCode/личные credentials. Его нельзя прятать в prompt/config/args.

## Хранилища и владение данными

При варианте 1 существуют **три независимых durable состояния**, и каждое решает свою задачу:

1. **Secretary Server SQLite:** канонические Conversation entries, Worker binding, Turns, Attempts, Outcomes, Results, Approvals, события и delivery state.
2. **Node LocalStore:** idempotent command claims, Worker → native runtime mapping, pending request IDs и durable outbox до Server ACK.
3. **Pi Durable storage:** локальная transcript/entry history, Agent settings, submissions, Pi tasks/checkpoints, tool state и provider session identity.

Не подключать Pi Durable к `secretary.db` и не объявлять его SQLite авторитетом над Server. У Go Server и Pi нет общего transaction manager: сбой между Pi submit, Node mapping, Node ACK и Go outcome может оставить запись в одном слое без соответствующего состояния в другом. Для каждого перехода нужна reconcile policy.

Практичная mapping-схема для PoC:

```text
Server WorkerRef + immutable HarnessInstance
        ↕ только через authenticated command / Node-local mapping
Node AttemptID → Pi Conversation ID + Pi submission/task IDs
        ↕
Pi Durable SQLite в приватной постоянной Node data directory
```

Один Worker должен переиспользовать одну Pi Conversation для Follow-up; очередной Go Attempt создаёт новый Pi input/run внутри неё, а не новую Worker identity. Pi Conversation ID, Pi task IDs и raw Pi history остаются на Node. Go Server получает нормализованные Activity и ровно один terminal AttemptOutcome/Result. Дубликат Activity/Outcome по Node outbox event ID остаётся идемпотентным по существующей core логике.

Для первого PoC проще изолировать одну Pi DB на один Worker и не проверять параллельный доступ к общему файлу. Это **не** рекомендация для production масштаба: при нескольких Workers надо отдельно выбрать shared Node storage/процесс или DB per Worker и проверить конкурентное открытие, backup, WAL и restart. У upstream SQLite adapter есть lock timeout, но из этого не следует гарантия multi-process Pi task ownership.

## Сопоставление трёх решений

### 1. Go core authoritative, Pi Durable за Node Runtime/Session — условно подходит

**Плюсы**

- Сохраняются нынешние lifecycle, immutable Node/Harness binding, Approval API, Result semantics, command dedupe и Node outbox.
- Node уже владеет native runtime mapping и реализует Restart/Resume на границе адаптера.
- Pi Durable может дать durable внутренние generation/tool checkpoints и richer tool identities; его внутренние retries/tasks остаются деталями одного Go Attempt.
- Go Server может и дальше принимать только sanitized activity/outcome events с привязкой к точным `WorkerRef/TurnID/AttemptID`.

**Цена и условия**

- Новый runtime kind/probe, Node.js `>=22.19.0`, собственный bridge, npm lockfile и отдельный data home.
- Конвертация Profile, Model/Thinking, allow_tools, MCP, Approval/needs-input и Session methods.
- Между серверным командным claim и Pi durable submit нужна fault-injection acceptance; без неё нельзя ACK-ать dispatch.
- Экспериментальный API требует pin версии и отдельного upgrade test. Никакого silent fallback в OpenCode/fx.

**Оценка:** лучший вариант для ограниченного PoC. Не готов для production или для переключения default.

### 2. Pi Durable authoritative для execution state — не подходит как простая интеграция

Если этим подразумевается, что Pi DB решает, завершён ли Secretary Worker Turn, принят ли Approval и существует ли пользовательский Result, вариант конфликтует с текущими Go constraints: те же lifecycle projections уже существуют в `phase4_store.go`, а Server — источник истины по spec. Outbox/replay и `command_id` дедупликация тоже живут за пределами Pi store. Появится двойное владение и вопрос, какую DB считать правильной после частичного commit.

Pi Durable можно признать авторитетом **только для внутреннего execution transcript/task checkpoint**, сохраняя Go lifecycle canonical. Но это уже вариант 1, а не замена Secretary system of record. Полная замена потребует перепроектировать доменную модель, approval ownership, node reconnect/outbox и migration; upstream библиотека сама эти Secretary API не предоставляет.

**Оценка:** не рекомендую; не включать в PoC.

### 3. Отложить и продолжать ACP/OpenCode — правильное операционное решение сейчас

Текущая реализация уже проходит доступные Node Runtime/Resumer seams и имеет native persistence/resume evidence; установка Pi-пакета не нужна для продолжения Phase 4. Ticket 31 одновременно ещё не закрыт по product gates, а ticket 33 требует owner login/acceptance в новых изолированных stores. Переключение на новый экспериментальный TS runtime сейчас создаст вторую матрицу acceptance до окончания первой.

**Плюс:** меньше изменений и сохраняется согласованный OpenCode v2 contract.  
**Минус:** остаётся ограничение явной tool identity в OpenCode ACP v2.0.22 и незавершённые production/Telegram gates. Pi может стать последующим альтернативным Node runtime, если tool activity/сохранение run даст измеримую ценность.

**Оценка:** предпочтительный выбор для проекта в данный момент.

## Минимальный PoC после отдельного разрешения

PoC не должен затрагивать текущий OpenCode config, уже существующие Worker bindings, production services или native stores.

1. **Изолированный host:** sidecar на Execution Node, отдельный каталог `0700`, отдельная SQLite DB, точная версия `@earendil-works/pi-durable@1.0.2` и lockfile. На первом этапе — один Worker и одна Pi Conversation.
2. **Детерминированная модель:** fake/local `pi-ai` provider без платного API-вызова; один synthetic Profile и один tool, ограниченный временным workspace. Начать с read-only инструмента, чтобы crash/replay не создавал внешний side effect.
3. **Минимальный bridge:** Go адаптер реализует `Runtime`, `Resumer`, `Session`; документирует framing, command IDs, event IDs, backpressure и close/abort. Pi Conversation ID сохраняется только в Node LocalStore mapping.
4. **Один lifecycle:** Start → Prompt → named tool call/result Activity → один terminal Result; затем Close/Resume того же Worker conversation и Follow-up в ней же.
5. **Crash injection:** остановить host на границах «Go command claim», «Pi input commit», «tool intent checkpoint», «tool side effect», «tool result commit», «Node outcome/outbox» и «Server ACK». Проверить reconcile behavior, не только успешный restart.
6. Только после предыдущих шагов добавить cancel/steer, один approval round-trip и второй Worker для проверки concurrency. Реальный provider auth и deployment остаются отдельным owner-approved этапом.

Не добавлять Pi в default inventory или `RuntimeRouter` до прохода всей обязательной матрицы ниже. Такой PoC не выполнен в рамках этого исследования.

## Критерии pass/fail для решения о дальнейшем использовании

| Проверка | PASS | FAIL |
|---|---|---|
| Identity и ownership | Один Secretary Worker остаётся привязан к тому же Node/HarnessInstance. Pi Conversation ID/task IDs остаются только в Node LocalStore; Server сохраняет canonical Worker/Turn/Attempt/Result. | Native ID попал в Worker envelope/server record, повторный Start создал второго Worker/conversation либо Pi store определяет Server Result. |
| Adapter contract | Start/Prompt/Steer/Queue/Cancel/Activity/Result/Close и Resume маппятся через ограниченный bridge; на ошибке виден runtime failure. | Требуется подменять `opencode` kind, запускать новый Worker/session при Resume или делать silent fallback. |
| Durable Resume | После остановки sidecar повторное открытие той же DB продолжает тот же Pi Conversation и сохраняет прошлую историю; Go Attempt получает ровно один terminal Outcome и Turn — один Result. | Resume теряет transcript/session, публикует исторические события как свежую Attempt activity или создаёт дубликат Outcome/Result. |
| Команды и replay | Повтор server `command_id`, Pi `requestId` и outbox event не выполняет повторный dispatch/input; повтор event не дублирует Activity. | Один и тот же доставленный command вызывает два Pi run, два tool call или второй Result. Одного поля с именем `requestId` недостаточно без теста. |
| Side effects | Перед вызовом сохраняется replay policy; автоматически повторяются только доказанно idempotent tools с тем же idempotency key. Неизвестный результат после crash становится `interrupted`/visible uncertainty, не replay-ится автоматически. | Tool с неопределённым внешним результатом запускается повторно или Go создаёт новый Attempt после uncertain crash. |
| Tool Activity | Реальные `toolCallId`/name/start/end конвертируются в `ActivityToolCall`/`ToolResult`; event replay дедуплицируется. Args/output проходят Secretary sanitizer; thinking/private data не публикуются. | Имя угадывается, событие синтезируется из текста, сырые arguments/output/reasoning выходят в observer/logs или name теряется. |
| Approval и input | Go Approval остаётся canonical; permission event привязан к Worker/Turn/Attempt и durable `request_id`; allow/deny переживает reconnect; side effect начинается только после accepted response. | Pi local state сам решает approval, response теряется/дублируется или отказ не препятствует инструменту. |
| Model/Profile/tools | Probe наблюдает конкретные модели и thinking levels для этого Pi adapter; выбранные exact pins и tool allowlist подтверждаются перед run. Unsupported model/profile возвращает видимую ошибку без подстановки. | Используется случайный provider default, unsupported reasoning молча игнорируется или extra tool появляется вне Profile allowlist. |
| Доступы/секреты | Выделенная auth path, точный очищенный subprocess environment, private DB/permissions, tools ограничены workspace/policy; npm dependencies и лицензии просмотрены. | Используются личный OpenCode store/ambient API keys, рабочая DB лежит в Workspace или sidecar имеет незаявленный tool/network доступ. |
| Crash/restart и конкурентность | Fault injection проходит на старте, tool и terminal-event границах; два независимых Worker не повреждают/не теряют Pi state. | DB lock, два scheduler, shutdown или reconnect порождают duplicate run либо потерю состояния; такие случаи объявляются unsupported без видимого отказа. |
| Обновление | Точная версия и transitive dependency graph закреплены; upgrade проходит persistence/replay regression до rollout. | Обновление по `latest` или диапазону `^1.0.2` неожиданно меняет публичный контракт/формат без migration gate. |

**Решение:** любой FAIL в ownership, uncertain side effects, Approval, credentials или duplicate Result — стоп для применения в Secretary, даже если один demo prompt успешен.

## Что пока нельзя подтвердить

- Ни этот repo, ни npm dependencies не устанавливались; Pi Durable не запускался против Secretary, OpenCode или реальной модели. Это исследование первоисточников и кода, не integration proof.
- Не проверена совместимость опубликованного npm tarball со всеми текущими `main` API/source files; для реального прототипа целевой контракт должен быть именно npm `1.0.2` плюс соответствующий tag/commit, а не mutable `main`.
- Не подтверждено, что open/reopen/recovery выдерживает одновременные процессы/несколько host процессов на одном SQLite store или все power-loss и SQLite WAL сценарии.
- Не доказана exactly-once семантика от Secretary `command_id` до Pi submission и arbitrary external tool. ToolTask safe/unsafe — локальная защита Pi, не общая транзакция Go/Pi.
- Не реализованы и не проверены Secretary Approval/`needs_input`, MCP bridge, Pi inventory/probe, перевод Profiles/allow_tools, provider login и mapping конкретных моделей/reasoning.
- Не проверены задержка, throughput, storage growth, backup/restore, retention, параллельные Workers и поведение при обновлении schema/API.
- Не выполнены полный production setup и Telegram acceptance текущего OpenCode default; это известные gates текущего проекта, а не пробелы, которые можно закрыть подключением Pi.

## Первоисточники

### Pi Durable

- [Официальный анонс Earendil (1 октября 2026)](https://earendil.com/posts/pi-durable/)
- [README пакета на immutable tag `v1.0.2`](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/README.md)
- [npm metadata `@earendil-works/pi-durable@1.0.2`](https://registry.npmjs.org/@earendil-works/pi-durable/1.0.2) и [npm `latest`](https://registry.npmjs.org/@earendil-works/pi-durable/latest)
- [Package exports](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/index.ts), [Harness API/types](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/types.ts), [Harness implementation](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/harness.ts)
- [Submissions](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/submissions.ts), [Agent events](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/events.ts), [Tool task replay](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/tool.ts), [Agent defaults](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/harness/agent.ts)
- [Node SQLite adapter](https://github.com/earendil-works/pi/blob/v1.0.2/packages/durable/src/storage/sqlite/node.ts)
- [Отдельный Pi coding-agent package на том же tag](https://github.com/earendil-works/pi/blob/v1.0.2/packages/coding-agent/package.json)

### Secretary v2

- [`internal/core/phase4_types.go`](../../internal/core/phase4_types.go), [`internal/core/phase4_store.go`](../../internal/core/phase4_store.go), [`internal/core/harness_contract.go`](../../internal/core/harness_contract.go)
- [`internal/node/node.go`](../../internal/node/node.go), [`internal/node/runtime_router.go`](../../internal/node/runtime_router.go), [`internal/node/execution_node.go`](../../internal/node/execution_node.go), [`internal/node/local_store.go`](../../internal/node/local_store.go)
- [`internal/node/opencode_runtime.go`](../../internal/node/opencode_runtime.go), [`internal/node/acp_runtime.go`](../../internal/node/acp_runtime.go), [`internal/node/opencode_persistence_e2e_test.go`](../../internal/node/opencode_persistence_e2e_test.go)
- [`cmd/secretary-node/main.go`](../../cmd/secretary-node/main.go), [`docs/configuration.md`](../../docs/configuration.md), [`docs/node-deployment.md`](../../docs/node-deployment.md), [`docs/phase4-release-gate.md`](../../docs/phase4-release-gate.md)
- [Ticket 31](../../.scratch/.archived/phase-4/issues/31-make-opencode-v2-default-harness.md), [Ticket 33](../../.scratch/.archived/phase-4/issues/33-isolate-opencode-native-state.md), [Phase 4 spec](../../.scratch/.archived/phase-4/spec.md)

## Может ли Pi Durable заменить Go Secretary v2? Полный перенос, гибрид или продолжение ACP

Этот раздел отвечает на более широкий вопрос, чем предыдущая оценка adapter seam: может ли Pi Durable заменить Go-сервер и его SQLite как систему учёта, и что на деле потребуется для такого перехода.

### Короткий ответ

**Напрямую мигрировать Go core на Pi Durable нельзя: готового порта, импортёра Secretary DB или эквивалентного приложения у Pi Durable нет.** Полная замена концептуально возможна как разработка нового TypeScript-приложения Secretary, использующего Pi Durable для agent conversations/tasks, но это переписывание продуктового сервера вокруг библиотеки, а не перенос базы в Pi или замена Go core одним вызовом API.

Из трёх вариантов:

1. **Полная замена Go core TypeScript-сервисом на Pi Durable** — осуществима как отдельный большой rewrite; сейчас не обоснована и несёт наибольший риск данных и гарантий.
2. **Гибрид: Go Secretary остаётся каноническим, Pi Durable исполняет работу** — технически возможен за существующей Node Runtime-границей, но добавляет ещё один store и мост; это наименее рискованный способ проверить Pi, но не даёт причин переносить system of record.
3. **Продолжить текущий ACP/OpenCode путь** — предпочтительный вариант для текущего этапа. Он уже вписан в Secretary runtime contract, а текущие release/acceptance gates ещё не закрыты.

Моя рекомендация: **не заменять Go core и не начинать миграцию данных.** Сначала закрыть записанные OpenCode gates. Если останется конкретная проблема, которую Pi Durable решает лучше ACP (например, настоящая tool-call identity или восстановление внутренних checkpoints), отдельно разрешить гибридный spike из предыдущего раздела.

### За что сейчас отвечает Go Secretary

Go core — не тонкая обёртка для запуска модели. Его замена требует восстановить перечисленные ниже доменные гарантии, даже если Pi Durable сохранит transcript и task checkpoints.

| Область | Текущая ответственность и гарантия | Основные источники |
|---|---|---|
| Каноническая беседа и входы | `core.Store` хранит `persons`, `conversations`, упорядоченные `conversation_entries`, дедупликацию внешних входов по `(adapter_id, external_message_id)`, события и доставки. `AppendInbound` не превращает повтор одного внешнего message ID в новый вход. | [`internal/core/store.go`](../../internal/core/store.go): `migrate`, `AppendInbound`, `EntriesAfter`, `appendEntryWithIdentity` |
| Секретарь и его turn lifecycle | Идентичность Secretary привязана к Person/Conversation; входы очередятся в `secretary_turns`; события имеют последовательность. `FinishSecretaryTurnWithOutput` атомарно завершает turn, фиксирует успешный ответ/выходные delta и связанное событие/доставку. | [`internal/core/secretary.go`](../../internal/core/secretary.go), [`internal/core/secretary_completion.go`](../../internal/core/secretary_completion.go): `FinishSecretaryTurnWithOutput`, [`internal/secretary/runtime.go`](../../internal/secretary/runtime.go) |
| Worker продуктовая модель | Worker, его immutable Project/Node/Harness binding, Turns, Attempts, отдельные AttemptOutcome и один пользовательский Result на Turn — сущности Secretary, а не синонимы agent conversation или Pi task. Retryable outcome, новый Attempt и retry command могут фиксироваться одной транзакцией. | [`internal/core/phase4_types.go`](../../internal/core/phase4_types.go), [`internal/core/phase4_store.go`](../../internal/core/phase4_store.go): `FinishAttempt`, `RecordNodeAttemptOutcome` |
| Разрешение исполнения | `ResolveDispatch` проверяет Project mapping, Node online/draining/revoked, фактически наблюдённый HarnessInstance и его model/reasoning pins; явный выбор не перекидывается молча на другой harness. Worker фиксирует результат разрешения как неизменяемую привязку. | [`internal/core/dispatch_resolver.go`](../../internal/core/dispatch_resolver.go), [`internal/core/projects.go`](../../internal/core/projects.go), [`internal/core/harness_contract.go`](../../internal/core/harness_contract.go) |
| Команды и неопределённые исходы | Server сохраняет стабильный Worker command ID, dedupe key и lease до handoff. Повтор доставки использует ту же команду; неизвестный результат может стать `uncertain`, а не притворяться отказом или безопасным retry. Node claims дедуплицируют command ID; нормализованные events идут через durable outbox до Server ACK. | [`internal/core/phase4_store.go`](../../internal/core/phase4_store.go): `ClaimWorkerCommand`, `ReclaimWorkerCommand`; [`internal/ctl/worker_lifecycle.go`](../../internal/ctl/worker_lifecycle.go): `WorkerService`, `deliverCommand`; [`internal/node/local_store.go`](../../internal/node/local_store.go) |
| Approval и управление доступом | Approval — серверная запись с Worker/Turn/Attempt/Node/Project identity, решением и audit event. Client pairing, scope checks, revocation, одноразовое Node pairing и Node credentials — отдельные security границы, а не возможности agent runtime. | [`internal/core/phase4_types.go`](../../internal/core/phase4_types.go): `Approval`; [`internal/core/approval.go`](../../internal/core/approval.go); [`internal/core/client.go`](../../internal/core/client.go); [`internal/core/node_registry.go`](../../internal/core/node_registry.go) |
| Project/Node control plane | Project paths и execution policy, Node enrollment, credential derivation/encryption, liveness, drain/revoke, inventory и удалённый Node protocol нужны до того, как Worker вообще может попасть к runtime. | [`internal/core/projects.go`](../../internal/core/projects.go), [`internal/core/node_registry.go`](../../internal/core/node_registry.go), [`internal/node/server_manager.go`](../../internal/node/server_manager.go), [`internal/node/protocol.go`](../../internal/node/protocol.go), [`internal/node/protocol_types.go`](../../internal/node/protocol_types.go) |
| Внешние интерфейсы | Web API обслуживает беседу/потоки, Worker и Approval routes, scoped Clients и секретаря; MCP даёт Secretary инструменты управления Worker; Telegram — отдельный adapter с собственными auth/bridge semantics. | [`internal/webapi/server.go`](../../internal/webapi/server.go), [`cmd/secretary-mcp/main.go`](../../cmd/secretary-mcp/main.go), [`internal/telegram/adapter.go`](../../internal/telegram/adapter.go), [`cmd/secretaryd/telegram.go`](../../cmd/secretaryd/telegram.go) |
| Конфигурация и эксплуатация | Профили, tools, модели, policy, `user.md`, skills, секреты окружения, запуск server/Node, логи и backup/restore не хранятся в Pi task log и требуют собственной реализации/переноса. | [`internal/config/config.go`](../../internal/config/config.go), [`cmd/secretaryd/main.go`](../../cmd/secretaryd/main.go), [`docs/configuration.md`](../../docs/configuration.md), [`docs/node-deployment.md`](../../docs/node-deployment.md), [`docs/always-on-runbook.md`](../../docs/always-on-runbook.md) |

Это не означает, что Secretary гарантирует exactly-once произвольного внешнего side effect: такого обещания нет. Важная текущая гарантия уже: durable identity, scoped dedupe, повторяемая доставка, явная неопределённость, проверяемая привязка результата и запрет слепого повторного выполнения там, где исход неизвестен. Замена обязана сохранить эти свойства и их границы, а не обещать абстрактное «exactly once».

### Pi Durable и Go SQLite — разные модели данных

У Pi Durable есть публичный TypeScript API `Harness.open(storage, ...)` → Conversation → `conversation.submit(...)` → `Submission.wait()`, свои conversation entries, tasks/checkpoints, tools и event stream. Это API harness-библиотеки, а не Secretary API для Person, Project, Node, Worker, Approval, Telegram, scoped Client или server delivery. Даже если Pi task по назначению похож на часть исполнения Worker, он не несёт всех Secretary инвариантов и не заменяет их без отдельного прикладного слоя. Публичные типы и код v1.0.2 описаны в [Harness API](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/harness/types.ts), [Harness implementation](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/harness/harness.ts), [Submissions](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/harness/submissions.ts) и [Storage API/types](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/types.ts).

Обе системы используют SQLite, но **совпадение движка не означает совместимость файла или транзакций**:

- Go `core.Store` открывает SQLite через `modernc.org/sqlite`, применяет собственные последовательные schema migrations, foreign keys, WAL и backup перед migration. Его таблицы Secretary описаны в `internal/core/store.go`, `internal/core/phase4_store.go`, `internal/core/secretary.go`, `internal/core/projects.go` и `internal/core/node_registry.go`.
- Pi v1.0.2 публикует собственные Storage implementations, включая Node adapter на `node:sqlite`/`DatabaseSync`; они сериализуют свои durable records и используют свою схему. См. pinned [SQLite Node adapter](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/storage/sqlite/node.ts) и [SQLite storage implementation](https://github.com/earendil-works/pi/tree/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/storage/sqlite).
- У Pi не найден официальный импортёр или schema migration из Secretary DB. Одинаковый `.db` путь не превращает `phase4_attempts`/`deliveries` в Pi conversations/tasks. Совместное размещение отдельных таблиц технически не даст общей транзакции между Go Store и Pi Storage API и не решит атомарный commit Worker command, Pi submit, tool outcome и Server Result.
- Существующий [`secretary-migrate`](../../cmd/secretary-migrate/main.go) вызывает [`migration.Run`](../../internal/migration/migration.go): это контролируемая миграция Secretary Phase 3 → Phase 4, с backup/rollback, конфигом и `user.md`. Это **не** экспортёр в Pi и не следует использовать как Pi-конвертер. Runbook описывает только restore-check текущей Go-схемы: [`docs/always-on-runbook.md`](../../docs/always-on-runbook.md), разделы Backup/Restore-check.

Отсюда есть два разных значения слова «перенести»:

- **Перенести файлы SQLite или открыть старый файл через Pi:** не является миграцией и не подходит.
- **Написать новый TypeScript importer и приложение:** в принципе возможно, но importer должен сохранить/связать Secretary identity и доменную историю, а новое приложение — заново реализовать инварианты и операции, которые не предоставляет Pi.

Переводить можно отдельные проекции. Например, текст беседы и порядок `conversation_entries.seq` можно импортировать как Pi entries с исходными Secretary IDs в metadata. Но исходные ID нужно сохранять как ссылки, а не заменять новыми без таблицы соответствий. Worker, Turn, Attempt, Outcome, Result, Approval, event/delivery cursors, idempotency outcomes, pairing/revocation, Project/Node inventory и security policy требуют явной схемы назначения, проверок ссылок и восстановления поведения. В legacy Phase 3 `worker_bindings` также хранит runtime session ID; Phase 4, напротив, умышленно исключает native IDs из Server Worker record. Эти старые bindings нельзя массово трактовать как безопасно возобновляемые Pi executions.

Есть и состояние вне DB: конфиг/профили/skills/`user.md`, owner secrets, provider credentials, Node credentials, локальные Node `config.json`/`data`, OpenCode native stores, raw logs и реальные Project workspace directories. Их нельзя вывести из Pi conversation import. Не переносить API keys из личного OpenCode store или прежней среды в новый runtime; auth нужно заново одобрить и provision-ить. Workspace-файлы не дублировать и не перемещать автоматически.

### Сравнение вариантов

| Вариант | Что сохраняется / что придётся сделать | Выгоды | Затраты и блокирующие вопросы | Решение |
|---|---|---|---|---|
| **Полная замена Go core**: новый TypeScript Secretary, Pi Durable для agent execution | Надо переписать HTTP/WebSocket API и авторизацию; canonical Conversation/Worker/Turn/Attempt/Result/Approval store; event sequence, dedupe, delivery outbox и recovery; Project/Node registry, enrollment и protocol; MCP, Telegram и clients; config, deployment, monitoring и migration. Pi остаётся библиотекой исполнения/хранения agent state, а не готовым Secretary backend. | Одна реализационная экосистема TypeScript; прямой доступ к Pi API; потенциально меньше межъязыкового glue и richer durable agent/tool model. Выгоды реальны, только если команда хочет порт приложения независимо от Pi. | Большой rewrite до полного parity; experimental API и upgrade surface; отсутствие официального DB importer; перепроектирование idempotency/approvals/security; dual-store failure windows; новая эксплуатация Node.js и credential provisioning; риск потери связи между историей и пользователями/Worker/result; maintenance rollback непрост. | **Не сейчас.** Технически возможно как новый продуктовый backend, но Pi Durable один Go core не заменяет. Нет миграционного основания или измеренного выигрыша, который оправдывал бы переписывание. |
| **Гибрид**: Go остаётся единственным каноническим сервером, Pi Durable исполняет выбранные Worker | Оставить всё из текущего Server DB и API; добавить новый явный HarnessKind/inventory probe и Node.js sidecar либо отдельный runtime service; сохранять Pi IDs/task IDs только в Node-local mapping; Activity/Approval/terminal Outcome проходит через существующий Node protocol и Go core. | Самый ограниченный способ получить данные о Pi checkpoints/tool events без переноса system of record; текущие clients и доменные API не меняются; Pi можно включать только для новых Worker, не трогая OpenCode workers. | Node.js `>=22.19.0` и отдельная установка; собственный RPC/IPC/event bridge; Pi DB и Node outbox нужно согласовать через recovery; профиль/model/MCP/tools/approval надо адаптировать; ни Pi submit, ни Go command/outcome не разделяют commit. Больше complexity, чем ACP, и преимущества ещё не измерены. | **Только изолированный PoC после текущих gates.** Пригоден как экспериментальный runtime, но не как ступень автоматически ведущая к замене Go. |
| **Оставить ACP/OpenCode** | Сохранить `Runtime`/`Resumer` contracts, Go Store и текущую архитектуру; закрыть ticket 31, ticket 33 и `docs/phase4-release-gate.md`. | Минимум миграции и новых компонентов; native persistence/resume уже имеют локальные/private evidence; не меняется доменное владение. | Остаются описанные release/deployment/auth/Telegram gates и ограничения ACP event identity; если конкретный capability gap останется, его надо подтвердить на acceptance, не предполагать. | **Предпочтительно сейчас.** Это единственный вариант без новой миграции и параллельной runtime/storage матрицы. |

### Реалистичный путь к полной миграции — если решение когда-нибудь изменится

Это скорее поэтапная замена приложения, чем миграция «из Go в Pi». Безопасный порядок должен сохранять один writer на доменные записи и делать Pi/TypeScript слой shadow/read-only, пока он не готов.

1. **Инвентаризация и freeze scope.** Зафиксировать источники истины и все таблицы/внешние файлы; собрать счётчики, foreign-key связи, idempotency keys, event sequence/cursors и перечень активных Attempt. Отдельно описать соответствие каждой Go операции и её гарантий целевому TS сервису. Не брать внутренние Pi tables за целевую доменную модель без schema design.
2. **Снимок и доказуемая копия.** Для анализа использовать только отдельную копию. Runbook использует SQLite `.backup` при работающем WAL; Go `core.Open` также делает `VACUUM INTO` snapshot перед собственными migration. Проверять копию через SQLite `integrity_check`, foreign keys и сохранённые исходные counts. Production source, Node LocalStore, workspaces, runtime native stores и credentials оставить нетронутыми.
3. **Однонаправленный shadow importer.** Написать отдельный экспорт/импорт Go → TS/Pi, который не пишет в source и не принимает live requests. Сохранить Person/Conversation IDs и `seq`, внешние message identities, Worker/Turn/Attempt связи, outcome/result/approval history, event order и origin/idempotency metadata. Pi Conversation/task IDs должны жить в таблице mapping рядом с source IDs. Неразрешённая ссылка или неполная запись — ошибка импорта, не silent drop. Сначала не импортировать active attempts как resumable: остановить выдачу новой работы, дождаться terminal/uncertain resolution и явно отметить неизвестные как interrupted.
4. **Перенос интерфейсов, не владения записями.** При желании можно портировать read-only UI/API фасады по одному, пока все writes и state transitions идут через Go canonical service. Telegram/MCP/client auth и Node control отдельно переключать только после matching contract и idempotency. Не отправлять одни mutation параллельно в Go и TS «для синхронизации»: без общей транзакции двойная запись не безопасна.
5. **Shadow сравнение.** На read-only копии сравнить counts, связи, conversation order, terminal statuses, Result↔Turn/Attempt, Approval decisions, scope/revocation и события. Для API shadow traffic сравнивать ответы, не выполнять tools или внешние side effects повторно.
6. **Финальная синхронизация и fencing.** Cutover — только после drain. Остановить приём новых writes, остановить scheduler/Node dispatch, выяснить статус каждого active attempt, сделать согласованный snapshot с финальным delta, проверить counts/hash/links, затем поднять ровно один write leader — новый service. Старый Go binary оставить остановленным и его DB read-only. Решить заранее, как входящие Telegram/client events буферизуются и затем replay-ятся через сохранённые idempotency IDs.
7. **Закрывать parity постепенно.** Сначала read-only и тестовые synthetic conversations, затем ограниченная группа новых Worker, затем остальные каналы/пользователи; legacy активные Workers оставлять на прежнем backend до завершения либо переводить отдельной процедурой, а не менять immutable binding в середине Turn.

**Ограничение rollback:** до того, как новый service примет записи, rollback прост: остановить его и вернуть Go на неизменённый исходный DB после проверки. После принятия новых mutation эта старая DB уже отстаёт. Слепое переключение обратно потеряет новые входы/results; требуется проверенный TS → Go reverse importer или durable journal/delta replay с сохранением idempotency и event IDs. Такого механизма сейчас нет. Нельзя обещать безусловный rollback после cutover, если не реализовать и не проверить обратную синхронизацию. При ambiguous external tool side effect сначала требуется reconcile; восстановление старой DB само по себе не доказывает, что повтор безопасен.

### Что даст маленький decision spike

До обсуждения большого rewrite достаточно **одного изолированного spike**, но он должен проверять не только Pi prompt demo:

- В отдельном временном окружении и только на de-identified копии DB, предоставленной/одобренной владельцем, исследовать, можно ли детерминированно экспортировать одну Conversation с сохранением исходных IDs/order и связанных Worker/Turn/Attempt/Outcome/Result/Approval ссылок. Production-файлы не открывать для записи.
- Зафиксировать собственную минимальную TS доменную схему (не выдавать Pi `Task` за Secretary `Worker`) и пройти один fake-provider lifecycle: idempotent inbound → Worker/Turn/Attempt → fake tool/approval pause-and-resume → один terminal outcome/result → replay того же idempotency key.
- Сохранить и повторно открыть Pi Durable v1.0.2 SQLite store; проверить, что Pi conversation/task и TS domain records согласованы после штатного close/reopen. Вызовы не должны иметь сетевых или реальных tool side effects.
- Импорт считать успешным только при равных исходным counts по выбранному срезу, отсутствии потерянных/осиротевших ссылок, совпадающем порядке истории, отсутствии второго Result/side effect при replay и подтверждении, что source DB и внешний runtime state не изменились.

**Результат spike:** он может определить стоимость полного rewrite и подтвердить применимость SQLite/Pi API; он не докажет production durability, все crash windows, конкурентность, Telegram/API security, реальный provider auth или безопасный rollback. Если для выполнения критических требований приходится заново строить transactional lifecycle/approvals/outbox вокруг Pi, это подтверждает, что Pi — execution substrate, а не замена system of record. Spike не следует начинать до отдельного разрешения и закрытия нынешних OpenCode gates; в этом исследовании он не запускался.

### Первоисточники к этому разделу

Ссылки на код Pi Durable ниже привязаны к git commit `cd32f7725fdbddbaecdff5b1e68491563394e0ca`, который указан как `gitHead` в exact npm metadata версии `1.0.2`. Npm metadata также фиксирует tarball и его integrity, поэтому выводы относятся к конкретному опубликованному артефакту, а не к меняющейся ветке `main` или `latest`:

- [Официальная npm metadata `@earendil-works/pi-durable@1.0.2`](https://registry.npmjs.org/@earendil-works/pi-durable/1.0.2) — версия, `gitHead`, Node engine, exports и зависимости.
- [Опубликованный tarball v1.0.2](https://registry.npmjs.org/@earendil-works/pi-durable/-/pi-durable-1.0.2.tgz) — SHA-512 `pDh8eMSSFIVOTh0H53KhtjaS/XFrulEVREN8wnY8vtbYY0srioZ2impajvkXwoZcG6pRgbPz/WVatZLyk5TIbg==` согласно metadata.
- [README на закреплённом commit](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/README.md), [публичные exports](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/index.ts), [Node SQLite adapter](https://github.com/earendil-works/pi/blob/cd32f7725fdbddbaecdff5b1e68491563394e0ca/packages/durable/src/storage/sqlite/node.ts).
- Внутренние детали Go и deployment подтверждаются путями и символами, указанными в таблице выше; миграционные границы — [`cmd/secretary-migrate/main.go`](../../cmd/secretary-migrate/main.go), [`internal/migration/migration.go`](../../internal/migration/migration.go), [`docs/always-on-runbook.md`](../../docs/always-on-runbook.md).
