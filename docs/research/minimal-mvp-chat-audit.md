# Аудит чата Minimal MVP

Дата: 2026-10-09. Локальный checkout `42721bb`; `spec/THE spec.md` имеет пользовательские незакоммиченные изменения. Production omarchy этим аудитом не проверялся. Изменён только этот документ; код, конфигурация, runtime и credentials не менялись.

Область: [Minimal MVP](../../spec/THE%20spec.md#minimal-mvp), строки 158–170: общая переписка, Workers на разных машинах в Codex/Claude Code, прямой ввод через простой UI, active steering по умолчанию и `/q` для очереди, Telegram group topics и бот-администратор. Прочитаны `AGENTS.md`, `GLOSSARY.md`, `docs/agents/domain.md`, `docs/architecture/runtime-contracts.md`; каталога `docs/adr/` нет. Этот документ исследует доставку и представление чата; фактическая готовность удалённых Nodes и native auth требует отдельной проверки.

`done` означает реализацию с проходящей локальной проверкой, `partial` — реализованную часть без полного acceptance, `missing` — конкретный отсутствующий путь. Прохождение fixtures не означает live Telegram/native acceptance.

## Соответствие MVP

| Требование | Статус | Основание |
| --- | --- | --- |
| Одна Personal Conversation между Web и Telegram | partial | Web читает canonical entries; Telegram зеркалит `message.saved` других Clients, Secretary reply и terminal Result. Историческая потеря canonical reply исправлена в текущем checkout и покрыта тестом; live многоканальный acceptance здесь не запускался. [Bridge](../../cmd/secretaryd/telegram.go:193), [зеркало Client input](../../internal/telegram/adapter.go:721), [Web entries](../../web/src/App.svelte:470). |
| Прямое сообщение Worker из Web/Topic | done | Web отправляет `/v1/workers/{ref}/message`; Telegram использует тот же endpoint. Working → Steer, idle → новая Turn/Attempt на прежнем binding, needs_input → ответ по request_id. [Web action](../../web/src/App.svelte:361), [Telegram HTTP client](../../internal/telegram/http.go:237), [MessageWorker](../../internal/ctl/worker_lifecycle.go:546). |
| Immediate steering по умолчанию | partial | Service для working вызывает Runtime.Steer, но Claude Code print runtime явно не поддерживает steering. [Service](../../internal/ctl/worker_lifecycle.go:626), [Claude runtime](../../internal/node/claude_runtime.go:227). |
| `/q` как очередь Follow-up для active Worker | missing | В phase4 MessageWorker нет разбора `/q`; текст передаётся как steering. `/message` и `/follow-up` используют одинаковый service. Legacy `/queue` существует отдельно и не делает phase4 MessageWorker queued. [API](../../internal/webapi/observer.go:457), [working branch](../../internal/ctl/worker_lifecycle.go:626), [legacy queue](../../internal/webapi/observer.go:256). |
| Telegram group topics для Workers | done | Durable mapping создаётся на worker.created, task prompt публикуется в topic; известный topic адресует Worker, General адресует Secretary, неизвестные topics отбрасываются. [Routing](../../internal/telegram/adapter.go:528), [topic creation](../../internal/telegram/adapter.go:832). |
| Бот-администратор и фактические forum permissions | partial | Код вызывает createForumTopic и обрабатывает ошибку транспорта. Права настоящего бота не проверялись; наличие mapping/unit tests не доказывает назначение admin. [Bot API](../../internal/telegram/http.go:182). |
| Telegram форматирование | done | Поддерживаемый HTML subset: bold, headings, inline/fenced code, безопасные HTTP(S) ссылки; escape raw HTML, UTF-16 limit, разбиение длинных сообщений, persisted fallback при parse rejection, restart checkpoints. Это ограниченный Markdown, не полный CommonMark. [Formatter](../../internal/telegram/format.go:19), [delivery](../../internal/telegram/adapter.go:1140). |
| Web Markdown | missing | Conversation body, Results и Secretary stream вставлены Svelte как plain text. `**bold**`, Markdown links и fences показываются исходным текстом. [Conversation](../../web/src/App.svelte:470), [Results](../../web/src/App.svelte:466). |
| Web Worker activity | partial | Есть replay/live cursor, compact tool name/preview/status. Остальные object payloads форматируются JSON и показываются в `<pre>`, а не связным чатом Worker. [Replay](../../web/src/App.svelte:285), [formatter](../../web/src/ui-model.js:86), [observer](../../web/src/App.svelte:483). |

## Конкретные дефекты и ограничения

1. **`/q` не реализован в актуальном прямом Worker input.** Telegram не меняет text перед отправкой `/message`; API передаёт text в MessageWorker; working branch безусловно вызывает Steer. Поэтому `/q продолжи потом` адресует active Worker сейчас и сохраняет prefix в переданном тексте. Кнопка Web «Follow-up» тоже вызывает тот же service и во время working направляет steering. Это вывод из полного текущего пути, а не live acceptance. Уже существующий `TestWorkerServiceMessageLifecycleMatrix` подтверждает working→steer и idle→dispatch, но не содержит `/q` negative fixture. Минимальная проверка: заменить working text этого fixture на `/q later` и проверить, что Runtime.Steer вызван, новой Turn нет; этот аудит тестовые файлы не изменял.

2. **Active Claude Code Worker нельзя steer через нынешний runtime.** `claudeSession.Steer` всегда возвращает `ErrClaudeCodeSteeringUnsupported`; Queue тоже возвращает явную ошибку, поскольку native print session требует новую Attempt. Это не отменяет поддержку idle follow-up через Dispatch/Resume. Минимальная существующая проверка: `TestClaudeCodeSessionSteeringIsExplicitlyUnsupported` в [Claude tests](../../internal/node/claude_runtime_test.go:136).

3. **Web не рендерит Markdown, generic Worker text activity показана JSON.** Ввод/ответ `# Заголовок\n\n**текст**\n\n[ссылка](https://example.com)` отображается в `<p>` с маркерами; activity `{kind:"text",text:"ответ"}` форматируется JSON.stringify и показывается в `<pre>`. Это текущий код представления, не ошибка auth или отсутствие сообщения в durable истории. Минимальная воспроизводимая проверка formatter: `node --input-type=module -e 'import {formatActivityPayload} from "./web/src/ui-model.js"; console.log(formatActivityPayload({payload:{kind:"text",text:"ответ"}}))'`. Визуальный browser acceptance не проводился.

## Secretary не отвечает: различать запись, dispatch и доставку

Исторический инцидент исследован в [35-diagnosis-20261008](../../.scratch/.archived/phase-4/reports/35-diagnosis-20261008.md:9): два Secretary turns завершились succeeded и сохранили Addressed reply, но тогда Telegram bridge пропускал `conversation.entry`. Одновременно оба spawn_worker были отклонены: explicit FX + inherited/explicit `low`, тогда как observed FX reasoning_levels были пустыми. Workers не появились; зависание runtime не было подтверждено. Это историческое доказательство, не результат новой production проверки.

**В текущем checkout canonical delivery path уже исправлен.** [telegramEvent](../../cmd/secretaryd/telegram.go:199) преобразует `conversation.entry` с kind secretary и body в SecretaryTextDeltaEvent. [TestTelegramBridgeDeliversCanonicalAddressedRepliesForEveryTurn](../../cmd/secretaryd/telegram_test.go:468) воспроизводит два turns с addressed_reply_only, без assistant chunks; отправляются ровно два reply, user/worker_result entries не попадают в этот путь. Тест прошёл. Исторический report с `sends=0` нельзя использовать как доказательство текущего локального дефекта. Попал ли fix в фактический production binary, проверяет отдельный audit omarchy.

Адресованный ответ: server-issued turn/input → `RecordSecretaryReply` → canonical EntrySecretary → conversation.entry → bridge text delta → durable pending batch → secretary.turn.finished → Flush → outbox/Bot API. [Запись](../../internal/core/secretary_reply.go:403), [batch](../../internal/telegram/adapter.go:883), [Flush](../../internal/telegram/adapter.go:949). Core opt-in контракт требует точную durable reply identity и проверяет completion evidence; по умолчанию addressed contract выключен согласно [runtime contracts](../architecture/runtime-contracts.md:12). Для нового эпизода «не отвечает» нужно отдельно проверить существование input/turn, lifecycle завершения, canonical reply entry, bridge cursor/outbox и фактический SendMessage. Один indicator typing или пустой Worker list не устанавливает причину.

## Auth, routing и reply path

- Telegram pairing code одноразовый и с TTL; связывает OwnerChat и FromID. После pairing принимаются allowlisted chat+sender. Внешний код не получает server credential. [Pairing](../../internal/telegram/adapter.go:425), [owner checks](../../internal/telegram/adapter.go:460).
- Telegram transport использует private internal Bearer credential для `/v1/messages` и Worker `/message`, `Idempotency-Key=telegram:{update_id}`. [HTTP](../../internal/telegram/http.go:209). Web использует owner session; внешний Client проверяется по scope, Worker lookup ограничен Personal Conversation; spoofed X-Client-ID отвергается. [Worker authorization](../../internal/webapi/observer.go:33), [action identity](../../internal/webapi/observer.go:406). `ScopeWorkerMessage` разрешает `/message`, но не `/follow-up`/cancel/approve.
- Worker spawn → durable worker.created/topic mapping → Task prompt → lifecycle/tools/input notifications. Terminal Result идёт в Worker topic и отдельным зеркалом в General с topic title, dedup по terminal identity; ordinary worker text activity в Telegram намеренно не отправляется. [Events](../../internal/telegram/adapter.go:744), [terminal mirror](../../internal/telegram/adapter.go:817), [activity exclusion test](../../internal/telegram/adapter_test.go:933). Каналы поэтому имеют общий durable Result, но разные live views.
- Pending input/Approval request_id сохраняется в mapping и прикладывается к следующему Worker message, очищается после принятия/resolution. Нормальный follow-up после resolution не становится ответом старому Approval. [Mapping routing](../../internal/telegram/adapter.go:541), [request lifecycle](../../internal/telegram/adapter.go:766).

## Выполненные локальные проверки

Все проверки используют fixtures/temp stores; реальная модель, Telegram отправки и production runtime не запускались.

```sh
go test ./internal/telegram ./cmd/secretaryd -run 'Telegram|WorkerTopic|LongResult|LegacyPartial|OversizedMarkdown|BotAPIChunk|SingleOversized|ResultKeeps' -count=1
cd web && npm test
go test ./internal/ctl ./internal/webapi -run 'TestWorkerServiceMessageLifecycleMatrix|TestWorkerServiceSteeringDedupeKeepsDistinctMessages|TestWorkerActionAPIExposesMessageFollowUpAndApprove|TestWorkerMessageScopeOnlyAllowsMessageRoute|TestTicket09GenericWorkerMessageUsesAuthenticatedBearerClient|TestTelegramInternalCredentialCanSubmitMessageWithoutClientCredential|TestWorkerObserverStatusSteerStopAndActivity' -count=1
go test ./internal/node -run '^TestClaudeCodeSessionSteeringIsExplicitlyUnsupported$' -count=1
```

Telegram/secretaryd, ctl/webapi и Web: PASS; Web 10/10. Native fixture tests, которые требуют opt-in env, не засчитываются как live native acceptance. Отдельный Claude unsupported test подтверждает ограничение adapter; это ожидаемое поведение текущего теста, а не выполнение steering.

Минимальный live acceptance после устранения пробелов: обычный query в General и Web должен дать ровно один canonical reply в обоих каналах; spawn на local/remote Codex/CC должен создать topic; active direct input должен steer, `/q` дождаться idle; idle follow-up должен сохранить binding/history; terminal summary должен появиться один раз в topic и General; Markdown/code длиннее Telegram limit должен оставаться читаемым после разбиения. Для всего MVP такие проверки ещё требуются, scoped unit tests их полностью не заменяют.
