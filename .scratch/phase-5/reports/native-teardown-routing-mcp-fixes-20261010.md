# Исправления native teardown, production Node routing и ACP MCP wire

Исходный integration snapshot: `d76c372`. Исправления выполнены в отдельной ветке `p5/cc-close`; пользовательские изменения `spec/THE spec.md` и `spec/memory.md` не затронуты.

## Claude Code Close

Публичный executable fixture из `/private/tmp/p5-standards-descendant-test.go` воспроизвёл проблему: native успешный Result уже прочитан, но дочерний `sleep 30` сохраняет stdout/stderr, поэтому Close не возвращается за 200 мс. Red: `go test ./internal/node -run '^TestReviewClaudeCloseWithInheritedOutputPipe$' -count=1` — FAIL до исправления.

Claude запускается в собственной Unix process group. Close, отмена context, отмена блокирующей stdin-записи и завершение чтения stdout используют одно завершение этой группы через `sync.Once`. Закрываются stdin/stdout; `Cmd.WaitDelay` ограничивает stderr drain 250 мс. Группа завершается до `cmd.Wait`, пока native PID ещё принадлежит этому процессу; повторный Close не повторяет сигнал. Close ждёт `done` максимум пять секунд и возвращает ошибку при неудачном teardown. Существующий watcher публикует failed Outcome при ошибке Close.

Regression проверяет через Runtime/Session чтение успешного native Result, bounded Close, отмену lifetime context, отсутствие повторного terminal и отсутствие живого дочернего процесса. Проверка допускает только уже завершённый zombie до его системного reap. Сигнал направлен только в созданную runtime process group. Процессы, сознательно покинувшие эту группу, не являются управляемыми потомками этой сессии; inherited pipes всё равно не удерживают Close.

## Production Node routing

Предыдущее исправление `internal/webapi/nodes.go` осталось в snapshot. Production `rootHandler` обходил его, передавая все Node HTTP requests напрямую ServerManager. Поэтому owner GET `/v1/nodes` возвращал 401.

Удалён лишний обход: HTTP routes проходят webapi, которая выбирает owner/client/admin/enrollment authority. `/v1/nodes/connect` сохраняет отдельный protocol handler. Публичный rootHandler regression сначала подтвердил owner 401, затем проверил owner read 200, отказ anonymous/invalid cookie/invalid admin, отсутствие admin доступа через owner cookie и сохранение admin Bearer read. Существующий WebSocket handshake проверяет действительную Node capability и отказ Client credential; fixture подключена к production webapi routing.

## ACP MCP wire

При отсутствии `args`/`env` Start/Resume отправляли JSON null. Live investigation сообщила silent MCP drop в ACP1.12. Wire normalization теперь копирует серверы и отправляет наружный список, args и env как массивы, не меняя исходный StartRequest. Она применяется непосредственно в обоих session/new и session/load; поэтому не зависит от того, сохранил ли upstream clone пустой slice.

Публичный ACP process fixture отклоняет null по wire schema. Red: `TestACPRuntimeMCPArraysOnStartAndResume/Start/omitted` и `/Resume/omitted` — RPC -32602 до исправления. Проверяются nil outer list, omitted args/env, explicit empty arrays, nonempty values и сохранение исходного запроса.

## Проверки

- `go test ./internal/node -run 'Claude|TestACPRuntimeMCPArraysOnStartAndResume|WorkerMCP' -count=1` — PASS.
- Тот же targeted набор с `-race` — PASS.
- `go test -race ./cmd/secretaryd -run 'TestRootHandlerOwnerNodeAccess|TestNodesConnect|TestControlRoutes' -count=1` — PASS.
- `go build ./...` — PASS.
- `git diff --check` — PASS.

Это проверка исправлений через публичные fixtures. Она не является полной live acceptance native MCP или всей Phase 5. Claude quota403 по-прежнему блокирует native Claude acceptance; независимый live Codex прогон требует собственных свидетельств.
