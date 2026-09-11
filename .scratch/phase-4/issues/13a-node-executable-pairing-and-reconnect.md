# 13a Node executable, pairing and reconnect

Type: task
Status: ready-for-agent
Blocked by: 04, 05b

## Work

Рано собрать настоящий `secretary-node`, чтобы Worker orchestration можно было проверять на двух process/machines до позднего packaging work.

- Добавить отдельный Node executable/process, который устанавливает outbound authenticated connection к `secretaryd`.
- Реализовать Node identity, enrollment/pairing, revoke, heartbeat, drain и reconnect lifecycle.
- Реализовать минимальные server Node endpoints/commands, необходимые для pairing и управления Node.
- Подключить HarnessInstance inventory из 05b и передавать его по protocol 04.
- Сохранять native runtime session mappings и command dedupe/outbox локально на Node.
- После reconnect не запускать старый Attempt повторно и не менять Worker binding.
- Не включать сюда Tailscale packaging, launchd, `sex setup` и doctor. Они идут в 13b.

## Acceptance

- MacBook Node и home server Node реально запускаются как два отдельных process и pair с одним server.
- Reconnect после network loss восстанавливает authenticated connection, inventory и local outbox.
- Node heartbeat/offline/draining/revoke видны server-side.
- Node не принимает Client credentials, а harness не получает server callback capability.
- Повторная command не запускает второй process или Attempt.
- Два настоящих Node process готовы для integration tests 06a/06b и 15.
