# Bootstrap the Go core and durable state

Type: task
Status: resolved
Triage: ready-for-agent

Spec: ../spec.md

## Work

Create the Go module and SQLite-backed Secretary server foundation. Implement migrations and repository operations for Person, Personal Conversation, Conversation entry, Task, Worker binding, Attempt, Result, inbound deduplication and rotatable Secretary capability.

Expose the Task and Attempt state machine from the spec through an application boundary. Cover state transitions, Result idempotency, `dispatch_failed`, `interrupted` and closure with deterministic tests.

## Answer

Реализованы Go module, `secretaryd` bootstrap и SQLite store для Person, Personal Conversation, ordered Conversation entries, inbound deduplication, Task, Worker binding, Attempt, Result и rotatable Secretary capability.

Store реализует выбранный state machine, idempotent Result и безопасный Task closure. Deterministic SQLite tests покрывают inbound deduplication, ordering Result, dispatch retry, closure active Attempt и capability rotation.

Проверка: `mise exec -- go test ./...`, `mise exec -- go vet ./...` и краткий запуск `go run ./cmd/secretaryd` прошли успешно.
