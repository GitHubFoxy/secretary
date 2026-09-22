# 03 Pi viewer snapshot and live state

Type: task
Status: ready-for-agent
Blocked by: 00, 02

## Goal

Сделать `pi --experimental secretary` полезным read-only viewer на MacBook Air.

## Work

- Переиспользовать существующий `SecretaryClient` и `SecretaryPresentation`; не создавать новый server-side Pi domain model.
- Открывать bounded initial snapshot Conversation, Secretary state, Workers и latest Results.
- Показывать selected Worker details и safe semantic Activity.
- Подписываться на live Conversation, Secretary and Worker events с server-issued cursor.
- При reconnect выполнять replay от последнего confirmed cursor.
- При gap/expired replay выполнять canonical snapshot resync.
- Явно показывать connected, reconnecting, offline и revoked states.

## Non-goals

- Send message, Worker creation/control, approval action, raw ACP output, tool arguments, native session IDs и chain of thought.

## Acceptance

- `pi --experimental secretary --once` печатает snapshot и завершается.
- Interactive Pi показывает последующие Conversation events, Worker activity и terminal Results.
- Restart Pi и temporary network loss не создают duplicate presentation entries.
- Pi offline во время Worker execution после reconnect видит terminal state.
- UI не раскрывает запрещённые поля.
