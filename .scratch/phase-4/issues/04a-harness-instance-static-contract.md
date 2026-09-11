# 04a HarnessInstance static contract

Type: task
Status: ready-for-agent
Blocked by: 01

## Work

Зафиксировать маленький общий schema contract для HarnessInstance и capabilities до реализации Node transport.

- Определить stable fields: instance ID, Node reference, harness kind, version, authentication, health/status, model IDs и reasoning levels.
- Разделить execution capabilities и normalized activity capabilities.
- Зафиксировать capability names для shell/edit/cancel/steering/approvals и activity `thinking_summary`, `tool_call`, `tool_result`, internal subagent events и других observed types.
- Определить typed shape для inventory snapshot без native runtime session ID.
- Определить typed shape для Activity, AttemptOutcome и command metadata, которую будут транспортировать 04 и 05b.
- Сохранить server policy отдельно от observed inventory и не включать aliases `fast`, `smart`, `cheap`.

## Acceptance

- Contract compiles independently and is consumed by Node protocol and adapter tests.
- Fixture может описать `macbook/claude`, `macbook/codex`, `home-server/fx` и capability differences между ними.
- Model/reasoning IDs являются observed values, а не server-defined global catalog.
- Capability-dependent activity types имеют явную representation и не создаются synthetic adapter-ом.
- Contract не содержит native runtime session IDs, callback capabilities или child Worker fields.
