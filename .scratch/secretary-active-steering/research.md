# Исследование: уточнение активному Secretary

Дата: 2026-10-11. Проверен HEAD `1f7f58fd6c48514aded7ed9aada9ddc783432d9e`. Исследование не меняет runtime, production или пользовательские файлы.

## Вывод

Причина задержки находится в durable ветке `Runtime.HandleMessage`: она безусловно создаёт queued Secretary turn и выходит до `Session.Steer`. Простое добавление вызова steering недостаточно: durable turn сейчас имеет ровно один `input_id`, а подтверждения, ответы, Worker origins и completion guard проверяют именно его. Нужны несколько неизменяемых input identities внутри одного исполнения Secretary и явный durable результат доставки каждой identity. [Runtime](../../internal/secretary/runtime.go), [тип turn](../../internal/core/secretary_types.go), [origin authorization](../../internal/core/secretary_reply.go), [completion](../../internal/core/secretary_completion.go).

Предлагаемый вариант: additive registry `secretary_inputs`, общий последовательный обработчик входов и terminal для одной Secretary identity, steering существующего активного исполнения. Ни новый Secretary prompt, ни `/q`, ни cancel+новый prompt не заменяют обычное уточнение. Это соответствует уже записанному продуктовому контракту Steering message. [Runtime contracts](../../docs/architecture/runtime-contracts.md), [GLOSSARY](../../GLOSSARY.md).

## Проверенные первичные источники

1. Durable `HandleMessage` сначала распознаёт `/q` через широкий `HasPrefix`, затем для любого ввода вызывает `QueueMessage`/`EnqueueSecretaryTurn`. После этого `startNextDurable` не запускает очередь, пока `r.busy`. Non-durable ветка умеет `Session.Steer`, но при `false` молча ставит текст в память; это также не соответствует явной ошибке неподдерживаемого steering. [Runtime](../../internal/secretary/runtime.go), [enqueue](../../internal/core/secretary.go).
2. HTTP сначала сохраняет `AppendInbound`, а затем запускает `HandleMessage` отдельной goroutine. HTTP outcome `saved` не содержит outcome steering; ошибку goroutine обработчик игнорирует. Порядок запуска goroutines не является порядком Conversation entries. Нужна обработка по canonical `entry.Seq`, а не по моменту захвата mutex. [HTTP messages](../../internal/webapi/server.go), [AppendInbound](../../internal/core/store.go).
3. Telegram General поступает через тот же HTTP client; direct Worker topic идёт по отдельному Worker-first пути. Поэтому worker-topic live acceptance не доказывает General steering. [Telegram ingress](../../internal/telegram/adapter.go), [production wiring](../../cmd/secretaryd/telegram.go), [Worker lifecycle](../../internal/ctl/worker_lifecycle.go), [прошлая приёмка topic steering](../phase-5/reports/codex-telegram-acceptance-20261010.md).
4. `consumeRuntimeResult` очищает `busy`, `activeTurnID` и `activeInputID` перед durable completion commit. Новая доставка не должна пересекаться с этим переходом. `runPrompt` добавляет одну server-issued пару IDs в prompt. [Runtime](../../internal/secretary/runtime.go).
5. Authorization новых replies/origins, stream suppression, binding MCP launch и required-reply completion сравнивают переданный input с единственным `secretary_turns.input_id`. Старую identity нельзя заменить новой: уже начатые tool calls и существующие origin links должны остаться валидными. [Replies/origins](../../internal/core/secretary_reply.go), [MCP binding](../../internal/core/secretary_mcp_observation.go), [completion guard](../../internal/core/secretary_completion.go).
6. Repo Codex adapter разрешает только native outcome `injected`. `startedNewTurn` вызывает best-effort Cancel и явную ошибку reconciliation; idle rejected. Есть fixture и opt-in native safe-tool test. [Adapter](../../internal/node/acp_runtime.go), [tests](../../internal/node/codex_runtime_test.go).
7. Установленный на omarchy `@agentclientprotocol/codex-acp` 1.12.0 сериализует steering по arrival внутри session. `performSteeringRequest` вызывает Codex `turn/steer`; при отсутствии active turn или завершении во время вызова переходит к `startNewTurnFromSteering` и возвращает `startedNewTurn`. Следовательно, серверный gate не устраняет гонку с native terminal; receipt не означает доказанное применение моделью. Проверено read-only SSH в owning source `/home/coder/.local/share/secretary/toolchain/codex-acp/1.12.0/node_modules/@agentclientprotocol/codex-acp/dist/index.js`, строки 33320–33444; локальная точка вызова: [acpSession.Steer](../../internal/node/acp_runtime.go).
8. Recovery текущего Store помечает active Secretary turn interrupted; оно не воспроизводит незавершённый prompt автоматически. Этот принцип нужно сохранить и для steering с неизвестным receipt. [Recovery](../../internal/core/secretary_api.go), [context/recovery tests](../../internal/core/secretary_context_test.go).

