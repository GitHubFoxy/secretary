# 16 Pi Client integration

Type: task
Status: ready-for-agent
Blocked by: 09

## Work

Подключить Pi как Client после стабилизации общего Client API. Pi не становится Node, Worker harness или новой domain entity.

- Реализовать pairing и отдельный Client credential.
- Показывать одну Personal Conversation с ordered replay и live Secretary stream.
- Дать Worker observe, message, cancel, close, Approval и `needs_input` через server API.
- Открывать полный Worker observer по `worker_ref`.
- Использовать server state для reconnect и не запускать новый Worker при восстановлении клиента.
- Сохранить Pi extensions presentation/client capabilities, не добавляя server-side child tree.

## Acceptance

- Pi проходит общий Client pairing, Conversation replay и live subscription contract.
- Pi видит тот же Worker и Result, что Web и Telegram.
- Reconnect Pi не создаёт duplicate message, Turn, Attempt или Result.
- Pi не получает Node token и не может вызвать Node protocol напрямую.
- Worker activity и Approval flow работают через существующие server endpoints.
- Pi integration не меняет Worker-first domain model.
