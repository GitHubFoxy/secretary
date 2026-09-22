# 02 Pi read-only credential

Type: task
Status: claimed
Blocked by: 00, 01
Contract: `docs/pi-viewer.md`

## Goal

Выдать Pi отдельный минимальный Client credential для просмотра состояния Secretary.

## Work

- Аудировать существующие `/v1` read endpoints и WebSocket subscriptions, нужные viewer.
- Ввести или подтвердить минимальный набор scopes: Conversation read, Worker read и Approval read только при необходимости отображения summary.
- Запретить pairing без явного scope list на server boundary: пустой `scopes` больше не даёт default полный набор. Учесть, что `SecretaryPairOptions.scopes` в Pi optional и `client.ts` не отправляет поле, если оно не задано, а существующие вызовы парятся без scopes.
- Сделать pairing/redeem path для отдельного Pi viewer credential.
- Проверить authorization до WebSocket upgrade и при reconnect.
- Документировать private storage credential на MacBook Air и revoke flow.

## Non-goals

- `conversation:write`, Worker message/cancel/close, approve/deny, Node protocol, model control и owner bootstrap credential.

## Acceptance

- Pi credential читает только declared viewer endpoints.
- Pairing с пустым scope list отклоняется или даёт явно заданный узкий набор, но не default полный.
- Попытки написать message, изменить Worker, approve/deny и вызвать Node protocol получают 403 или 404.
- Revoke прерывает активную subscription и запрещает reconnect.
- Pi не хранит bootstrap token, Node credential или Telegram internal credential.
- Regression tests покрывают HTTP и WebSocket authorization, read-only scope matrix и revoke активных HTTP и WebSocket streams.
