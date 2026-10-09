# 06a Worker lifecycle and Secretary tools

Type: task
Status: resolved
Blocked by: 01, 02, 04, 05b, 06b, 07, 13a

## Work

Реализовать Worker lifecycle и Secretary tool surface поверх resolver из 06b.

- Реализовать `list_nodes`, `list_projects`, `list_workers`, `get_worker`, `spawn_worker`, `message_worker`, `cancel_worker` и `close_worker`.
- `spawn_worker` создаёт один Worker, первый Turn и Attempt metadata, затем вызывает routing resolver.
- `message_worker` делает Steering для active Worker, отвечает на pending `needs_input`, создаёт Follow-up для idle Worker и resume после interrupted state.
- `cancel_worker` останавливает Attempt, а `close_worker` безопасно завершает Worker без нового child tree.
- Retry реализовать как internal-only `retry_attempt`, недоступный Secretary и Client. `final` AttemptOutcome закрывает Turn и создаёт Result, а новый Attempt того же Turn создаётся только после terminal `retryable` AttemptOutcome.
- Uncertain/running Attempt не retry-ить автоматически, а завершать `interrupted` как final outcome по recovery policy.
- Каждый Attempt получает AttemptOutcome. Финальный Turn получает один Result напрямую в Personal Conversation.
- Сохранить `fx` как default Worker harness, отдельно от Secretary runtime harness.
- Не создавать Task, child Worker, parent Task или server-side subagent aggregate.

## Acceptance

- Secretary tool list совпадает с target contract и не содержит `create_task`, `retry_dispatch`, `retry_attempt`, `queue_worker_message`, `request_approval` или child-worker tools.
- Active Worker получает Steering без нового Turn; idle Worker получает новый Follow-up Turn; interrupted Worker использует resume.
- Internal retry создаёт новый Attempt только после доказанного terminal `retryable` outcome и не создаёт Conversation entry до финального Result.
- `final` outcome закрывает Turn и создаёт его единственный Result.
- Worker с недоступным Node остаётся bound и queued. Выполнение того же intent на другом Node создаётся как новый Worker.
- Сценарий с несколькими Attempts создаёт несколько AttemptOutcomes и ровно один Result в Conversation.
- Cancel и Close idempotent и не оставляют active Attempt.
- Internal harness subagents видны только как Worker activity.
- Unavailable harness и invalid model завершаются explicit error без fallback.
