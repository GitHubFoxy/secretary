# 09 Client API, pairing and replay

Type: task
Status: resolved
Blocked by: 02, 03b, 06a, 06b, 08, 13a

## Work

Стабилизировать общий server Client contract для Web, Telegram, paired Clients и CLI.

- Реализовать Client pairing, device identity, explicit scopes, revoke и отдельные credentials, не совместимые с Node token; отклонять pairing с отсутствующим, `null` или пустым scope list.
- Формировать allowlisted DTO для credential-authenticated Client responses, не возвращать raw domain structs, credentials, native session IDs, внутреннюю topology, context snapshots, diagnostics, raw ACP, tool arguments/output или скрытые рассуждения. Owner Web session сохраняет только поля, необходимые UI.
- Реализовать Conversation, Secretary stream, Worker/Turn/activity, Approval, Projects, Nodes и Client management APIs.
- Дать ordered replay по `after_seq` и WebSocket live delivery с атомарной replay boundary.
- Добавить `GET /v1/user` и `PUT /v1/user` для чтения и validated atomic update external `user.md` с durable revision.
- Скрыть legacy `/v1/tasks` и `task_id` от новых Clients, оставив их только временным migration/diagnostic compatibility surface.
- Возвращать server acknowledgement с `message_id`, `entry_seq`, state, optional `worker_ref` и `turn_id`.
- Применять server authorization и idempotency к каждому side-effecting endpoint.
- Не помещать Node tokens, callback capabilities или secrets в Client responses, Conversation или activity.
- Terminal Worker Result доставлять прямо в Personal Conversation без дополнительного Secretary model turn.

## Acceptance

- Web, Telegram и paired Clients используют один API и не содержат собственной domain logic.
- `PUT /v1/user` атомарно сохраняет external `user.md`, возвращает новую revision и не ломает active snapshot при invalid input.
- После изменения `user.md` следующий Secretary turn видит новую preference.
- Pairing требует явные scopes; Client credentials не дают Node protocol или невыданные permissions.
- Credential-authenticated Worker list/details/turn responses используют allowlisted DTO; поля, запрещённые для Client, отсутствуют во всём response, включая вложенные slices. Owner Web session contract остаётся целым.
- Pair, reconnect, ordered replay, cursors, deduplication и revoke работают без дублей и без повторного запуска Worker.
- New API не имеет Task endpoints или `task_id` в обычном response.
- Client не может вызвать Node-only protocol напрямую.
- Approval actions и Worker message/cancel/close проходят через server state.
- Terminal Result появляется в Conversation без автоматического нового Secretary turn.
- Revoked Client теряет доступ к чтению и изменению state; revoke завершает активные HTTP/WebSocket subscriptions, запрещает reconnect, а WebSocket authorization проверяется до upgrade.
- API contract покрыт integration tests с duplicate requests и reconnect.
