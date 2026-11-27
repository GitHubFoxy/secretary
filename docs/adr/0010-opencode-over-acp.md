# ADR 0010: OpenCode подключается через ACP отдельным процессом на ход

Дата: 2026-11-27. Контекст: issue #138, план `docs/plans/138-opencode.md`.

## Решение

Адаптер `server/opencode.ts` говорит с OpenCode по Agent Client Protocol: один `opencode acp` на ход, JSON-RPC по строкам stdin/stdout. Контракт `Turn` из `server/harness.ts` и модель процессов Secretary сохраняются без изменений: прерывание уведомлением `session/cancel`, добивание через 10 секунд в `spawnHarness`, resume через `session/resume`.

## Почему не фоновый сервис

`opencode serve` + SSE держит ходы в долгоживущем сервере: убийство группы процессов не останавливает ход, прерывание и остановка требуют отдельных API-вызовов, а падение Secretary оставляет висящие ходы. Это ломает ADR 0006 (systemd владеет процессами harness) и надёжность очереди. Приватный сервер, который `opencode acp` поднимает сам и который умирает при закрытии stdin, ложится на существующую модель: процесс на ход, убийство группы останавливает всё.

`opencode run --format json` не подходит: нет потока событий и отмены хода, контракт `Turn` выполнить нельзя.

## Проверенные факты

- Ход: `initialize` → `session/new`/`session/resume` → `set_config_option` (model, effort) → `session/prompt`. Итогового текста в ответе нет — собирается из `agent_message_chunk`; адаптер дописывает синтетическое событие `opencode_result`.
- Отмена — уведомление `session/cancel` (без id, это не запрос): ответ приходит с `stopReason: "cancelled"`.
- `OPENCODE_CONFIG_CONTENT` сливается с файловым конфигом: через него задаются `permissions allow-all` и `instructions` (файл с systemPrompt вне папки проекта). Поведение зафиксировано тестом слияния, при обновлении OpenCode проверять первым делом.
- MCP: `session/new`/`resume` принимают stdio и HTTP серверы; HTTP-заголовки передаются как есть (`headers: [{name, value}]`).
- `/compact` текстом prompt запускает сжатие; клиент заявляет `session.compaction` и получает `compaction_update`.
- Лимиты PAG отсутствуют, поэтому отдельный эндпоинт лимитов (как `codexRateLimits`) не нужен.
- Skills: OpenCode ищет `.claude/skills` и `.agents/skills` нативно (compatibility-источники), поэтому публикации Secretary в эти папки достаточно — `.opencode/skills` не понадобился. Skills-команды OpenCode выполняет сам по тексту `/имя`, инъекция пути как у Codex не нужна.

## Риски

- Ответы `set_config_option` при пакетных запросах приходят не по порядку — загрузчик моделей спрашивает последовательно.
- Сессии OpenCode копятся в общей `~/.local/share/opencode/opencode.db`; удаление Session в Secretary их не чистит (как у Claude/Codex). При желании — `session/delete` в адаптере остановки, отдельным решением.
- OpenCode делает файловые снапшоты для undo; если в рабочих папках это станет нагрузкой — отключать `snapshots: false` в инъекции конфига.
