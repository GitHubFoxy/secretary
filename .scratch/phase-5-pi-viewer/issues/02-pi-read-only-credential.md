# 02 Pi read-only credential

Type: task
Status: ready-for-agent
Blocked by: 00, 01

## Goal

Выдать Pi отдельный минимальный Client credential для просмотра состояния Secretary.

## Work

- Аудировать существующие `/v1` read endpoints и WebSocket subscriptions, нужные viewer.
- Ввести или подтвердить минимальный набор scopes: Conversation read, Worker read и Approval read только при необходимости отображения summary.
- Сделать pairing/redeem path для отдельного Pi viewer credential.
- Проверить authorization до WebSocket upgrade и при reconnect.
- Документировать private storage credential на MacBook Air и revoke flow.

## Non-goals

- `conversation:write`, Worker message/cancel/close, approve/deny, Node protocol, model control и owner bootstrap credential.

## Acceptance

- Pi credential читает только declared viewer endpoints.
- Попытки написать message, изменить Worker, approve/deny и вызвать Node protocol получают 403 или 404.
- Revoke прерывает активную subscription и запрещает reconnect.
- Pi не хранит bootstrap token, Node credential или Telegram internal credential.
- Regression tests покрывают HTTP и WebSocket authorization.
