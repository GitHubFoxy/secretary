# 03: общий прямой ввод Worker и durable очередь /q

Type: task
Status: ready-for-agent
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

Пока отсутствует.

## Comments

Server владеет очередью. In-memory runtime queue и переименование кнопки Follow-up не удовлетворяют ticket.
