# 16 Pi Client integration

Type: task
Status: resolved
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

## Answer

Реализован узкий TypeScript adapter `phase-1/packages/coding-agent/src/secretary/` поверх Go `/v1/*` API. Pairing проходит через pending handoff, owner approval и one-time redeem отдельного Client credential. Conversation, Secretary turn stream и Worker activity используют ordered replay с cursor, WebSocket live delivery, deduplication и reconnect без spawn. Добавлены Worker message/respond, cancel, close, Approval approve/deny, user, Projects и Nodes read APIs.

`SecretaryPresentation` хранит только Pi presentation state, выбранный `worker_ref` и подписки. Он не импортирует Go, не открывает Node protocol, не принимает Node credential и не создаёт child tree. Existing Pi extensions остаются presentation/client capabilities. Граница и правила security описаны в `phase-1/packages/coding-agent/docs/secretary-client.md`.

Добавлены unit tests на pairing, credential isolation, replay order, live duplicate, reconnect cursor, Worker actions, Approval endpoint и auth failure. Проверено: `npm run test --workspace=@earendil-works/pi-coding-agent`, Pi build/typecheck, `go test ./...`, `go test -race ./...`, `go vet ./...`, web tests/build и monorepo import/entry/browser checks.
