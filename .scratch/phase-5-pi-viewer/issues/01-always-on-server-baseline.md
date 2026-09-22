# 01 Always-on server baseline

Type: task
Status: ready-for-agent
Blocked by: 00

## Goal

Сделать always-on Laptop надёжным read source для Pi viewer.

## Work

- Подготовить documented server data directory, SQLite backup и restore-check.
- Добавить или подтвердить launchd runbook для Secretary server и Node daemon.
- Описать Tailscale-only access между always-on Laptop и MacBook Air.
- Добавить bounded health/status checks, пригодные для runbook.
- Проверить restart server, reconnect Node и сохранность durable state.

## Non-goals

- Server HA, Kubernetes, public Internet exposure и Telegram.

## Acceptance

- После reboot Laptop server и Node автоматически доступны.
- Pi-host через Tailscale может прочитать health/status endpoint.
- Реальный Codex или fx HarnessInstance становится ready после restart.
- Restore в отдельный temporary directory подтверждает читаемую Conversation и Worker Result.
- Runbook не содержит credentials, bootstrap fragments или native IDs.
