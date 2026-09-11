# Phase 4 acceptance gate

Type: task
Status: ready-for-human
Blocked by: 03, 04, 05, 06, 07, 08, 09, 10, 11, 12, 13, 14

## Work

Создать deterministic release gate и manual real-harness proof для всего Phase 4 contract.

- Поднять чистый Secretary server, MacBook Node и home server Node.
- Проверить observed HarnessInstances, Projects с разными path mappings и Web/Telegram Personal Conversation.
- Проверить полный Secretary stream, один active Secretary turn, durable input queue и параллельные Workers.
- Проверить Worker-first lifecycle без Task и child Workers.
- Проверить несколько Attempts одного Turn, AttemptOutcomes и ровно один финальный Result.
- Проверить Approval и `needs_input` через `respond_worker` из другого Client.
- Проверить immutable Worker binding, offline Node и отсутствие smart migration.
- Прервать сеть после завершения harness и подтвердить Node outbox replay.
- Повторить server→Node command и подтвердить отсутствие второго process.
- Выполнить real acceptance для `fx`, Claude Code и Codex. OpenCode проверить отдельно, если он установлен.
- Запустить Go tests, race tests, vet, frontend checks, builds, embedded assets и `git diff --check`.

## Acceptance

- Проходит acceptance scenario из раздела 24 `spec.md` на чистой конфигурации и после restart.
- Claude Code, Codex и `fx` реально выполняют предусмотренные flows; OpenCode не используется как замена Claude Code.
- Node network loss не теряет terminal AttemptOutcome и не создаёт duplicate execution.
- Один Turn публикует один Result, даже если было несколько failed Attempts.
- Web, Telegram и debug-only Control Room показывают согласованный server state.
- Revoke Client/Node, secret redaction, offline state и no-silent-fallback доказаны.
- `go test ./...`, `go test -race ./...`, `go vet ./...`, frontend checks и production builds проходят.
- Manual matrix и diagnostic evidence записаны в `docs/phase4-release-gate.md`.
