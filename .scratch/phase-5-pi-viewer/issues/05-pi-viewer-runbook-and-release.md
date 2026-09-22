# 05 Pi viewer runbook and release

Type: task
Status: resolved
Blocked by: 04
Contract: `docs/pi-viewer.md`

## Goal

Сделать Pi viewer повторяемым personal setup, а не разовой developer demo.

## Work

- Написать concise install/runbook для always-on Laptop и MacBook Air.
- Описать start, status, logs, backup, restore-check, restart, credential revoke и Tailscale diagnosis.
- Добавить минимальный automated release check для Pi viewer code path и документации.
- Обновить release evidence с реальным PASS или честным BLOCKED/FAIL.

## Non-goals

- Telegram, write controls, multi-user, server HA и новые harnesses.

## Acceptance

- Новый MacBook Air можно pair-ить как viewer без доступа к SQLite или Node credential.
- После reboot пользователь выполняет одну documented Pi command и видит server state.
- Runbook содержит recovery для server offline, Node offline, Tailscale unavailable и revoked credential.
- Release note объявляет только `Pi read-only viewer`, не full remote control.

## Comments

2026-09-22: код/доки часть готова, `./scripts/phase5-release-gate.sh` PASS (RC=0, run `p5-deterministic-7f59baf-20260922T160835Z`, дерево не изменилось, запись в `docs/phase5-release-gate.md`). Gate использует `npm run check:readonly` (biome без `--write`); approve flow в runbook печатает credential только в shell-переменные и передаёт на Air пайпом через Tailscale SSH. Строка acceptance "одна documented Pi command после reboot" осталась `NOT RUN` в manual proof: reboot и прогон команды на реальном железе не выполнялись. Тикет остаётся `claimed` до этого прогона.

## Answer

Документация и release gate готовы, все четыре acceptance строки закрыты фактами.

- `docs/always-on-runbook.md`: разделы "Viewer pairing (Pi read-only credential)" и "Tailscale diagnosis". Pair/approve/revoke с обязательным `Idempotency-Key` (без него `400 idempotency key is required`), credential capture'ится в shell-переменные и никогда не печатается, передача на Air — пайпом через Tailscale SSH в файл `0700`/`0600`. Добавлен fallback: если Tailscale SSH capability выключена и порт 22 на Air закрыт, весь flow выполняется с Air через Serve endpoint'ы.
- `docs/pi-viewer-runbook.md`: credential storage, фактические snapshot/TUI команды (bundle + `PI_EXPERIMENTAL=1` со снятием прокси-переменных, проверено на прогоне), четыре connection state и recovery-матрица. Node offline описан с обеих сторон: viewer symptom — stale workers без control action, server diagnosis — `journalctl`/`systemctl`/`sex node status`; чтение `/diagnostics` из Pi запрещено (403, не входит в surface).
- `docs/pi-viewer-release-note.md`: объявляет только `Pi read-only viewer`, "Viewer не является remote control", `Documented grant` — ровно три read scope без write scopes.
- `scripts/phase5-release-gate.sh` + `docs/phase5-release-gate.md`: семь шагов, gate только читает дерево (`gofmt -l`, `npm run check:readonly`, снимок diff до/после). Фактический прогон `p5-deterministic-7f59baf-20260922T160835Z`, RC=0, `TREE_UNCHANGED`; пользователь воспроизвёл его самостоятельно.
- `phase-1/package.json`: `check` разбит на `check:write` + `check:static`, добавлен `check:readonly` без `--write` — gate не мутирует дерево. Убран unused variable warning в `secretary/runtime.ts`, biome отформатировал 7 viewer-файлов.
- Manual proof (запись в `docs/phase5-release-gate.md`): reboot omarchy 2026-09-22, health 200 через 25s, оба сервиса active с `Linger=yes`, documented `--once` с Air RC=0 — snapshot с 4 workers, 4 conversation entries, approvals пусто. Прогон выявил и закрыл два расхождения документации с реальностью: установленный `pi` не поддерживает `--experimental` (runbook переведён на bundle), а прокси-переменные окружения на Air ломают bundle-транспорт (`fetch failed`, решение — `env -u ...`, задокументировано).
