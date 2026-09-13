# 08 Approval and input round trip

Type: task
Status: resolved
Blocked by: 04, 06a, 06b

## Work

Сделать полный Worker-originated путь Approval и `needs_input` через Node и Clients.

- Node adapter отправляет permission request или user input request с durable `request_id`.
- Server хранит Approval, связывает его с Worker, Turn, Attempt, Node и Project и публикует audit event.
- Client может Approve, Deny или передать input. Server направляет результат Node command `respond_worker { request_id, response }`.
- Node передаёт typed response нужному harness runtime и дедуплицирует повтор.
- Approval переводит Attempt/Worker в `waiting_approval`, input request в `needs_input`.
- Trusted local auto-approval остаётся явной policy с audit event и не применяется автоматически к remote Node.
- Secretary не получает `request_approval` tool и не выполняет machine operation вместо Worker.

## Acceptance

- Permission request проходит `Worker/harness → Node → server → Client`.
- Approve, Deny и `needs_input` проходят обратно `Client → server → Node.respond_worker → Worker/harness`.
- Повторный response по тому же `request_id` возвращает установленное состояние и не повторяет machine action.
- Approval и input остаются durable после server или Node reconnect.
- Expired, denied и revoked cases видимы и не запускают работу повторно.
- Trusted local mode включается только явной policy и записывается в audit.
- Ни один Client не получает Node credentials или callback capability.
