# Worker lifecycle, dispatch and Secretary tools

Type: task
Status: ready-for-human
Blocked by: 01, 02, 04, 05, 07

## Work

Реализовать Worker-first orchestration между Secretary server и Node.

- Реализовать `list_nodes`, `list_projects`, `list_workers`, `get_worker`, `spawn_worker`, `message_worker`, `cancel_worker` и `close_worker`.
- `spawn_worker` создаёт один Worker, первый Turn и Attempt metadata, затем выбирает Project, Node и HarnessInstance по policy.
- Если подходящий Node недоступен, Worker остаётся видимым в `queued` и не переназначается молча.
- После создания Worker его Project, Node, HarnessInstance и execution policy snapshot immutable.
- `message_worker` делает Steering для active Worker, отвечает на pending `needs_input`, создаёт Follow-up для idle Worker и resume после interrupted state.
- `cancel_worker` останавливает Attempt, а `close_worker` безопасно завершает Worker без нового child tree.
- Retry остаётся внутренностью Turn. Каждый Attempt получает AttemptOutcome, а финальный Turn получает один Result.
- Сохранить `fx` как default Worker harness, отдельно от Secretary runtime harness.
- Не создавать Task, child Worker, parent Task или server-side subagent aggregate.

## Acceptance

- Secretary tool list совпадает с target contract и не содержит `create_task`, `retry_dispatch`, `queue_worker_message`, `request_approval` или child-worker tools.
- Active Worker получает Steering без нового Turn; idle Worker получает новый Follow-up Turn; interrupted Worker использует resume.
- Worker с недоступным Node остаётся bound и queued. Выполнение того же intent на другом Node создаётся как новый Worker.
- Сценарий с несколькими Attempts создаёт несколько AttemptOutcomes и ровно один Result в Conversation.
- Cancel и Close idempotent и не оставляют active Attempt.
- Internal harness subagents видны только как Worker activity.
- Unavailable harness и invalid model завершаются explicit error без fallback.
