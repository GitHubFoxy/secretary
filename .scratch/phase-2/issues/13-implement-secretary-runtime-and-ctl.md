# Implement Secretary runtime and secretaryctl

Type: task
Status: resolved
Triage: ready-for-agent
Blocked by: 10, 12

Spec: ../spec.md

## Work

Run one persistent Codex ACP Secretary session on the local Node. Inject a rotatable capability-scoped `secretaryctl` and implement its typed Task lifecycle commands: create, retry Dispatch, close, list and show.

Ensure no Channel adapter or Worker receives generic control-plane authority. Test capability rotation and command authorization at the application boundary.

## Answer

Реализованы `internal/secretary` и `internal/ctl`. Persistent Secretary использует один Local Node session, отправляет обычные сообщения через Steering, а `/q` сохраняет до idle boundary и затем делает новый prompt turn. ACP runtime поддерживает повторные `session/prompt`, Cancel, startedNewTurn и typed activity mapping.

`secretaryctl` реализует `create`, `retry`, `close`, `list`, `show` и `rotate-capability`. Каждая команда проверяет rotatable capability и owner Conversation. `Close` останавливает active Worker через узкий Node boundary. Local dispatch runner подхватывает durable `dispatching` Task, созданный ctl, и передаёт его Node.

`secretaryd` подключает persistent runtime и dispatch runner при `SECRETARY_ACP_COMMAND`, создаёт initial capability для нового owner и сохраняет её hash в SQLite. Fake ACP process и process-level integration test покрывают initialize, session readiness, activity, prompt completion и Steering.

Проверка: `mise exec -- go test ./...`, `mise exec -- go vet ./...` и ручной запуск `secretaryd` с fake ACP прошли успешно.
