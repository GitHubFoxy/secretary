# 05 Pi viewer runbook and release

Type: task
Status: ready-for-agent
Blocked by: 04

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
