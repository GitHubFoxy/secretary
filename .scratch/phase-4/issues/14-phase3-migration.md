# 14 Phase 3 migration

Type: task
Status: ready-for-agent
Blocked by: 01, 02, 03a, 03b, 06a, 06b, 07, 09, 13a

## Work

Перенести существующее Phase 3 state в Phase 4 без потери пользовательской истории и без возврата старой domain model.

- Сохранить Person, Secretary identity, Personal Conversation и external `user.md` contents/revision.
- Преобразовать Phase 3 Task с Worker binding в Worker + Turn history.
- Оставить legacy Task rows только read-only для migration/diagnostics и не создавать новые.
- Удалить `task_id` из новых Conversation, API, UI и Secretary tools.
- Не переносить `runtime_session_id` в server Worker record. Доказанное session mapping сохраняется только локально на Node.
- Исторические child Task records не восстанавливать как child Workers.
- Перенести старые Attempt terminal states в AttemptOutcomes и создать не более одного Result для каждого завершённого Turn.
- Мигрировать `fast`, `smart` и `cheap` в явные model pins или adapter defaults без появления aliases в Phase 4 contract.
- Разделить старую runtime config на `secretary.harness/model/reasoning` и `worker_policy.default_harness`.
- Перед миграцией автоматически создавать backup и откатывать invalid config/schema migration.
- Сохранить `phase-1/` Pi snapshot и `sex setup`.

## Acceptance

- Clean migration сохраняет Conversation entries, Worker intent, Project context, `user.md` contents и visible history.
- После migration новый API не возвращает Task или child Worker records.
- Неизвестное native session mapping не приводит к silent resume, а даёт explicit `interrupted`.
- Исторические Attempt states становятся AttemptOutcomes, а завершённый Turn имеет ровно один Result.
- Existing aliases не используются новым Secretary или Worker policy contract.
- Invalid migration input оставляет backup и active state нетронутыми.
- Migration можно повторить idempotently без duplicate Workers, Turns, AttemptOutcomes или Results.
- Manual migration test проходит на копии текущего `secretary.db`.
