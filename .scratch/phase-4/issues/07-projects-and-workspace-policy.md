# 07 Projects and workspace policy

Type: task
Status: resolved
Blocked by: 01, 04, 05b, 13a

## Work

Добавить manual Projects registry и policy выбора рабочей области для Worker.

- Project хранит стабильный ID/name, description, Node→path mappings, allowed HarnessInstances или harness kinds, default Node policy и execution policy.
- Server не сканирует весь диск и не создаёт Projects из случайных директорий.
- Worker получает Project snapshot и конкретный workspace path при Dispatch.
- Node проверяет mapping, workspace root и execution policy до принятия Dispatch.
- Project policy поддерживает required capabilities и явные model/reasoning pins.
- Изменение Project не переписывает snapshot уже созданного Worker.
- Project policy не может обойти immutable Node/HarnessInstance binding.

## Acceptance

- Project можно создать, изменить, удалить и получить через server API.
- Один Project имеет разные path mappings для MacBook Node и home server Node.
- Dispatch в отсутствующий или запрещённый path завершается explicit error.
- Worker сохраняет исходный Project/policy snapshot после reload и изменений registry.
- Server не выполняет filesystem scan и не создаёт скрытые Projects.
- Resolver учитывает Project restrictions и не меняет binding уже созданного Worker.
