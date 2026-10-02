# Справочник конфигурации

Secretary читает `config.toml` из data directory. При первом setup создаётся пример из `internal/config/defaults/config.toml`; существующие файлы не перезаписываются автоматически.

Основные секции:

- `[profiles]`: пути внешних Markdown profiles.
- `[tools]`: разрешённые tools.
- `[models]` и `[secretary]`: модель и runtime policy Secretary.
- `[worker_policy]`: default harness, model policy и лимит active Attempts для Workers.
- `[telegram]`: отдельная модель для названий Worker Topics. Эта секция не включает Telegram polling и не хранит bot credentials.
- `[retention]`: срок и предел хранения raw logs.

Запуск и применение изменений описаны в [quickstart](quickstart.md). Telegram deployment и private credentials настраиваются отдельно, см. [always-on runbook](always-on-runbook.md).

## Модель названий Worker Topics

```toml
[telegram]
title_harness = "opencode"
title_prompt = "title-generation-prompt.md"
title_model = "gpt-6-luna"
title_model_reasoning = "minimal"
```

| Ключ | Default | Назначение |
| --- | --- | --- |
| `telegram.title_harness` | `opencode` | Harness только для названий Topics. Сейчас поддерживается OpenCode v2. |
| `telegram.title_prompt` | `title-generation-prompt.md` | Внешний Markdown system prompt. Относительный путь считается от директории `config.toml`; абсолютный путь также разрешён. |
| `telegram.title_model` | `gpt-6-luna` | Model ID. Bare ID означает `openai/<model>`; для другого provider задайте `provider/model-id`. |
| `telegram.title_model_reasoning` | `minimal` | Запрошенный reasoning effort только для генерации названия. |

Допустимые reasoning значения: `none`, `minimal`, `low`, `medium`, `high`, `xhigh`. Значения регистрозависимы. `none` означает запрос без reasoning, `minimal` означает минимальное усилие. `default`, `off`, пустая строка и неизвестные значения не принимаются; скрытого наследования reasoning из Secretary runtime нет.

Model ID должен быть непустым, без пробелов, управляющих и невидимых форматирующих символов. В slash-separated ID нельзя оставлять пустые компоненты: `/model`, `provider/` и `provider//model` невалидны. Суффикс `#variant` не допускается: effort задаётся отдельно через `title_model_reasoning`. Ошибка указывает конкретный ключ и не печатает его содержимое.

Defaults применяются к отсутствующим ключам по отдельности. Старый config без `[telegram]`, пустая секция или секция с одним ключом продолжают загружаться. Явно заданная пустая строка считается ошибкой, а не запросом default.

Пример отдельной модели с отключённым reasoning:

```toml
[telegram]
title_model = "provider/title-model"
title_model_reasoning = "none"
```

### Поддержка provider и область действия

Config loader проверяет значения локально и не обращается к provider catalogue. Наличие model ID и поддержка конкретного effort зависят от provider. Принятый config не является подтверждением, что provider уже предоставляет `gpt-6-luna` или все перечисленные reasoning levels.

Генератор из [тикета 17](../.scratch/phase-4/issues/17-generate-worker-topic-titles.md) запускает `opencode run --standalone` на Secretary server. Нужны OpenCode v2 в `PATH` и его provider authentication; установка только на удалённом Execution Node недостаточна. Shared background service не используется. Генератор не меняет Secretary/Worker runtime, их model/effort или пользовательские defaults OpenCode.

Модель явно передаётся в `--model provider/model#secretary-title`. Отдельный variant задаёт `settings.reasoningEffort` из конфигурации. Эти настройки относятся только к запросу названия. Provider packages могут отклонять или не поддерживать отдельные effort values; они должны поддерживать OpenCode `reasoningEffort` для выбранной модели. Генератор не выбирает другую модель при ошибке. Custom provider definitions из обычного OpenCode config не копируются; доступны встроенные providers и их сохранённая authentication.

System prompt берётся из `title-generation-prompt.md`, а пользовательское сообщение содержит только JSON с `Worker.intent`, который уходит в Dispatch как `OriginalUserIntent`. Worker title, Project/Policy snapshots, Worker profiles и `user.md` в запрос не входят. OpenCode получает временные `HOME`, config и state directories, отдельный primary agent с `steps = 1`, deny-all permissions и моделью без tools. Глобальные/project instructions, skills, MCP и пользовательские plugins не подмешиваются. Provider authentication остаётся в исходной data directory; credentials не копируются в prompt или временный config. Это trusted-local вызов harness, не отдельный Worker и не sandbox.

На генерацию отводится не более пяти секунд. Если OpenCode v2 отсутствует, model/effort отклонены provider, вызов завершается ошибкой или возвращает пустое/невалидное название, Topic получает читаемую первую строку задачи. Длинная строка сокращается до 60 рун с многоточием; непригодная или чувствительная строка заменяется на `Задача Worker`. Ошибка генерации не возвращается в Dispatch и не останавливает обработку Result после таймаута. JSON, многострочный текст, Markdown, ссылки и служебные/секретные маркеры не принимаются как ответ модели.

Выбранное название сохраняется вместе с Topic mapping. Replay и restart не запускают генерацию повторно и не переименовывают существующие Topics. Для старых событий без `intent` сохраняется прежнее поведение по Worker title.

Эти настройки не меняют `secretary.model`, `secretary.reasoning`, legacy `[runtime]`, Worker harness/model policy и compiled Profile hashes. Они не добавляются в системные инструкции Secretary или Worker.

### Применение и ошибки

Настройки входят в config snapshot и его JSON representation. Их изменение меняет config version и отображается в config diff под ключом `telegram`, но не создаёт diff runtime profiles. Изменение содержимого prompt меняет config version и diff `telegram.title_prompt`, не затрагивая compiled Secretary/Worker profiles.

При setup и открытии старого конфига создаётся `title-generation-prompt.md` рядом с `config.toml`. Уже существующий файл не перезаписывается. Если старый config не задаёт `title_prompt`, прямой loader может использовать встроенный template до materialization. Явно заданный путь должен существовать. Пустой файл или файл больше 64 KiB считается ошибкой; неуспешный reload сохраняет прежний prompt и snapshot.

После редактирования файла используйте существующее `Validate and apply` в Config либо поддерживаемый перезапуск сервера. Невалидные значения возвращают configuration error; неуспешный reload оставляет последний действующий snapshot без изменений. Менять always-on server config в рамках локального тестирования не требуется.
