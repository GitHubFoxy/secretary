# 11 Debug-only Control Room

Type: task
Status: ready-for-agent
Blocked by: 05b, 06a, 06b, 08, 09

## Work

Адаптировать Debug-only Control Room под Phase 4 operations и observability.

- Показывать Nodes, heartbeat, drain/revoke и HarnessInstance inventory с observed capabilities, models, versions и health.
- Показывать Workers, Turns, Attempts, AttemptOutcomes, immutable bindings и Result/delivery state.
- Показывать Projects, path mappings, Clients, pairing/revoke и Approval audit.
- Дать diagnostic view normalized events, Node command IDs, outbox/replay state и redacted raw harness logs.
- Сохранить atomic Profile/config edit and reload semantics.
- Жёстко ограничить Control Room флагом `--debug`. Без debug endpoint и assets недоступны.
- Не показывать secrets, Node credentials, callback capabilities или raw chain-of-thought.

## Acceptance

- `/control-room` недоступен при запуске без `--debug`.
- В debug mode видны два Nodes, их HarnessInstances, Worker bindings, Attempts и AttemptOutcomes.
- Дубликаты events и deliveries можно диагностировать по event ID, sequence и command ID.
- Profile/config reload с invalid input сохраняет active snapshot.
- Revoke Client/Node доступен и виден в audit.
- Diagnostic export редактирует secrets и не содержит runtime session IDs server-side.
