# 02 Durable events, idempotency and server delivery

Type: task
Status: ready-for-agent
Blocked by: 01

## Work

Сделать server event log и delivery state основанием для at-least-once processing.

- Сохранять normalized events с event ID, monotonic sequence, aggregate, source, correlation и causation metadata.
- Добавить idempotency для inbound messages, lifecycle actions, Worker/Turn/Attempt creation, internal `retry_attempt`, AttemptOutcome и final Result.
- Гарантировать один логический Conversation entry и один logical Result при повторной доставке.
- Реализовать server delivery outbox для Conversation entries, Results, Approvals и важных notifications.
- Дать Client ordered replay по `after_seq` и атомарную границу между replay и live delivery.
- Сохранить distinction между server outbox и Node-local runtime outbox из ticket 04.
- Не создавать отдельный Conversation entry или Secretary model turn при приёме terminal Worker Result.

## Acceptance

- Повторный inbound с тем же dedupe key не создаёт новую Conversation entry, Worker или Turn.
- Повторный lifecycle action возвращает прежний durable outcome.
- Повторный Attempt terminal event возвращает существующий AttemptOutcome.
- Повторный final Result не создаёт новый Result или Conversation entry.
- Terminal Result принимается напрямую в Personal Conversation и не запускает отдельный Secretary turn.
- Reconnect Client получает пропущенные events в порядке sequence без дублей.
- Delivery retry и failed state видимы в server state и audit log.
- Server restart восстанавливает event sequence, outbox и idempotency records.
- `go test ./...` и `go test -race ./...` покрывают duplicate delivery и concurrent finalization.
