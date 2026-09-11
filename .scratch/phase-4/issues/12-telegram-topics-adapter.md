# Telegram Topics adapter

Type: task
Status: ready-for-human
Blocked by: 06, 08, 09

## Work

Добавить Telegram как первый channel adapter с одной Personal Conversation и Topics для Workers.

- Подключать Telegram из Web через одноразовый code или deep link, без ввода server token в обычном чате.
- Ограничить MVP одним владельцем через allowlist и polling-first transport.
- General chat использовать для Secretary и его full/important stream.
- Создавать отдельный Topic для каждого Worker и направлять туда acknowledgement, compact status, Approval, failure, completion и Follow-up.
- Сопоставлять Topic с `worker_ref` в server state и сохранять mapping durable.
- Дедуплицировать Telegram updates по update ID.
- Фильтровать noisy activity до удобного текста, оставляя rich tool cards в Web/Pi.

## Acceptance

- Web и Telegram видят одну Personal Conversation.
- General chat корректно отображает Secretary turn и его stream/notifications.
- Каждый Worker получает правильный Topic, а Follow-up внутри Topic возвращается тому же Worker.
- Approval, `needs_input`, failure, completion и offline Node не теряются.
- Повторный Telegram update не создаёт второй message, Worker, Turn или Result.
- Неавторизованный user не может читать или менять Secretary state.
- В Telegram не попадают Node token, channel secret или raw chain-of-thought.
