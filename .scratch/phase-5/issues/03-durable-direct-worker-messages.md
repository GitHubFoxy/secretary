# 03: общий прямой ввод Worker и durable очередь /q

Type: task
Status: claimed
Blocked by: None

## What to build

Пользователь пишет Worker через Web observer или его Telegram topic. Обычное сообщение active Worker направляет текущую работу, `/q текст` немедленно сохраняется и видимо ждёт следующей Attempt. Оба канала и Secretary MCP message_worker используют одну server-owned семантику.

## Acceptance criteria

- [ ] В актуальном Worker-first public path `/q` разбирается один раз, prefix удаляется из prompt; пустой `/q` даёт понятную validation error и не создаёт работу.
- [ ] Обычный input working Worker вызывает steering текущей Attempt; unsupported/offline/неизвестное принятие не превращаются в скрытую очередь или automatic retry. Idle обычный input продолжает прежний Worker binding как Follow-up.
- [ ] `/q` создаёт durable Queued message/Future Follow-up, ответ API и оба канала показывают queued. Во время active Attempt runtime не получает queued prompt и новая Attempt не запускается.
- [ ] После terminal Result очередь FIFO создаёт новый Follow-up с новой Attempt и прежней native session; принятие очереди и переход к delivery устойчивы к crash между ними.
- [ ] Server/Node restart сохраняют очередь и identities, не повторяют неизвестное выполнение или активную Attempt; interrupted Worker сначала восстанавливает прежнюю session по существующему контракту. Если resume невозможно, очередь остаётся видимой с ошибкой.
- [ ] Повтор HTTP/MCP/Telegram input с той же identity не создаёт вторую queued запись/Attempt; разные inputs с одинаковым текстом остаются разными сообщениями.
- [ ] Task closure не исполняет накопленный queue позже: pending записи завершаются явным отменённым состоянием, история сохраняется. Cancel active Attempt не закрывает Worker binding; доставка queue после terminal учитывает этот state.
- [ ] Pending Approval/input response сохраняет request_id routing; `/q` не становится ответом старому Approval. Никакие прямые Worker inputs не добавляются в model context Secretary.
- [ ] Web и Telegram General/topic routing дают один и тот же режим отправки без разных локальных парсеров или legacy Task-based queue обхода.

## Проверка

Public seams: authenticated server HTTP/MCP и channel command/output. Переиспользовать Worker message lifecycle, scopes/idempotency, Telegram routing и continuation tests. Добавить red/green сценарии active `/q`, restart, replay и terminal FIFO на public boundary с controllable native Session fixture. Настоящий cross-harness steering и queue matrix выполняется в 05; этот ticket демонстрируется на имеющемся Codex либо controlled boundary и не зависит от завершения CC spike.

## Answer

Реализован server-owned Worker-first `/q`: общий parser в `WorkerService.MessageWorker` перед request_id routing, durable FIFO записи `worker_queued_messages`, additive `queued_messages`/`action_mode`/`action_message_id` в read/mutation responses. Pending prompt не пересекает runtime boundary до terminal Result. Promotion атомарно сохраняет Turn/Attempt, immutable continuation checkpoint, command intent и принятую Secretary origin identity. Runtime pump использует прежний dispatch/resume путь; restart не повторяет неизвестное выполнение. История имеет состояния pending/delivering/delivered/blocked/canceled и WorkerRef-scoped durable events. Close сначала отменяет очередь, затем активную Attempt; Cancel сохраняет binding.

Проверено:

- Red: `TestWorkerQueueWaitsForTerminalAndPreservesFIFO` не компилировался без нового public API/read model; green проверяет отсутствие steering/delivery во время active, FIFO, replay, новый Turn и stripping prefix.
- WorkerService public tests: restart с сохранением message identity и resume interrupted binding; pending input с request_id не получает queued text; recovery до transport handoff; разные inputs с одинаковым текстом; failed handoff видим как blocked без automatic retry; closure cancel queue; queued Result сохраняет origin после окончания Secretary turn.
- Authenticated owner HTTP: POST `/v1/workers/:ref/message` принимает `/q`, replay сохраняет message ID, GET `/v1/workers/:ref` показывает очередь.
- Controlled external ACP process (`TestPublicQueuedFollowUpRetainsNativeHistory`): idle `/q` по прежней session до/после Node reopen; active held prompt не получает queue до terminal; новый Follow-up вспоминает nonce в той же native session. Fixture не является live доказательством Codex/CC.
- `go test ./internal/core ./internal/ctl ./internal/webapi ./internal/mcp` PASS; `go test -race ./internal/ctl ./internal/webapi ./internal/mcp` PASS. Дополнительный scoped queue race PASS.

Осталось интегрировать вызов `RunQueuedWorkerMessages(ctx)` в main (ticket 01), показать queued/history в Web/Telegram (ticket 04) и пройти настоящую native/topology matrix (ticket 05). Поэтому live gate не объявлен пройденным, ticket остаётся claimed до интеграционной проверки.

## Comments

Server владеет очередью. In-memory runtime queue и переименование кнопки Follow-up не удовлетворяют ticket.
