# OpenCode как третий harness (#138)

## Цель

Secretary Agent и Specialists работают не только на Claude Code и Codex, но и на OpenCode: тот же чат, очередь, прерывание, resume, MCP `delegate`/`send`, отчёты родителю. На omarchy OpenCode уже установлен и ходит через PAG, поэтому отдельная подписка не нужна.

## Что есть сейчас

Контракт адаптера — `Turn` в `server/harness.ts`: один ход на процесс, `spawnHarness` через `setsid`, события в `onEvent`, прерывание по `AbortSignal` с добиванием через 10 секунд, `onDispatching`/`onAccepted` для очереди. Claude Code говорит stream-json (`server/claude.ts`), Codex — JSON-RPC `app-server` (`server/codex.ts`). Реестр ходов в `server/index.ts:44`, список harness для клиента в `web/src/agents.js`. Модели и уровни reasoning спрашиваются у самого harness (`server/models.ts`). События в чат превращает только `server/chat.ts` (ADR 0007). Системный промпт Specialist — `specialistPrompt` из `server/persona.ts`, MCP-токен на сессию выдаёт `server/sessions.ts:118–126`.

## Как OpenCode устроен на omarchy

- `opencode v2.1.2`, бинарник `/usr/bin/opencode` (Arch-пакет, `autoupdate: false`).
- Конфиг `~/.config/opencode/opencode.json`: провайдер `pag` (openai-compatible, `baseURL https://omarchy.tail089ef.ts.net:9446/v1`, ключ `{file:~/pag/cpa/secrets/client.key}`), политика запрещает все провайдеры кроме `pag`. Модели — `pag/<id>` (около 30). Вход по подписке не нужен: авторизация — ключ PAG.
- Глобальный `~/.config/opencode/AGENTS.md` с предпочтениями владельца загружается в каждую сессию, как `~/.claude/CLAUDE.md` у Claude Code.
- Фоновый сервис (`opencode service`, порт 49374) принадлежит TUI. ACP его не использует.
- Сессии хранятся в общей SQLite `~/.local/share/opencode/opencode.db` (WAL), поэтому resume работает между процессами.

## Проверено вживую

Пробы гонялись на omarchy в `/tmp/opencode` на бесплатной модели `pag/beam-free` (скрипты `probe.py`, `probe2.py` остались там же).

1. **Транспорт.** `opencode acp` — ACP v1, JSON-RPC по строкам stdin/stdout. Сам поднимает приватный сервер (`serve --stdio --port 0`), сетевой порт не открывает, завершается при закрытии stdin. Это ложится на `spawnHarness` и ADR 0006 без изменений.
2. **Ход.** `initialize` → `session/new {cwd, mcpServers}` → `sessionId` и `configOptions` (model, effort, mode build/plan). `session/set_config_option {configId: "model"|"effort", value}` меняет модель; ответ содержит обновлённый `configOptions`. `session/prompt` шлёт `session/update`: `agent_thought_chunk`, `agent_message_chunk`, `tool_call` (kind `shell`/`execute`, `rawInput`), `tool_call_update` (status), `usage_update`. Ответ на prompt — `{stopReason: "end_turn", usage: {...}}`; **итогового текста в ответе нет**, его собирают из `agent_message_chunk`.
3. **Permissions.** `OPENCODE_CONFIG_CONTENT='{"permissions":[{"action":"*","resource":"*","effect":"allow"}]}'` — bash выполнился без единого запроса подтверждения.
4. **Системный промпт.** `OPENCODE_CONFIG_CONTENT` с `instructions: ["/path/file.md"]` — модель выполнила инструкцию из файла. Так подаётся `systemPrompt` Secretary без правки папки проекта.
5. **Resume.** Новый процесс `opencode acp`, `session/resume {sessionId, cwd}` (cwd обязан совпадать) — контекст прошлого хода на месте. Ответ содержит только `configOptions`.
6. **Параллельность.** Два процесса `opencode acp` в разных папках одновременно отработали ходы, оба `end_turn`.
7. **Конфиг через окружение.** `OPENCODE_CONFIG_CONTENT` сливается с конфигом (проверено подменой модели по умолчанию). В документации V2 переменная не описана, но в бинарнике и на практике работает; тест адаптера должен фиксировать это поведение.

