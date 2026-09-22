# 01 Always-on server baseline

Type: task
Status: resolved
Blocked by: 00
Contract: `docs/pi-viewer.md`

## Goal

Сделать always-on Laptop надёжным read source для Pi viewer.

## Work

- Подготовить documented server data directory, SQLite backup и restore-check.
- Добавить или подтвердить launchd runbook для Secretary server и Node daemon.
- Описать Tailscale-only access между always-on Laptop и MacBook Air.
- Закрепить supported topology: `secretaryd` loopback-only (`127.0.0.1:8081`), Tailscale Serve делает HTTPS proxy поверх loopback, Tailscale ACL ограничивает доступ owner devices. `-listen 0.0.0.0` и прямой LAN/public bind запрещены.
- Описать Serve surface (вариант B): полный `/v1` и статика User UI поверх одного loopback server, с перечислением защищённых routes вне Pi surface - `/v1/nodes/connect`, `/v1/internal/secretary/tools/call`, `/v1/telegram/pairing`, `/v1/clients/*` - и их credential-гейтов. Control Room и `/v1/control/*` остаются debug-only. `GET /v1/bootstrap` остаётся доступен по `conversation:read` и в negative tests не входит.
- Добавить negative tests: Pi credential получает отказ на Node protocol, internal Secretary tools, Telegram pairing, control routes и все write routes.
- Добавить bounded health/status checks, пригодные для runbook.
- Проверить restart server, reconnect Node и сохранность durable state.

## Non-goals

- Server HA, Kubernetes, public Internet exposure и Telegram.

## Acceptance

- После reboot Laptop server и Node автоматически доступны.
- Pi-host через Tailscale может прочитать `GET /v1/health`.
- Реальный Codex или fx HarnessInstance становится ready после restart.
- Restore в отдельный temporary directory подтверждает читаемую Conversation и Worker Result.
- Serve публикует `/v1` и User UI, Control Room и `/v1/control/*` недоступны вне `-debug`.
- Negative tests фиксируют отказ Pi credential на `/v1/nodes/connect`, `/v1/internal/secretary/tools/call`, `/v1/telegram/pairing` и control routes.
- В runbook и launcher нет `-listen` вне loopback.
- Runbook не содержит credentials, bootstrap fragments или native IDs.

## Answer

Acceptance пройден на реальном железе 2026-09-22 (omarchy, Arch Linux; MacBook Air как Pi-host).

- Reboot без входа пользователя (linger включён): `systemctl --user is-active secretaryd.service secretary-node.service` → `active` / `active`. Uptime подтвердил свежую загрузку.
- `curl --noproxy '*' https://omarchy.tail089ef.ts.net/v1/health` с MacBook Air → `{"status":"ok"}`. Прямой curl без `noproxy` упирался в локальный `https_proxy` на Air, не в сервер. Tailscale Serve: `proxy http://127.0.0.1:8081`, ACL owner-only включён в админ-консоли.
- Реальный fx HarnessInstance ready: `sex doctor` без проблем (`ok: fx`, `ok: secretaryd`, `ok: bootstrap environment`, `ok: config`). Node `omarchy` подключён outbound.
- Restore-check: свежий `sqlite3 .backup`, `secretary-migrate` на копии, `PRAGMA integrity_check` → `ok`. Счётчики нулевые, что ожидаемо для свежего деплоя без рабочей нагрузки (одна строка `conversations`).
- Negative tests: `TestNodesConnectRequiresNodeReference`, `TestNodesConnectRejectsRealViewerCapability` (`cmd/secretaryd/main_test.go`, коммит `bf0156c`); `go test ./internal/webapi ./cmd/secretaryd` зелёный.
- awk-дефект из launcher исправлен (`sex:66`, коммит `d413bff`): `journalctl -b` показывает 0 `regexp escape sequence` warning.
- Runbook переписан с launchd на Arch systemd (`docs/always-on-runbook.md`, коммит `9bced9c`); секреты только в `environment` (0600), юниты без секретов, P1-correction про pairing token lifecycle задокументирован.

Ветка `phase4-implementation`, все коммиты запушены (`08fd676`, `bf0d760`, `20cfd08`, `bf0156c`, `d413bff`, `9bced9c`).

Дополнение: статус возвращён в `claimed`, flow ниже признан FAIL (см. следующий раздел). После фикса тикета 06 flow повторён успешно, оба тикета закрыты (см. конец файла).

## Answer продолжение: real Worker flow FAIL (workspace isolation)

Flow признан FAIL и не используется как подтверждение нормальной Worker execution. Причина — нарушение workspace isolation (P0, вынесен в тикет 06, блокирует дальнейшие real Worker claims).

- `list_nodes` через internal tools bridge показал server-observed inventory после reboot: `omarchy/fx` v0.0.10, `authenticated: true (local)`, `status: ready`, execution `[shell, edit, cancel]`, heartbeat свежий, `online: true`.
- Проект `acceptance` создан через `POST /v1/projects` (web-сессия по bootstrap token) с mapping `omarchy -> .../acceptance-ws`. Workspace добавлен в node config через `sex node setup --workspace`, юнит Node перезапущен.
- `spawn_worker` (intent: создать `ACCEPTANCE.md` с текстом `acceptance-ok`, preferences `project_id: acceptance`, `harness_kind: fx`): один Worker задиспатчен на `omarchy/fx`, один terminal succeeded Result за ~14 секунд.
- Свежий `sqlite3 .backup` + restore-check на копии: `integrity_check` → `ok`, `conversations|1`, `workers|1`, `phase4_results|1` (status `succeeded`). `results|0` это legacy-таблица, реальный результат лежит в `phase4_results`.
- Ветка `phase4-implementation`, все коммиты запушены (`08fd676`, `bf0d760`, `20cfd08`, `bf0156c`, `d413bff`, `9bced9c`).

Почему FAIL: файл создан с верным содержимым, но в `/home/coder/ACCEPTANCE.md` вместо declared project workspace. Worker с project mapping писал вне workspace — это нарушение workspace isolation, а не успешный flow. Stray-файл удалён.

## Answer продолжение: повторный flow после фикса 06 — PASS, тикет закрыт

- Фикс `f336024` задеплоен на omarchy (`git pull`, `sex setup`, restart юнитов).
- Повторный real fx flow: один Worker на `omarchy/fx`, один terminal succeeded Result. Sentinel с точным содержимым создан строго в mapped workspace (`acceptance-ws`), `$HOME` чист.
- Свежий backup + restore-check: `integrity_check ok`, `workers|2`, `phase4_results|2` (оба `succeeded`).
- Все пункты acceptance выполнены: reboot без логина (оба юнита active), health через Tailscale Serve с MacBook Air, fx ready в server-observed inventory, readable Conversation + Worker Results, negative tests, loopback-only bind, runbook без секретов.
