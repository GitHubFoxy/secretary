# Worker-first core state and migration contract

Type: task
Status: ready-for-human
Blocked by:

## Work

Перестроить server-owned domain и persistence contract вокруг Worker, Turn, Attempt, AttemptOutcome и Result.

- Worker является основной product entity и хранит intent, Project, immutable Node/HarnessInstance binding, policy snapshot и lifecycle до explicit close.
- Turn содержит один пользовательский intent или Follow-up, несколько Attempts при retry и один terminal Result.
- Attempt содержит только server lifecycle metadata. Каждый terminal Attempt завершается AttemptOutcome со status `succeeded`, `failed`, `canceled` или `interrupted`, error и diagnostics.
- Result относится к Turn, а не к Attempt. Intermediate AttemptOutcomes не создают Conversation entries.
- Новый API, UI и Secretary tools не используют Task, `task_id`, parent Task или child Worker records.
- Server Worker record не содержит native runtime session ID. Session mapping остаётся Node-local.
- Зафиксировать уникальность одного active Turn на Worker и одного Result на Turn.
- Подготовить schema/model migration seam для read-only legacy Task rows без создания новых Task records.

## Acceptance

- Core types и SQLite schema явно разделяют AttemptOutcome и Result.
- Повторный Attempt terminal event не создаёт второй AttemptOutcome.
- На Turn нельзя создать второй логический Result.
- Retry создаёт новый Attempt того же Turn и не создаёт новый Conversation entry до финального Result.
- В server Worker/Attempt/Result records отсутствует native runtime session ID.
- Новые domain/API types не содержат parent/child Worker tree или `task_id`.
- Existing tests покрывают state transitions, close, retry и invalid transitions.
- Перед destructive migration сохраняется backup локального database.