## Разбор наблюдаемого случая

Данные [исходной диагностики сервера и native transcripts](incident.md), переданные в это исследование: первый input принят `2026-10-10T15:39:26.339Z`; уточнение принято `15:39:35.227Z`, поставлено в очередь `15:39:35.248Z`. Worker создан только `15:40:11.907Z`. Первый Worker prompt получен `15:40:14.934Z`, завершён `15:40:21.167Z`. Первый Secretary завершён `15:40:17.842Z`; второй native Secretary prompt получен `15:40:17.917Z`; новый Worker Attempt с уточнением получил prompt `15:40:38.275Z`, завершён `15:40:42.946Z`.

Secretary turns: `stn_c48f7fdce234642baf167c7c051ee2a1` и `stn_2ed2f583173f253782efdb066c2e91db`. Worker reference один: `wrk_cbb02a53c0050723fa5151b929e293d5`. Это исходная Attempt и отдельный Follow-up с уточнением, а не дубликат одного Result. Уточнение уже существовало до создания Worker, поэтому основной дефект — очередь Secretary, не отсутствие native Worker steering. Механизм воспроизводится отдельным тестом настоящего `HandleMessage`; fixture сохраняется в [diagnostic/](diagnostic/). Исследование не повторяло модельные вызовы и не обращалось к посторонним transcript histories.

## Варианты и выбранное решение

| Вариант | Оценка |
|---|---|
| Вызывать `Steer(text)` и менять `activeInputID` | Отклонён: новая identity не проходит Store authorization; смена старой ломает in-flight tools, origins и guard. |
| Cancel и создать новый Secretary turn | Отклонён: это другой lifecycle, возможны уже выполненные действия; same-turn steering подменяется retry. |
| Направить General сразу активному Worker | Отклонён: сервер угадывает Task и отбирает у Secretary решение о маршрутизации. |
| Добавить per-input registry внутри существующего исполнения | Выбран: сохраняет durable identity, родство replies/origins и один native active turn при confirmed injection. |

Основания выбора: server authority, точные origin identities и запрет скрытой очереди/нового исполнения уже закреплены в [runtime contracts](../../docs/architecture/runtime-contracts.md); ограничения исходной single-input реализации подтверждают [reply authorization](../../internal/core/secretary_reply.go) и [completion](../../internal/core/secretary_completion.go).

## Рекомендуемый контракт для реализации

### Durable acceptance и порядок

- Создать `secretary_inputs`: `input_id` PK; `identity_id`; `conversation_entry_id` UNIQUE; canonical `entry_seq`; `body`; `mode` (`normal`/`queued`); nullable `secretary_turn_id`; `delivery_state`; generation target; timestamps; error code. Одно внешнее inbound identity получает один registry input. Одинаковые тексты с разными identities остаются отдельными сообщениями.
- Сохранять registry input вместе с inbound entry в одном Store transaction. Legacy `HandleMessage(text)` оставить только как compatibility seam с явным synthetic inbound identity для тестов/внутренних вызовов; production передавать entry identity, а не голый текст.
- Один dispatcher читает pending inputs по `entry_seq`. HTTP больше не создаёт independent delivery goroutines. HTTP `202 saved` остаётся receipt хранения; доставка/ошибка появляется отдельным durable event, доступным Web/Telegram.
- `/q`, `/q `, `/q\n`, `/q\t` используют точные границы общего parser: prefix удаляется, пустая команда rejected, `/query` — обычный текст. Queue FIFO запускается только в idle. Runtime pairing backlog — видимая pending delivery, а не false steering success.

Это рекомендации, а не существующая schema. Точки изменения: [AppendInbound](../../internal/core/store.go), [HTTP ingress](../../internal/webapi/server.go), [HandleMessage](../../internal/secretary/runtime.go); точный Worker parser уже есть в [MessageWorker](../../internal/ctl/worker_lifecycle.go).

### Активное исполнение и receipt

