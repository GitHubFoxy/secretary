# 01 Worker-first core state and migration contract

Type: task
Status: resolved
Blocked by:

## Work

Перестроить server-owned domain и persistence contract вокруг Worker, Turn, Attempt, AttemptOutcome и Result.

- Worker является основной product entity и хранит intent, Project, immutable Node/HarnessInstance binding, policy snapshot и lifecycle до explicit close.
- Turn содержит один user intent или Follow-up, несколько Attempts при retry и один terminal Result.
- Attempt содержит только server lifecycle metadata. Каждый terminal Attempt завершается AttemptOutcome со status `succeeded`, `failed`, `canceled` или `interrupted`, error и diagnostics.
- AttemptOutcome содержит явную классификацию `retryable` или `final`.
- Result относится к Turn, а не к Attempt. Intermediate AttemptOutcomes не создают Conversation entries.
- Новый API, UI и Secretary tools не используют Task, `task_id`, parent Task или child Worker records.
- Server Worker/Attempt/Result records не содержат native runtime session ID. Session mapping остаётся Node-local.
- Зафиксировать один active Turn на Worker и один Result на Turn.
- Ввести internal-only `retry_attempt` operation: `final` AttemptOutcome закрывает Turn и создаёт Result, а новый Attempt допускается только после terminal `retryable` AttemptOutcome. Операция не доступна Secretary или Client и не retry-ит uncertain execution.
- Подготовить schema/model migration seam для read-only legacy Task rows без создания новых Task records.

## Acceptance

- Core types и SQLite schema явно разделяют AttemptOutcome и Result.
- Повторный Attempt terminal event не создаёт второй AttemptOutcome.
- На Turn нельзя создать второй логический Result.
- Internal retry создаёт новый Attempt того же Turn только после `retryable` outcome и не создаёт новый Conversation entry.
- `final` outcome закрывает Turn и создаёт его единственный Result.
- Uncertain или running Attempt не запускается повторно автоматически и получает explicit `interrupted` как final outcome.
- В server Worker/Attempt/Result records отсутствует native runtime session ID.
- Новые domain/API types не содержат parent/child Worker tree или `task_id`.
- Existing tests покрывают state transitions, close, retry и invalid transitions.
- Перед destructive migration сохраняется backup локального database.

## Answer

Worker-first core contract реализован в `internal/core`. Новые Worker, Turn, AttemptOutcome и Result records используют отдельные Phase 4 SQLite tables, сохраняют immutable Project/Node/HarnessInstance binding и не содержат native runtime session ID. Terminal AttemptOutcome идемпотентен, `retry_attempt` разрешён только после terminal `retryable` outcome, а `final` outcome создаёт единственный Result и Conversation entry. Recovery переводит недоказанные Attempts в `interrupted`/`final` без автоматического retry.

Legacy Task tables и API сохранены как migration compatibility seam, но новые Phase 4 operations не создают Task records. Полная конверсия существующих Phase 3 rows и переключение production API остаются отдельными tickets 06a/06b/14. Добавлена backup-копия существующей SQLite database перед migration. Тесты покрывают retry, duplicate terminal delivery, один Result на Turn, active Turn invariant, immutable binding, invalid transitions, recovery и backup.
