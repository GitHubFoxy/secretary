# Release note: Pi read-only viewer

Первый client-релиз Secretary: Pi на MacBook Air читает состояние always-on сервера через Tailscale. Это read-only viewer.

## Что объявляется

`Pi read-only viewer`: `pi --experimental secretary` (snapshot через `--once` и interactive TUI) поверх HTTPS-эндпоинтов сервера в tailnet.

Viewer не является remote control: он только читает состояние сервера и не выполняет никаких действий от имени владельца.

## Documented grant

Выдаётся ровно один credential с тремя scope'ами:

| Scope | Что даёт |
| --- | --- |
| `conversation:read` | Personal Conversation, live stream, Secretary turn stream |
| `worker:read` | список Workers, статус, детали, worker activity |
| `approval:read` | summary pending Approvals |

Credential хранится на Air в `~/.config/secretary/viewer-credential` (каталог `0700`, файл `0600`).

## Что не входит

- Отправка сообщений и worker commands (`message`, `respond`, `steer`, `queue`, `stop`, `cancel`, `close`).
- Approval decisions (`approve`, `deny`).
- Node control и Node protocol.
- Управление Clients, bootstrap token, доступ к SQLite и Data directory.
- Telegram и phone client.

## Границы данных

Каждый viewer endpoint отдаёт отдельный allowlisted public DTO: без `node_id`, `harness_instance_id`, `diagnostics`, `context_snapshot`, `policy_snapshot`, credentials, native IDs и raw ACP frames. `GET /v1/workers/{worker_ref}/diagnostics` для viewer credential возвращает `403`.

## Документация

- Контракт: `docs/pi-viewer.md`
- Серверный runbook: `docs/always-on-runbook.md`
- Air-side runbook: `docs/pi-viewer-runbook.md`
- Release evidence: `docs/phase5-release-gate.md`