- Idle Secretary: normal input начинает новый durable turn, исходный `secretary_turns.input_id` остаётся immutable primary identity. Active Secretary: attach input к текущему turn, подготовить envelope с **этими же turn ID и новой input identity**, текстом уточнения и кратким правилом «уточнение относится к текущей работе; используй точную identity для действий по нему».
- Состояния steering delivery: `prepared` (ещё не отправлен), `sending` (durable intent committed перед RPC), `injected` (подтверждён native receipt), `failed` (доказанный отказ), `unknown` (timeout/disconnect/неоднозначный новый native turn). `injected` не переименовывать в «модель применила»: это требуется доказать изменённым ответом/действием в live acceptance.
- Сериализовать начало delivery и обработку terminal единым per-identity gate. На время bounded RPC удерживается право ownership перехода, но **не SQL transaction, не `r.mu`, не блокировка MCP Store operations**. Native activity/tool callbacks продолжают идти. Terminal, пришедший во время RPC, сохраняется для завершения после receipt; RPC ограничен timeout, например 10 секунд.
- Если terminal уже выиграл до `sending`, признать Secretary idle и начать обычный новый turn; не утверждать, что это active steering. Если RPC вернул `startedNewTurn`, сохранить unknown/error, выполнить существующий best-effort Cancel и не запускать повторный Prompt. False/unsupported/offline также не превращать в queue.
- При `failed`/`unknown` добавить один canonical понятный error entry с input identity; HTTP saved/Telegram reaction не должны скрывать ошибку. Повтор inbound identity возвращает текущий receipt и не делает новый RPC.

Эти изменения закрывают переходы, которые сейчас разделены между [runtime receipt/terminal](../../internal/secretary/runtime.go) и [Codex steering](../../internal/node/acp_runtime.go). Server gate обеспечивает порядок собственных переходов; residual native fallback остаётся явным reconciliation outcome, описанным выше.

### Replies, acknowledgement, origins и observer

- Store authorization проверяет membership пары `(secretary_turn_id,input_id)` через registry и person/capability/generation. Primary input и ранее injected identities остаются валидными. Текущий `sending` input получает provisional authorization для tools на той же generation/turn: callback может поступить до сохранения receipt, и ожидание `injected` внутри tool handler создаст гонку или deadlock. `failed`/`unknown` закрывают provisional право на новые actions; уже принятые tool actions/origins сохраняются и требуют reconciliation, не отменяются задним числом. Старые origins/replies не переписываются.
- ACK остаётся отдельным idempotent progress slot на каждую точную пару; повтор текста возвращает ту же запись, другой текст конфликтует. Уточнение не требует второй многословной «делегировал» фразы. ACK не удовлетворяет final/required-reply guard.
- Не подменять единственную `activeInputID` задним числом. Хранить primary identity и ordered confirmed inputs отдельно. Tool calls со старой pair проверяются по membership, а late activity без явной input identity остаётся turn-level activity; сервер не приписывает её последнему input по времени.
- В legacy один terminal final публикуется максимум один раз: shared canonical reply entry связан таблицей coverage со множеством confirmed inputs этого исполнения, если у них ещё нет самостоятельного canonical reply/origin disposition. Latest confirmed input является основной адресацией entry, остальные сохраняют свои IDs и явную связь с той же entry. Модель получает весь ordered набор и правило учитывать каждый input в совместном ответе. Это accounting одного совместного ответа, а не доказательство семантической полноты по тексту. Нельзя автоматически объявлять предыдущие inputs `superseded`: второй input может добавлять независимый вопрос. Coverage не удаляет уже опубликованный ответ или принятый Worker action. Failed/unknown inputs не покрываются успешным final.
- Для opt-in addressed-v1 **не ослаблять** правило exact `reply_to_user`: каждое independently delivered input требует своего точного addressed final. Простая supersession запись, ACK или Worker Result не удовлетворяют required-reply guard. Чтобы оставить один пользовательский bubble для корректировки в addressed-v1, нужен отдельный явный атомарный адресованный coverage contract; он вне этой минимальной реализации. Политика addressed-v1 должна попросить Secretary адресовать оба input slots, а terminal Store проверяет все confirmed delivered slots. Legacy deployment автоматически на addressed-v1 не переключается.
- `secretary_turn_launches` остаётся binding turn→launch; `BindSecretaryRuntimeTurn` проверяет input membership вместо `t.input_id=?`, не заводит новый launch при steering. Discovery evidence остаётся launch-level. Worker origin links по старой/новой pair сохраняются независимо и materialize один canonical Result, даже если обе pair связаны с одной Worker Turn.

Owning seams: [ack/replies/origins](../../internal/core/secretary_reply.go), [completion](../../internal/core/secretary_completion.go), [MCP binding](../../internal/core/secretary_mcp_observation.go), [native/activity consumer](../../internal/secretary/runtime.go). Указанная coverage схема — выбранная рекомендация, не утверждение о текущем API.

### Secretary idle, Worker active

General остаётся сообщением Secretary. Он получает новый turn, видит canonical Worker binding/status и сам выбирает `message_worker`. При ещё активном Worker существующий lifecycle отправляет steering в тот же Attempt; при idle Worker это Follow-up с новым Attempt. Поэтому критерий «ровно один Result» применим только когда оба native исполнения успели получить уточнение до terminal; уже готовый Result не отменяется и не скрывается. Direct Worker topic сохраняет существующий отдельный путь. [Worker lifecycle](../../internal/ctl/worker_lifecycle.go), [authority/Result contract](../../docs/architecture/runtime-contracts.md).