Из документации ACP (<https://opencode.ai/docs/cli/acp/>), не проверялось вживую:

- `session/cancel` завершает ход с `stopReason: "cancelled"`; незавершённые инструменты помечаются `failed`.
- MCP: `session/new`/`resume` принимает stdio и HTTP серверы; HTTP-заголовки передаются как есть, без OAuth. Формат HTTP: `{type: "http", name, url, headers: [{name, value}]}`. Сервер с именем из конфига замещается клиентским на время жизни процесса.
- `/compact` текстом prompt запускает сжатие; при `clientCapabilities.session.compaction` приходят `compaction_update`, иначе маркер `_meta["opencode/compaction"]`.
- Команды: prompt, начинающийся с `/имя` доступной команды, выполняет её; список приходит в `available_commands_update` после `session/new`.
- Лимиты и retry: `session_info_update` с `_meta["opencode/retry"]` (`error.type: "provider.rate-limit"`, `nextRetryAt`).
- Картинки: блок `{type: "image", data, mimeType}`; `promptCapabilities.image: true`.
- Дочерние сессии OpenCode (его субагенты) приходят как обновления с `_meta["opencode/child-session"]`.
- Если провайдеру нужен вход, prompt падает с ошибкой `auth_required`.

## Варианты транспорта

1. **ACP, процесс на ход (рекомендация).** Проверен целиком, контракт `Turn` и модель процессов Secretary сохраняются, прерывание и добивание работают как у Codex.
2. HTTP API фонового сервиса или `opencode serve` + SSE. Долгоживущий сервер владеет ходами: убийство группы процессов не останавливает ход, прерывание и остановка требуют отдельных API-вызовов, статусы и восстановление после падения Secretary усложняются. Против ADR 0006.
3. `opencode run --format json`. Нет потока событий и отмены хода — контракт `Turn` не выполнить.

## Рекомендация

Адаптер `server/opencode.ts` по образцу `codex.ts`: JSON-RPC поверх `spawnHarness`, один `opencode acp` на ход.

- **Запуск.** `['opencode', 'acp']`, env дополняется `OPENCODE_CONFIG_CONTENT`: `permissions` allow-all и `instructions: [файл с systemPrompt]`, если он передан. Файл — временный, вне папки проекта; содержимое стабильно между ходами.
- **Ход.** `initialize` (с `clientCapabilities.session.compaction`) → `session/new` или `session/resume` по `resumeId` → `onSessionId` → `set_config_option` model (и effort, если `reasoning` не null) → `onDispatching` → `session/prompt` (текст + image-блоки) → `onAccepted` на первом `session/update` после отправки → каждое уведомление в `onEvent`. Текст ответа копится из `agent_message_chunk`; после ответа на prompt адаптер дописывает синтетическое событие `{type: "opencode_result", text, stopReason, usage}` — единственный источник итогового текста для `agentAnswer` и отчёта родителю. Успех хода — `stopReason === "end_turn"`.
- **Прерывание.** По `interrupt` — уведомление `session/cancel`; добивание через 10 секунд уже есть в `spawnHarness`. До отправки prompt — вернуться без отправки, как у Codex.
- **Compact.** При `options.compact` — prompt `/compact`, ждать `compaction_update`/ответ.
- **MCP.** В `session/new`/`resume`: secretary-сервер `{type: "http", name: "secretary", url, headers: [{name: "Authorization", value: "Bearer …"}]}` плюс внешние серверы из `prepareMcp`. Для stdio значения секретов уже лежат в env под ключами `SECRETARY_MCP_*` — адаптер передаёт их в ACP-поле `env` напрямую.
- **Модели.** Загрузчик в `models.ts`: `opencode acp` во временной папке, `session/new` → список моделей из `configOptions`; для каждой модели `set_config_option` → уровни effort из обновлённого `configOptions`. Токены не тратятся, кэш как у остальных.
- **События в чат** (`chat.ts`): `agent_message_chunk` → `writing` (не плодить сообщения на каждый чанк — проверить, как клиент склеивает подряд идущие `writing` у Claude); `agent_thought_chunk` → `thinking`; `tool_call` → `step` (summary из `rawInput.command` или пути); `compaction_update`/маркер → `compaction`; `opencode_result` → ответ агента, `cancelled` → ничего, ошибка → `error`; `_meta["opencode/retry"]` с rate-limit → `usageLimit` с `resetsAt` из `nextRetryAt`. Дочерние сессии OpenCode первой версией показывать как обычные шаги.
- **Команды.** `slashCommands('opencode', folder)` в `commands.ts`: `session/new` → `available_commands_update`. Скиллы-команды OpenCode выполняет сам по тексту `/имя`, отдельная инъекция как у Codex не нужна.
- **Авторизация.** Ошибку `auth_required` превращать в понятное сообщение с командой `opencode auth login`, по образцу Codex. На omarchy вход не нужен.

## План реализации

1. **Адаптер и контрактные тесты.** `server/opencode.ts`, фейковый `opencode acp` в `server/test-fixtures/` (JSON-RPC по строкам, лог вызовов), тесты в `harness.test.ts` по образцу Codex: resume, прерывание до и после отправки, зависший prompt, добивание по дедлайну, падение процесса, `auth_required`. Отдельно зафиксировать тестом слияние `OPENCODE_CONFIG_CONTENT`.
2. **Модели.** Загрузчик `opencodeModels` в `models.ts` + тест парсинга `configOptions`. Уровни effort у моделей разные (`low…max`, `default`); `reasoning: null` — effort не трогать.
3. **Чат.** Маппинг событий в `chat.ts` + тесты в `chat.test.ts` (текст, thinking, шаги, сжатие, отмена, лимит). Проверить, что `agentAnswer`/`speechAnswer`/`usageLimit` в `session.ts` подхватывают `opencode_result` без особых случаев в самой Session.
4. **Обвязка сервера.** Реестр `turns` в `index.ts`, `commands.ts`, `observability.ts` (opencodeLaunch). Лимитов подписки у PAG нет — отдельный эндпоинт не нужен.
5. **Клиент.** `web/src/agents.js` (третий harness), иконка в `Icon.svelte`, `modelLabel` — отбрасывать префикс `pag/`, частный случай рамки Claude в `ModelPicker.svelte` не ломать.
6. **Skills и persona.** OpenCode читает `AGENTS.md` папки сессии и `.claude/skills` нативно. Проверить в изолированном HOME, что day-summary skill виден; если нет — добавить публикацию в `.opencode/skills` в `skills.ts`. `CLAUDE.md`-заглушка из `persona.ts` для OpenCode не нужна.
7. **Документы.** Обновить `docs/architecture.md` и GLOSSARY; ADR о выборе ACP и приватного сервера на ход (почему не фоновый сервис).
8. **Приёмка.** `bun test` в `server/` и `web/`. Живой прогон на omarchy в изолированном профиле (`HOME=$(mktemp -d)`, `SECRETARY_PORT`/`SECRETARY_MCP_PORT` отличные от боевых; рабочую `~/.secretary` не трогать и службу не перезапускать): создать сессию opencode на `pag/beam-free`, отправить сообщение, прервать ход, продолжить после перезапуска сервера (resume), поднять Specialist с MCP `send`/`delegate`, выполнить `/compact`, запустить два Specialist параллельно, в том числе в одной папке проекта.

## Открытые вопросы и риски

- **Общая база.** Приватные серверы разных ходов пишут в один `opencode.db`. Параллельные ходы в разных папках проверены; одна папка и высокая нагрузка — нет. Шаг 8 это закрывает; при конфликтах блокировок рассматривать один долгоживущий `opencode serve` на Secretary (вариант 2) как запасной путь.
- **`OPENCODE_CONFIG_CONTENT` не документирован.** Поведение зафиксировать тестом; при обновлении OpenCode проверять первым делом. Запасной путь — проектный конфиг во временной папке сессии.
- **Рост хранилища.** Сессии OpenCode копятся в `~/.local/share/opencode`; удаление Session в Secretary их не чистит (как и у Claude/Codex). При желании — `session/delete` в адаптере остановки, отдельным решением.
- **Снапшоты.** OpenCode делает снимки файловой системы для undo; в рабочих папках Specialists это лишняя нагрузка. Проверить и при необходимости отключать `snapshots: false` в инъекции конфига.
- **Дочерние сессии.** Как показывать субагентов OpenCode в чате — первая версия сводит их к шагам; отдельный UX позже.
