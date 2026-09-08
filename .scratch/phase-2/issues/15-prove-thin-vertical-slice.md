# Prove the first thin vertical slice

Type: task
Status: claimed
Triage: ready-for-agent
Blocked by: 11, 12, 13, 14

Spec: ../spec.md

## Work

Add the approved automated end-to-end proof with temporary SQLite, real Go server and local Node, and fake Codex ACP process. It must cover login, ordered Conversation sync, Dispatch acknowledgement, observer activity, Steering, Stop and terminal Result.

Run one manual smoke against pinned upstream `codex-acp` and local Codex authentication. Record commands and observed behavior in the ticket answer.

## Answer

Добавлен `internal/e2e` test с настоящими Go HTTP/WebSocket server, temporary SQLite и process-level fake ACP. Проверка проходит по внешнему пути: owner login, ordered Conversation sync, Task Dispatch, Worker acknowledgement, observer status, activity, Steering, Stop, `stopping` и terminal canceled Result в Conversation.

Store теперь публикует committed Conversation entries через observer hook, поэтому Result от Dispatcher доходит до уже подключённого Conversation WebSocket. `go test -race ./...` и `go vet ./...` проходят.

Проверенный fake smoke command:

```sh
SECRETARY_BOOTSTRAP_TOKEN=boot SECRETARY_ACP_COMMAND=/path/to/fake-acp \
  go run ./cmd/secretaryd -data-dir /tmp/secretary-state -listen 127.0.0.1:8081
```

Pinned upstream `codex-acp` manual smoke требует локального Codex login и не запускался автоматически в этом deterministic test run. Его команда после установки зависимостей: `SECRETARY_ACP_COMMAND=/path/to/codex-acp SECRETARY_BOOTSTRAP_TOKEN=... go run ./cmd/secretaryd`.
