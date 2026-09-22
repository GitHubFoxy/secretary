# 03 Pi viewer snapshot and live state

Type: task
Status: ready-for-agent
Blocked by: 00, 02
Contract: `docs/pi-viewer.md`

## Goal

Сделать `pi --experimental secretary` полезным read-only viewer на MacBook Air.

## Work

- Переиспользовать существующий `SecretaryClient` и `SecretaryPresentation`; не создавать новый server-side Pi domain model.
- Добавить server-side limit и cursor для snapshot: выбрать контракт `GET /v1/conversation?before_seq=N&limit=N` или `GET /v1/conversation/tail?limit=N`, плюс limit для `/v1/workers` и `/v1/approvals`. Зафиксировать cursor/tail pagination в контракте.
- Открывать bounded initial snapshot Conversation, Secretary state, Workers и latest Results.
- Snapshot включает terminal context без открытия Worker detail: Worker status, latest Result summary, последнюю безопасную activity summary и pending Approval summary.
- Добавить allowlisted public DTO для каждого viewer endpoint, вместо raw domain structs: отдельный approval summary DTO (`id`, `kind`, `action_summary`, `risk_category`, `state`, `requested_at`, `expires_at`) вместо `[]core.Approval` и фильтр Approvals по Person или Conversation. Generic sanitizer остаётся defence-in-depth.
- Показывать selected Worker details и safe semantic Activity.
- Подписываться на live Conversation, Secretary and Worker events с server-issued cursor.
- При reconnect выполнять replay от последнего confirmed cursor.
- При gap/expired replay выполнять canonical snapshot resync.
- Явно показывать connected, reconnecting, offline и revoked states.

## Non-goals

- Send message, Worker creation/control, approval action, raw ACP output, tool arguments, native session IDs и chain of thought.

## Acceptance

- `pi --experimental secretary --once` печатает snapshot и завершается, не открывая WebSocket.
- Snapshot приходит bounded от server'а, viewer не скачивает всю историю Conversation.
- Server отдаёт viewer surface allowlisted DTO: `node_id`, `harness_instance_id`, `audit_event_id` и прочие raw domain fields в ответах недоступны.
- Approval list содержит только Approvals текущей Conversation.
- Interactive Pi показывает последующие Conversation events, Worker activity и terminal Results.
- Restart Pi и temporary network loss не создают duplicate presentation entries.
- Pi offline во время Worker execution после reconnect видит terminal state.
- UI не раскрывает запрещённые поля.
