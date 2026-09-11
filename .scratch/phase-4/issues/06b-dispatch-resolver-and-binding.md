# 06b Dispatch resolver and immutable binding

Type: task
Status: ready-for-agent
Blocked by: 01, 02, 04, 05b, 07, 13a

## Work

Реализовать routing resolver, который выбирает Project, Node и HarnessInstance до создания immutable Worker binding.

- При отсутствии override использовать `worker_policy.default_harness`, а затем явный preferred order.
- Учитывать Project path mapping, allowed harness kinds, required capabilities, Node health/capacity и observed HarnessInstance inventory.
- Поддержать explicit harness, Node и model/reasoning pins.
- Проверять model/reasoning ID только по selected HarnessInstance adapter inventory.
- Создавать immutable binding Worker к одному Node и HarnessInstance до Dispatch.
- Если selected Node недоступен, оставлять Worker queued на этой binding и не выбирать другую машину автоматически.
- Если пользователь хочет выполнить тот же intent на другом Node или harness, resolver создаёт новый Worker.
- Явно возвращать unavailable или invalid-selection error, без silent `fx` fallback.

## Acceptance

- No override выбирает `fx` как default Worker harness, отдельно от Secretary runtime harness.
- Explicit Claude Code выбирает Claude Code на подходящем Node.
- Model ID вне selected HarnessInstance inventory даёт visible error и не переключается на `fx`.
- Недоступный Node оставляет Worker queued и bound, без migration.
- Project restrictions и required capabilities учитываются до Dispatch.
- Повтор resolver для одного Worker не меняет Node, HarnessInstance или Project binding.
- Resolver не создаёт Task, child Worker или public retry operation.
