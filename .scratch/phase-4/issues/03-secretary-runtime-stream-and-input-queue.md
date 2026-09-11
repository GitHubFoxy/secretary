# Secretary runtime, stream and input queue

Type: task
Status: ready-for-human
Blocked by: 01, 02

## Work

Реализовать persistent Secretary identity с заменяемым runtime и полноценным stream основного разговора.

- Разделить `secretary.harness`, `secretary.model`, `secretary.reasoning` и `worker_policy.default_harness`.
- Восстанавливать Secretary context из identity, `user.md`, Conversation summary, recent entries, unseen Worker Results, open Workers, Projects, Nodes, HarnessInstances, Approvals и policy snapshot.
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
- Tool call/result явно показывают `list_workers`, `spawn_worker` и другие Secretary tools.
- `secretary.turn.finished` содержит terminal state и error при неуспехе.
- В stream нет raw chain-of-thought.
- Invalid Secretary config не заменяет действующий runtime snapshot.
