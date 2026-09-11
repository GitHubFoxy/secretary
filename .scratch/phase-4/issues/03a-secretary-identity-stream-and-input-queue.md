# 03a Secretary identity, stream and input queue

Type: task
Status: ready-for-agent
Blocked by: 01, 02

## Work

Реализовать persistent Secretary identity с заменяемым runtime, live stream и строгой последовательностью turns.

- Разделить `secretary.harness`, `secretary.model`, `secretary.reasoning` и `worker_policy.default_harness`.
- Хранить `user.md` как external Markdown file в user data directory с durable revision, validated atomic write и безопасной загрузкой. Context assembly остаётся в 03b, API в 09, editor в 10.
- Обрабатывать ровно один Secretary turn за раз.
- Сохранять обычные входящие сообщения во время active turn в durable ordered queue.
- Публиковать `secretary.turn.queued`, `secretary.turn.started`, `secretary.text_delta`, `secretary.thinking_summary`, `secretary.tool_call`, `secretary.tool_result` и `secretary.turn.finished`.
- Показывать безопасный thinking summary, но не raw chain-of-thought.
- Управлять runtime restart/reload без смены Secretary identity и без silent continuation неизвестного состояния.

## Acceptance

- Restart или смена Secretary runtime сохраняет одну identity и одну Personal Conversation.
- Второе сообщение во время active Secretary turn получает queue position и обрабатывается после текущего turn.
- Workers и их Attempts продолжают выполняться параллельно с Secretary turn.
- Client получает live stream и replay всех Secretary events без повторов.
- Tool call/result явно показывают вызовы Secretary tools и их исходы.
- `secretary.turn.finished` содержит terminal state и error при неуспехе.
- В stream нет raw chain-of-thought.
- `user.md` сохраняется во внешнем файле с новой revision после validated atomic write.
- Invalid Secretary config или invalid `user.md` не заменяет действующий snapshot.
