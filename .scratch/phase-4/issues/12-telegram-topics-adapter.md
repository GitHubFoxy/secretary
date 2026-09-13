# 12 Telegram Topics adapter

Type: task
Status: resolved
Blocked by: 03a, 06a, 08, 09

## Work

Добавить Telegram как первый channel adapter с одной Personal Conversation и Topics для Workers.

- Подключать Telegram из Web через одноразовый code или deep link, без ввода server token в обычном чате.
- Ограничить MVP одним владельцем через allowlist и polling-first transport.
- General chat использовать для Secretary и его compacted/throttled stream.
- Создавать отдельный Topic для каждого Worker и направлять туда acknowledgement, compact status, readable activity, Approval, failure, completion и Follow-up.
- Агрегировать и throttle-ить Secretary text deltas и tool events, не создавая отдельное Telegram message на каждый event.
- Не отправлять raw Worker events. Собирать readable activity batches вместо спама каждого delta/tool event.
- Сопоставлять Topic с `worker_ref` в server state и сохранять mapping durable.
- Дедуплицировать Telegram updates по update ID.

## Acceptance

- Web и Telegram видят одну Personal Conversation.
- General chat корректно отображает Secretary turn без одного Telegram message на каждый delta или tool event.
- Каждый Worker получает правильный Topic, а Follow-up внутри Topic возвращается тому же Worker.
- Worker Topic показывает compact status и readable activity, а не raw event spam.
- Approval, `needs_input`, failure, completion и offline Node не теряются.
- Повторный Telegram update не создаёт второй message, Worker, Turn или Result.
- Неавторизованный user не может читать или менять Secretary state.
- В Telegram не попадают Node token, channel secret или raw chain-of-thought.
