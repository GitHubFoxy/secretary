# Prove the first thin vertical slice

Type: task
Status: resolved
Triage: ready-for-agent
Blocked by: 11, 12, 13, 14

Spec: ../spec.md

## Work

Add the approved automated end-to-end proof with temporary SQLite, real Go server and local Node, and fake Codex ACP process. It must cover login, ordered Conversation sync, Dispatch acknowledgement, observer activity, Steering, Stop and terminal Result.

Run one manual smoke against pinned upstream `codex-acp` and local Codex authentication. Record commands and observed behavior in the ticket answer.

## Answer

Добавлен `internal/e2e` test с настоящими Go HTTP/WebSocket server, temporary SQLite и process-level fake ACP. Проверка проходит по внешнему пути: owner login, ordered Conversation sync, Task Dispatch, Worker acknowledgement, observer status, activity, Steering, Stop, `stopping` и terminal canceled Result в Conversation.

Store публикует committed Conversation entries через observer hook, поэтому Result от Dispatcher доходит до уже подключённого Conversation WebSocket. Follow-up создаёт новую Attempt и durable `worker_input` entry только после terminal предыдущей Attempt. Recovery запускается только server startup методом `Store.RecoverInterrupted`: отдельный `secretaryctl` больше не прерывает активные Attempts. Реальный ACP `session/update` envelope и текстовые chunks сохраняются корректно.

Проверки репозитория:

```sh
go test ./...
go test -race ./...
go vet ./...
```

Реальный manual smoke выполнен 2026-09-09 на локальном Codex login, Codex CLI `v0.144.0`, pinned checkout `@agentclientprotocol/codex-acp` `v1.10.0` и модели `gpt-5.6-luna`:

```sh
cd /private/tmp/codex-acp
npm install
npm run build

cd /Users/beruseruko/projects/secretary-v2
PATH=/tmp:$PATH \\
CODEX_PATH=$(command -v codex) \\
CODEX_CONFIG='{"model":"gpt-5.6-luna"}' \\
INITIAL_AGENT_MODE=agent-full-access NO_BROWSER=1 \\
SECRETARY_BOOTSTRAP_TOKEN=boot \\
SECRETARY_ACP_COMMAND=$(command -v node) \\
SECRETARY_ACP_ARGS=/private/tmp/codex-acp/dist/index.js \\
/tmp/secretaryd-real -data-dir /tmp/secretary-real-smoke/state -listen 127.0.0.1:18084
```

Наблюдения:

- `POST /v1/web/session` вернул `201`, сообщение через `POST /v1/messages` принято с `202`.
- Secretary создал Task через capability-scoped `secretaryctl`; реальный Worker получил prompt и вернул `WORKER_SMOKE_OK`.
- SQLite сохранила Attempt `succeeded`, Result `succeeded` с summary `WORKER_SMOKE_OK`, а Conversation получила `worker_result` и ответ Secretary с `worker_ref`.
- Для длинного реального Worker `POST /v1/workers/<worker_ref>/steer` вернул `202 {"injected":true}`. Следом `POST /v1/workers/<worker_ref>/stop` вернул `202 {"status":"cancel_requested"}`; Attempt стал `canceled` и Result записался. Во время остановки observer status был `stopping`, после перезапуска `secretaryd` durable status стал `canceled`.
- Обычный `secretaryctl show` при работающем server не перевёл этот Attempt в `interrupted`.

Pinned fake smoke command:

```sh
SECRETARY_BOOTSTRAP_TOKEN=boot SECRETARY_ACP_COMMAND=/path/to/fake-acp \
  go run ./cmd/secretaryd -data-dir /tmp/secretary-state -listen 127.0.0.1:8081
```
