# HarnessInstance inventory and adapter capabilities

Type: task
Status: ready-for-human
Blocked by: 04

## Work

Ввести observed HarnessInstance inventory и adapter capability contract.

- Node обнаруживает на каждой машине доступные harness instances с ID, kind, version, authentication, health и status.
- Adapter сообщает фактические model IDs, reasoning levels, execution capabilities и normalized activity capabilities.
- Activity types `thinking_summary`, `tool_call`, `tool_result`, internal subagent events и другие публикуются только при реальной поддержке adapter-а.
- Отделить policy preferences от observed inventory. `fast`, `smart` и `cheap` не входят в Phase 4 contract.
- Реализовать adapter seams для `fx`, Claude Code и Codex как обязательных MVP harnesses.
- Сохранить OpenCode как дополнительный compatibility adapter, не используя его вместо Claude Code.
- Недоступный или неаутентифицированный harness должен возвращать explicit error, без silent fallback.

## Acceptance

- Два Node-а могут сообщить несколько HarnessInstances с разными versions, models и capabilities.
- Server отклоняет model или reasoning pin, отсутствующий в selected HarnessInstance inventory.
- Harness без конкретного activity capability не генерирует synthetic event.
- Missing, unauthenticated или unhealthy harness виден как explicit unavailable state.
- `fx`, Claude Code и Codex проходят deterministic adapter probes; OpenCode probe является отдельным compatibility check.
- Inventory обновляется при reconnect и не переписывает immutable binding уже созданного Worker.
