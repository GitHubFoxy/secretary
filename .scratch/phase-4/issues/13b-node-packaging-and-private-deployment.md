# 13b Node packaging and private deployment

Type: task
Status: ready-for-agent
Blocked by: 09, 13a

## Work

Доделать deployment, packaging и operator experience после появления реального Node executable.

- Поддержать local setup на MacBook и home server, включая workspace mappings и installed harness discovery.
- Интегрировать private-network-first deployment, например Tailscale, без public inbound port на Node.
- Разделить Node credentials, Client credentials, Secretary runtime credentials и Telegram token.
- Сохранить `sex` и `sex setup` как исторические CLI names; setup должен явно показывать full-access trusted Node policy.
- Добавить launchd/service lifecycle, status, logs, doctor и безопасный revoke/drain flow.
- Не публиковать bootstrap URL или credentials в Git, logs или Worker envelope.

## Acceptance

- Два Node process устанавливаются и запускаются через documented private-network setup.
- Node после restart автоматически reconnect-ится outbound и не запускает старую работу повторно.
- Local setup работает без входящего порта на Node.
- `sex setup`, start/stop/status/logs/doctor не ломают существующий CLI contract.
- Pairing/revoke проверяет identity и не принимает Client credentials как Node credentials.
- Secret redaction проходит для config export, diagnostic logs и bootstrap output.
