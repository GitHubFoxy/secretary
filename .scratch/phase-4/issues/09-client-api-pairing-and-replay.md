# Client API, pairing and replay

Type: task
Status: ready-for-human
Blocked by: 01, 02, 04, 06, 08

## Work

Стабилизировать общий server Client contract для Web, Telegram, Pi и CLI.

- Реализовать Client pairing, device identity, scopes, revoke и отдельные credentials, не совместимые с Node token.
- Реализовать Conversation API, Worker/Turn/activity API, Approval actions, Projects, Nodes и Client management.
- Дать ordered replay по `after_seq` и WebSocket live delivery с атомарной replay boundary.
- Скрыть legacy `/v1/tasks` и `task_id` от новых Clients, оставив их только временным migration/diagnostic compatibility surface.
- Возвращать server acknowledgement с `message_id`, `entry_seq`, state, optional `worker_ref` и `turn_id`.
- Применять server authorization и idempotency к каждому side-effecting endpoint.
- Не помещать Node tokens, callback capabilities или secrets в Client responses, Conversation или activity.

## Acceptance

- Web, Telegram и будущий Pi используют один API и не содержат собственной domain logic.
- Pair, reconnect, replay и revoke работают без дублей и без повторного запуска Worker.
- New API не имеет Task endpoints или `task_id` в обычном response.
- Client не может вызвать Node-only protocol напрямую.
- Approval actions и Worker message/cancel/close проходят через server state.
- Revoked Client теряет доступ к чтению и изменению state.
- API contract покрыт integration tests с duplicate requests и reconnect.
