## Agent skills

### Code comments

Do not write explanatory, descriptive, or rationale comments in code files, tests, or scripts; the code itself must remain the source of truth, and documentation belongs in Markdown or other dedicated documentation files. Text inside string literals, test fixtures, URLs, and data is not considered commentary.
Compiler and toolchain directives whose comment syntax is strictly required (such as Go's `//go:embed` or `//go:build`) are permitted as a narrow exception, but must never contain attached explanatory prose.

### Issue tracker

Задачи и спецификации ведутся в GitHub Issues через `gh`. См. `docs/agents/issue-tracker.md`.

### Triage labels

Используются стандартные labels Matt Pocock skills. См. `docs/agents/triage-labels.md`.

### Domain docs

Single-context layout: `GLOSSARY.md`, `docs/architecture/runtime-contracts.md` и `docs/adr/`. См. `docs/agents/domain.md`.

### Production deployment

Перед обновлением production-сервера следуй `docs/always-on-runbook.md`. Сверяй эффективный `ExecStart` через `systemctl --user show` и все unit/drop-in через `systemctl --user cat`: обновление checkout или успешный `setup` сами по себе не подтверждают обновление работающего сервиса. После перезапуска проверь путь исполняемого файла у `MainPID`, встроенную revision, конфигурацию сервиса, число процессов, состояние systemd и health check. Отчитывайся об успешном обновлении только после совпадения запущенной revision с целевой.
