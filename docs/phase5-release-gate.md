# Phase 5 release gate: historical read-only viewer

Этот gate фиксирует историческую проверку read-only viewer и не подтверждает новый messaging extension. Текущий scope и run procedure описаны в `docs/pi-viewer.md` и `docs/pi-viewer-runbook.md`.

Разделяет детерминированный automated check и ручной proof на реальном железе. Один и тот же статус нельзя получать из другого источника: Go-тест не закрывает ручную строку, ручной прогон не заменяет gate.

## Deterministic automated check

Запуск из корня репозитория:

```sh
./scripts/phase5-release-gate.sh
```

### Прогон

- `run_id`: `p5-deterministic-7f59baf-20260922T162546Z`
- `commit`: `7f59baf` плюс незакоммиченные файлы тикета 05 (docs, gate script, phase-1)
- `command`: `./scripts/phase5-release-gate.sh`
- `observed_at_utc`: `2026-09-22T16:25:46Z`
- `RC`: `0`
- `tree`: `TREE_UNCHANGED` (снимок `git diff` до и после прогона совпал, gate только читает дерево)
- `PASS`: все семь шагов, финальная строка `phase5 release gate: PASS`.
- Прогон пользователем: самостоятельный запуск `./scripts/phase5-release-gate.sh` после ревью, все 7 шагов прошли.

В рабочем дереве тикета 05 остаются: 7 viewer-файлов `phase-1`, отформатированных ранее biome, и фикс unused variable в `secretary/runtime.ts` (убрано присваивание, значение не использовалось).

### Состав проверок

Gate останавливается на первой ошибке и проверяет:

- `gofmt -l internal cmd` только чтением, дерево не меняется;
- `go test ./...`, `go vet ./...`, `go build ./cmd/...`;
- `npm run check:readonly` в `phase-1` (biome без `--write`, tsgo, shrinkwrap и остальные проверки Pi client) плюс снимок diff до/после как страховку, что шаг ничего не изменил;
- документацию: четыре recovery-сценария в `docs/pi-viewer-runbook.md`, секцию viewer pairing с `Idempotency-Key` в `docs/always-on-runbook.md`, явный read-only disclaimer и отсутствие write scopes в `Documented grant` релизной заметки, путь credential файла;
- приватную границу viewer: `TestWorkerSurfaceStrictForClientCredential`, `TestReadOnlyCredentialIsRefusedOutsideTheReadSurface`, `TestApprovalListReturnsAllowlistedConversationApprovals`;
- `git diff --check`.

Запись прогона заполняется после фактического запуска: `run_id`, `commit`, `command`, `observed_at_utc`, `RC`.

## Manual viewer proof

Факты из реальных прогонов тикетов 04 и 07. Hardware: omarchy (Arch, always-on) + MacBook Air через Tailscale. Исполнитель: agent session, 2026-09-22. Секреты, tokens и raw ACP frames в ledger не записывались.

| # | Строка | Статус | Факт |
| ---: | --- | --- | --- |
| 1 | Pair viewer credential c явными тремя scopes | PASS | approve вернул ровно viewer grant |
| 2 | `pi secretary --once` через Tailscale | PASS | snapshot envelope без секретов |
| 3 | Live TUI видит workers на сервере | PASS | ESTABLISHED WS через Tailscale Serve |
| 4 | Рестарт `secretaryd`, reconnect и resume | PASS | новое соединение, процесс жив |
| 5 | Revoke: streams закрыты, reconnect остановлен | PASS | `1008 Client revoked`, новых подключений нет |
| 6 | Отозванный viewer заморожен, новый credential видит live worker | PASS | sentinel строго в mapped workspace |
| 7 | Strict DTO retest: list, details, turns без topology | PASS | рекурсивный обход response без запрещённых ключей, массивы непустые |
| 8 | `GET /v1/workers/{ref}/diagnostics` для credential | PASS | `403` |
| 9 | Pair нового viewer без SQLite и Node credential | PASS | pairing использует только bootstrap token из `environment` |
| 10 | Одна documented Pi command после reboot | PASS | reboot omarchy 2026-09-22: health 200 через 25s, `secretaryd`/`secretary-node` active, `Linger=yes`; `--once` с Air RC=0, snapshot с 4 workers и 4 conversation entries, approvals пусто |
| 11 | Evidence без credentials, tokens, prompts, native IDs, raw ACP frames | PASS | временные файлы удалены, credential отозван |

Строка 10 закрыта реальным reboot omarchy и прогоном команды из `docs/pi-viewer-runbook.md` 2026-09-22T16:23Z: pair выполнялся capture'ом в shell-переменные, credential записан в файл `0600` без вывода в терминал, в ledger попали только счётчики snapshot'а.

## Evidence rules

- Ledger содержит команду или артефакт, время, commit и статус `PASS`/`FAIL`/`BLOCKED`/`NOT RUN`.
- Секреты, bootstrap fragments, cookies, pending tokens, credential values и raw prompts в документацию не записываются.
- Ручная строка закрывается только фактом выполнения на реальном железе, не результатом Go-теста.
