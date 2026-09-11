# Node packaging and private deployment

Type: task
Status: ready-for-human
Blocked by: 04, 05, 09

## Work

Собрать отдельный `secretary-node` runtime и deployment path для двух доверенных машин.

- Добавить Node identity, pairing, revoke, heartbeat, drain и reconnect lifecycle.
- Запускать Node как отдельный процесс, который устанавливает outbound connection к `secretaryd`.
- Поддержать local setup на MacBook и home server, включая workspace mappings и installed harness discovery.
- Интегрировать private-network-first deployment, например Tailscale, без public inbound port на Node.
- Разделить Node credentials, Client credentials, Secretary runtime credentials и Telegram token.
- Сохранить `sex` и `sex setup` как исторические CLI names; setup должен явно показывать full-access trusted Node policy.
- Не публиковать bootstrap URL или credentials в Git, logs или Worker envelope.

## Acceptance

- Два Node процесса могут pair с одним Secretary server и отдельно revoke-иться.
- Node после restart автоматически reconnect-ится outbound и не запускает старую работу повторно.
- Node offline отображается на server, а bound Workers не мигрируют.
- Pairing/revoke проверяет identity и не принимает Client credentials как Node credentials.
- Local setup работает через private network без входящего порта на Node.
- `sex setup`, start/stop/status/logs/doctor не ломают существующий CLI contract.
- Secret redaction проходит для config export, diagnostic logs и bootstrap output.
