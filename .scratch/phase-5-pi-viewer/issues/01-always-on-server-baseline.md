# 01 Always-on server baseline

Type: task
Status: claimed
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