### Restart и миграция

- Additive migration создаёт registry, backfill по одному primary input на существующий turn без изменения старых IDs, snapshots, origins, Results или runtime policy. Для исторических turns, которые не имеют надёжного inbound mapping, `conversation_entry_id` nullable (UNIQUE только для непустых mappings); не связывать по похожему тексту.
- Prepared normal inputs, для которых native handoff доказанно не начат, можно доставить впервые. `sending` после crash становится `unknown`, без resend. `injected` в interrupted turn не становится новым prompt; queued `/q` сохраняет FIFO и существующую recovery семантику.
- Canonical input/error/ack events replay идемпотентен; Telegram использует существующую durable send identity, не отдельный ad-hoc send. Migration не выполняет модельных вызовов. Перед cutover проверить idle собственных probe sessions; пользовательские active Workers не закрывать.

Текущие гарантии, которые сохраняются: [Secretary recovery](../../internal/core/secretary_api.go), [prompt claims/recovery tests](../../internal/core/secretary_context_test.go), [Telegram canonical acknowledgement delivery](../../internal/telegram/adapter.go), [runtime contracts](../../docs/architecture/runtime-contracts.md).

## Матрица обязательных проверок

| Проверка | Проверяемый результат |
|---|---|
| Real Runtime+Store: active input | Та же Secretary turn, одна Steer, новый input ID; второй Prompt отсутствует. |
| Active `/q`, empty `/q`, `/query` | FIFO только idle; empty rejected; `/query` не теряет prefix. |
| Два HTTP inputs с обратным расписанием goroutines | Native envelopes идут по committed entry seq; identities не перепутаны. |
| Дубликат inbound; одинаковый текст/new identity | Дубликат не делает RPC; новый input делает отдельную доставку. |
| Terminal перед claim / во время RPC / после receipt | Явно определённый lifecycle; late terminal не завершает чужой turn. |
| Unsupported, false, timeout, offline, startedNewTurn | Один durable error; нет скрытого queued turn/retry; unknown остаётся unknown. |
| ACK/reply/action старой и новой identity | Membership валидна; чужая pair rejected; exact repeat идемпотентен; conflict сохраняется. |
| Legacy terminal после correction | Один final с latest confirmed identity; earlier coverage durable; уже выполненный action не теряется. |
| Addressed-v1 с отсутствующим exact reply | Terminal fails guard; ACK/origin/coverage не обходят его. |
| MCP launch generation, stale capability | Same launch при steering; stale generation rejected. |
| Crash prepared/sending/injected | Только доказанно unsent первое исполнение; unknown без resend; recovery error видим. |
| Web+Telegram replay/restart | Нет повторов canonical ACK/error/final, input receipt сохраняется. |
| Native Codex safe tool + clarification | Один native turn, уточнение меняет действие/ответ; не только RPC injected. |
| General при idle Secretary/active Worker | Только решение Secretary; same Worker Attempt при active, Follow-up при idle. |

Расширять существующие тесты, не подменять runtime копией его логики: [Runtime tests](../../internal/secretary/runtime_test.go), [reply tests](../../internal/core/secretary_reply_test.go), [context tests](../../internal/core/secretary_context_test.go), [Codex tests](../../internal/node/codex_runtime_test.go), [HTTP tests](../../internal/webapi/secretary_internal_test.go), [Worker lifecycle tests](../../internal/ctl/worker_lifecycle_test.go). Запустить focused unit/race tests, затем полный Go suite. Native acceptance явно opt-in и с безопасной собственной задачей; production исследование таких задач не создавало.

## Границы и остаточные риски

«Немедленно» означает передачу активному runtime при первой доступной safe boundary, не гарантированную миллисекундную реакцию модели. Native ACP может стартовать новый turn в race; текущий adapter лишь отменяет его после receipt, поэтому нулевые side effects неизвестного нового turn нельзя обещать. Для stronger guarantee отдельная будущая работа должна расширить native extension параметром expected turn/no-fallback, а не маскировать fallback в сервере. [Продуктовый контракт](../../docs/architecture/runtime-contracts.md), [adapter outcome handling](../../internal/node/acp_runtime.go).

Модель может уже совершить действие до применения уточнения; не отменять его молча. Данный тикет исправляет доставку и identity accounting, но не гарантирует отмену прежнего принятого Dispatch и не объединяет уже terminal Worker Results. [Dispatch/Result contract](../../docs/architecture/runtime-contracts.md).
