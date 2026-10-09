# 16 Pi Client integration (historical)

Type: task
Status: resolved
Blocked by: 09
Related: [28: удаление поддержки Pi viewer](28-remove-pi-viewer-extension.md)

Исторический статус: поддержка Pi Client/viewer снята в ticket 28. Этот файл описывает прежнюю реализацию. Его scope и acceptance results являются историческим свидетельством, а не текущими обязательствами по поддержке или планом реализации.

## Former scope

Подключить Pi как Client после стабилизации общего Client API. Pi не становится Node, Worker harness или новой domain entity.

- Реализовать pairing и отдельный Client credential.
- Показывать одну Personal Conversation с ordered replay и live Secretary stream.
- Дать Worker observe, message, cancel, close, Approval и `needs_input` через server API.
- Открывать полный Worker observer по `worker_ref`.
- Использовать server state для reconnect и не запускать новый Worker при восстановлении клиента.
- Сохранить Pi extensions presentation/client capabilities, не добавляя server-side child tree.

## Historical acceptance results

Эти критерии относились только к прежней реализации и больше не являются действующими требованиями.

- Pi проходил общий Client pairing, Conversation replay и live subscription contract.
- Pi видел тот же Worker и Result, что Web и Telegram.
- Reconnect Pi не создавал duplicate message, Turn, Attempt или Result.
- Pi не получал Node token и не мог вызвать Node protocol напрямую.
- Worker activity и Approval flow работали через существующие server endpoints.
- Pi integration не меняла Worker-first domain model.

## Historical implementation evidence

Реализован узкий TypeScript adapter `phase-1/packages/coding-agent/src/secretary/` поверх Go `/v1/*` API. Pairing проходит через pending handoff, owner approval и one-time redeem отдельного Client credential. Conversation, Secretary turn stream и Worker activity используют ordered replay с cursor, WebSocket live delivery, deduplication и reconnect без spawn. Добавлены Worker message/respond, cancel, close, Approval approve/deny, user, Projects и Nodes read APIs. Production Secretary MCP теперь проксирует Worker tools через server-owned runtime endpoint, поэтому отдельный MCP процесс не теряет Node connections и dispatch state. `secretaryd` выбирает bundled `secretary-mcp` рядом с daemon или явный `SECRETARY_MCP_COMMAND`, а web listener bind завершается до запуска persistent Secretary.

`SecretaryPresentation` хранит только Pi presentation state, выбранный `worker_ref` и подписки. Он не импортирует Go, не открывает Node protocol, не принимает Node credential и не создаёт child tree. Existing Pi extensions остаются presentation/client capabilities. Граница и правила security описаны в `phase-1/packages/coding-agent/docs/secretary-client.md`.

Добавлены unit tests на pairing, credential isolation, replay order, live duplicate, reconnect cursor, Worker actions, Approval endpoint и auth failure. Проверено: `npm run test --workspace=@earendil-works/pi-coding-agent`, Pi build/typecheck, `go test ./...`, `go test -race ./...`, `go vet ./...`, web tests/build и monorepo import/entry/browser checks.
