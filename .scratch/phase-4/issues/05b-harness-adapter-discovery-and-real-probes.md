# 05b Harness adapter discovery and real probes

Type: task
Status: ready-for-agent
Blocked by: 04, 04a

## Work

Реализовать adapter discovery и реальные probes поверх static HarnessInstance contract.

- Node обнаруживает на каждой машине доступные harness instances с ID, kind, version, authentication, health и status.
- Adapter сообщает фактические model IDs, reasoning levels, execution capabilities и normalized activity capabilities.
- Activity types `thinking_summary`, `tool_call`, `tool_result`, internal subagent events и другие публикуются только при реальной поддержке adapter-а.
- Реализовать deterministic probes для `fx`, Claude Code и Codex как обязательных MVP harnesses.
- Сохранить OpenCode как дополнительный compatibility adapter, не используя его вместо Claude Code.
- Недоступный или неаутентифицированный harness возвращает explicit error, без silent fallback.
- Не смешивать adapter discovery с routing resolver из 06b.

## Acceptance

- Два Node-а могут сообщить несколько HarnessInstances с разными versions, models и capabilities.
- Server отклоняет model или reasoning pin, отсутствующий в selected HarnessInstance inventory.
- Harness без конкретного activity capability не генерирует synthetic event.
- Missing, unauthenticated или unhealthy harness виден как explicit unavailable state.
- `fx`, Claude Code и Codex проходят deterministic adapter probes; OpenCode probe является отдельным compatibility check.
- Inventory обновляется при reconnect и не переписывает immutable binding уже созданного Worker.
- Probes используют contract 04a и не добавляют server-side session IDs.
