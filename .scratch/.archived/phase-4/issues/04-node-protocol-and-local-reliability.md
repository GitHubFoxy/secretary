# 04 Node protocol and local reliability

Type: task
Status: resolved
Blocked by: 01, 02, 04a

## Work

Перестроить Execution Node как отдельный outbound runtime с typed protocol и локальной защитой от потери сети. Использовать static HarnessInstance contract из 04a, не изобретать новый inventory schema.

- Определить authenticated typed WebSocket protocol для handshake, heartbeat, inventory, Dispatch, Activity и terminal Attempt events.
- Реализовать server→Node commands: Dispatch, Cancel, Steering, Resume и generic `respond_worker { request_id, response }`.
- Каждая command получает `command_id`; Node durable-сохраняет claim, processing state и outcome до запуска process.
- Повтор той же command возвращает сохранённый outcome и не запускает второй process или Attempt.
- Хранить native runtime session IDs и `worker_ref + turn_id + attempt_id` mappings только на Node.
- Реализовать durable local outbox для unsent normalized activity и terminal AttemptOutcome events до server acknowledgement.
- На reconnect отправлять buffered events и не менять immutable Worker binding.
- Убрать любые server callback capabilities из Worker envelope.

## Acceptance

- Повтор Dispatch/Cancel/Steering/Resume/respond command с тем же `command_id` не запускает вторую работу.
- Claim записывается до side effect. Неопределённое состояние после crash завершается explicit interruption, а не новым process.
- Node теряет сеть после завершения harness и после reconnect доставляет buffered activity и AttemptOutcome.
- Server получает terminal event через authenticated Node connection, даже если harness не знает адрес server.
- Node restart восстанавливает command dedupe table и local session mappings.
- Worker остаётся на исходном Node/HarnessInstance при offline, draining или reconnect.
- Protocol tests используют HarnessInstance fixtures из 04a и проверяют sequence, replay, auth failure и malformed messages.
