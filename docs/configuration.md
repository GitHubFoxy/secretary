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

## Runtime defaults Secretary и Workers

Чистый setup задаёт `secretary.harness = "opencode"` и `worker_policy.default_harness = "opencode"`. Secretary использует `openai/gpt-6.1-sol` / `xhigh`; новые Workers — `openai/gpt-6-luna` / `xhigh` из `worker_policy.model` и `worker_policy.reasoning`. fx, Claude Code и Codex остаются selectable через явный harness choice и Project policy.

```toml
[secretary]
harness = "opencode"
model = "openai/gpt-6.1-sol"
reasoning = "xhigh"

[worker_policy]
default_harness = "opencode"
model = "openai/gpt-6-luna"
reasoning = "xhigh"
```

Worker defaults применяются только при создании нового binding, если отсутствует соответствующий Project pin или явное preference. Явные preferences обязаны соблюдать Project policy. Model/reasoning проверяются по observed HarnessInstance; ошибка не включает fallback. Reload влияет на следующий Spawn, но не меняет existing binding, Follow-up или idempotent replay. `worker_policy.reasoning` независим от Secretary; допустимы `default`, `none`, `minimal`, `low`, `medium`, `high`, `xhigh`, поддержка зависит от harness/model. Для legacy config без этого ключа сохраняется прежняя компиляция профиля.

### Addressed reply для Secretary

`secretary.reply_contract = "addressed-reply-v1"` — отдельная opt-in настройка; чистая установка оставляет её выключенной. Версия предназначена для Secretary с проверенным OpenCode v2 ACP terminal contract. При таком профиле отсутствие assistant message chunks само по себе не завершает turn успешно: runtime требует успешный terminal RPC, `stop_reason=end_turn`, завершённый native event drain и ровно одну canonical addressed reply для точной пары server-issued Secretary turn/input в durable Core ledger. Ответ сохраняется через `reply_to_user`; его текст не копируется в untyped Summary.

Terminal evidence — внутренний typed adapter/runtime contract и не входит в public DTO. Отсутствующее или malformed evidence, иной stop reason, RPC/provider error, незавершённый drain или reply другой identity блокируют reply-only completion; наличие assistant/progress chunks также не проходит именно этот gate. Для verified OpenCode v2 final text принимается только из parent assistant message chunks, завершённых native `end_turn`; непроверенный `session/prompt` `response.Summary` игнорируется и не заменяет ни chunks, ни durable reply. Assistant final text с валидными messageId/end_turn сохраняет обычное поведение. Worker и legacy/unaddressed Secretary gate не включаются в этот opt-in; внешний Profile и production config автоматически не переписываются.

OpenCode Worker delivery использует V2 `agents[].system`, qualified `model`, `providers[].models[].variants[].settings.reasoningEffort` и ordered `permissions`. Adapter явно выбирает управляемый mode и наблюдаемый model/reasoning variant через ACP `session/set_config_option`; формат ACP — `provider/model/variant`, а не CLI `provider/model#variant`. Native mode ID и description marker связаны с фактическими instructions и policy. Перед prompt adapter проверяет echoed marker, model и effort. В OpenCode v2.0.22 config plugins активируются асинхронно, поэтому transient `mode/model/effort not found` повторяет только тот же explicit выбор в пределах пяти секунд. Отсутствующий профиль, неверный marker или неподдерживаемая настройка дают ошибку, а не переход на `build`/`plan`. Resume загружает исходный native session ID, отделяет history replay от новой Attempt и повторно подтверждает настройки; новый Result не содержит replay. Профиль Worker разрешает только перечисленные в `tools.allow_tools` actions. Secretary получает только server-owned `secretary_*` MCP actions; built-in shell/read/edit ему не открываются. ACP запускается с per-profile config и точным изолированным HOME/config/project environment, без автоматической загрузки пользовательских global instructions, plugins и skills. Credentials не попадают в prompt/config.

При создании Worker выбранный managed Profile фиксируется вместе с фактической HarnessInstance и Project model/reasoning pins. Версия, имя, точные инструкции и skills, список `allow_tools`, source identity и resolved delivery/binding hash хранятся в приватной колонке `workers.profile_snapshot`; это поле не входит в Worker JSON, DTO и event payloads. Dispatch, Follow-up, Resume и idempotent replay используют этот сохранённый snapshot, а не заново загружают текущий Profile или defaults. Новый Worker любого harness, включая FX, без authoritative Worker template source не создаётся. OpenCode без snapshot, malformed/hash-invalid Profile и несоответствие Harness/model/reasoning/Project policy завершаются безопасной ошибкой до runtime start; runtime guard также требует managed Profile. Для уже привязанного Worker новый Profile автоматически не подставляется. Единственное исключение — существующая legacy FX запись без snapshot, доказуемая durable Worker binding и pre-marker значение `workers.worker_template_required = false`; additive schema marker не меняет её binding/history. Node получает отдельный legacy marker только для такого пустого FX template; общий `harness == fx` обход и частично заполненный template отвергаются. Старые Claude/Codex Workers без snapshot fail closed, а не получают OpenCode defaults.

### Постоянное native state harness

Архитектура для обоих типов агентов: `Secretary/Worker → runtime adapter → выбранный harness`. Domain state и native state различны: Secretary server хранит Conversation и Worker binding, а адаптер управляет native sessions, history и специфическими настройками harness. Возможность выбрать любой поддерживаемый harness не означает, что его обычную пользовательскую DB безопасно переиспользовать.

**Выделенный shared native store:** на чистой установке `sex setup` выбирает один private data home Secretary и co-located Worker Node: `$HOME/.local/share/secretary/opencode-native`. Оба selection manifest ссылаются на этот exact path. `sex node setup` на Secretary host использует тот же store и не создаёт второй DB. Каждый runtime передаёт этот путь как `XDG_DATA_HOME`; OpenCode хранит native DB/auth/history в `opencode/`. Setup один раз запускает `opencode serve --port 0 --stdio` с закрытым stdin: native server создаёт `opencode.db` на случайном loopback port и сразу завершается. Это локальная DB bootstrap операция без `auth list`, provider call и login; setup проверяет DB и прекращает работу с видимой ошибкой, если инициализация не удалась. Выделенный remote Node использует собственный `<NODE_DATA>/opencode-native` и не делит native DB с Secretary или другими Nodes. Повторный setup, restart, upgrade, новая Attempt и Follow-up сохраняют выбранный store, auth и history. Secretary runtime, local Worker runtime, title generator, probes, model inventory и Doctor на co-located host используют один путь. ACP config остаётся временной и изолированной от Workspace; native history хранится вне Workspace.

Перед Doctor, provider login и owner selection Go строго проверяет Node deployment config и selection manifests: отклоняет повторные decoded keys (включая escaped aliases), case-insensitive aliases, неизвестные поля, malformed/trailing JSON и управляющие символы. Selector возвращает Shell CLI проверенные `mode`, canonical `data_home`/`data_dir`, `standalone` и `include_opencode`; Shell не читает deployment JSON через awk и не перечитывает manifests. Co-located Node Doctor/login/setup сравнивают выбранный Node store с Secretary; standalone Node использует независимые Go-selected data directory и store. Owner transition отдельно fail-closed проверяет config и сохранённые managed-session mappings. При ошибке или некорректной записи selector native CLI не запускается. На новой установке личная `~/.local/share/opencode` не читается, не меняется, не очищается и не связывается через symlink. Для старой установки/Node с managed sessions legacy path закрепляется selection record; runtime продолжает использовать exact path, пока owner не выполнит отдельный переход. Setup не запускает там `auth list`, Doctor блокирует readiness, а login отказывается от redirect/import. На co-located host `sex opencode login` авторизует единственный shared store; `sex node opencode login` указывает на тот же store, поэтому выполнять обе команды не нужно. Remote Node отдельно авторизуется через `sex node opencode login`. Missing auth остаётся видимой ошибкой Doctor/inventory. Credentials не копируются из личного store или в prompt, config, CLI args, activity и diagnostics; ambient provider API key/token/cloud credential variables очищаются из OpenCode subprocess environment. Только секреты для narrow server-owned MCP передаются в environment соответствующего MCP process. Data directories имеют `0700`, native DB/auth/WAL/SHM — `0600` под private `umask 077`; уже выбранный legacy store не chmod-ится.

Старая установка распознаётся по `secretary.db` или прежнему `config.toml`; старый Node — по `node-state.json` или deployment config без selection record. До перехода они сохраняют прежний `XDG_DATA_HOME` (или прежний default `$HOME/.local/share`) и получают migration requirement. `sex setup` не переключает старую fx-only установку автоматически. Владелец может выполнить `sex opencode select-shared-store`, только когда Secretary и co-located Node остановлены. Команда идемпотентно закрепляет общий новый path, но отказывается при OpenCode session mappings, конфликтующих выборках, незавершённой записи или непустом ещё не выбранном target store. Она не переносит credentials/history, не изменяет mappings/session IDs и не трогает старый fx state или личную OpenCode DB; после перехода owner отдельно запускает `sex opencode login`. При отказе прежние selections и данные остаются без изменений. Для удалённого Node этот переход не используется: его private store остаётся локальным.

Исторический owner-approved backup/reset на omarchy улучшил первоначальный round-trip, но не доказывал corruption DB. Отдельная гонка managed mode в OpenCode v2.0.22 устранена в ticket31 ограниченным exact selection/retry после публикации native config catalog; Resume использует исходный ID без reset. Это historical context, а не текущий missing-mode blocker.

OpenCode readiness не выводится из непустого текста `auth list`: Node probe и оба `sex doctor` запускают `opencode auth list --format json --standalone` в Go-проверенном выбранном store. Ready только при exit code 0 и корректном JSON, где хотя бы одно `connections[].type` равно `credential`; пустой список, malformed JSON, ошибка команды и credentials только в environment не проходят. Probe использует ограниченный timeout и private server вместо background service. HOME/XDG/config и provider credentials из ambient environment очищаются; личный `~/.local/share/opencode` не используется как fallback. Model/reasoning по-прежнему берутся только из native inventory.

**Статус реализации:** общий local store, отдельный remote Node store, owner-only legacy transition, runtime/probe/Doctor/login routing и regressions реализованы в ticket33. До read-only acceptance owner завершил headless OpenAI OAuth в production shared store; источник — handoff от 6 октября 2026 (`/private/tmp/secretary-auth-probe-task.md`). Selected-store native JSON и auth/catalog/ACP acceptance подтвердили сохранённый credential (`authenticated=true`); сама acceptance не запускала новый `auth login`. Отсутствие login-команды во время read-only проверки не означает отсутствие auth. Новая установка или отдельный remote store по-прежнему требуют явного login. Production harness switch/restart, Worker execution и Telegram acceptance остаются pending; см. [ticket 31](../.scratch/phase-4/issues/31-make-opencode-v2-default-harness.md) и [ticket 33](../.scratch/phase-4/issues/33-isolate-opencode-native-state.md).

Setup не переписывает существующий `config.toml` или external Profiles. Старые explicit fx настройки остаются fx; существующие Workers сохраняют immutable HarnessInstance, runtime adapter и локальный native session history. Node публикует OpenCode только после успешных version/auth checks, наблюдения native model API и ACP initialize; версия CLI сама по себе не означает readiness. Plaintext `opencode models` не доказывает reasoning. Inventory получает enabled model IDs и `variants[].settings.reasoningEffort` из частного native server в том же provider store, без model call; bootstrap environment не добавляет requested variants. Текущий domain inventory хранит общий набор reasoning levels; exact model/effort дополнительно подтверждаются при старте runtime. OpenCode v2.0.22 ACP не передаёт explicit tool identity: фактические read/MCP calls проверены, но normalized `tool_call`/`tool_result` capabilities не объявляются и имя не угадывается из title. Отсутствующий OpenCode в legacy Node config с явным `include_opencode = false` нужно включить при rollout отдельно.

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

Модель явно передаётся в `--model provider/model#secretary-title`. Отдельный variant задаёт `settings.reasoningEffort` из конфигурации. Эти настройки относятся только к запросу названия. Provider packages могут отклонять или не поддерживать отдельные effort values; они должны поддерживать OpenCode `reasoningEffort` для выбранной модели. Генератор не выбирает другую модель при ошибке. Custom provider definitions из обычного OpenCode config не копируются. Используются только provider settings, доступные в managed runtime config, и authentication из выбранного Secretary native store; личная authentication не импортируется.

System prompt берётся из `title-generation-prompt.md`, а пользовательское сообщение содержит только JSON с `Worker.intent`, который уходит в Dispatch как `OriginalUserIntent`. Worker title, Project/Policy snapshots, Worker profiles и `user.md` в запрос не входят. OpenCode получает временные `HOME`, config и state directories, отдельный primary agent с `steps = 1`, deny-all permissions и моделью без tools. `XDG_DATA_HOME` остаётся persistent Secretary store, выбранным при setup; это тот же store, что и у Secretary runtime и `sex doctor`. Глобальные/project instructions, skills, MCP и пользовательские plugins не подмешиваются. Credentials не копируются в prompt или временный config. Это trusted-local вызов harness, не отдельный Worker и не sandbox.

На генерацию отводится не более пяти секунд. Если OpenCode v2 отсутствует, model/effort отклонены provider, вызов завершается ошибкой или возвращает пустое/невалидное название, Topic получает читаемую первую строку задачи. Длинная строка сокращается до 60 рун с многоточием; непригодная или чувствительная строка заменяется на `Задача Worker`. Ошибка генерации не возвращается в Dispatch и не останавливает обработку Result после таймаута. JSON, многострочный текст, Markdown, ссылки и служебные/секретные маркеры не принимаются как ответ модели.

Выбранное название сохраняется вместе с Topic mapping. Replay и restart не запускают генерацию повторно и не переименовывают существующие Topics. Для старых событий без `intent` сохраняется прежнее поведение по Worker title.

Эти настройки не меняют `secretary.model`, `secretary.reasoning`, legacy `[runtime]`, Worker harness/model policy и compiled Profile hashes. Они не добавляются в системные инструкции Secretary или Worker.

### Применение и ошибки

Настройки входят в config snapshot и его JSON representation. Их изменение меняет config version и отображается в config diff под ключом `telegram`, но не создаёт diff runtime profiles. Изменение содержимого prompt меняет config version и diff `telegram.title_prompt`, не затрагивая compiled Secretary/Worker profiles.

При setup и открытии старого конфига создаётся `title-generation-prompt.md` рядом с `config.toml`. Уже существующий файл не перезаписывается. Если старый config не задаёт `title_prompt`, прямой loader может использовать встроенный template до materialization. Явно заданный путь должен существовать. Пустой файл или файл больше 64 KiB считается ошибкой; неуспешный reload сохраняет прежний prompt и snapshot.

После редактирования файла используйте существующее `Validate and apply` в Config либо поддерживаемый перезапуск сервера. Невалидные значения возвращают configuration error; неуспешный reload оставляет последний действующий snapshot без изменений. Менять always-on server config в рамках локального тестирования не требуется.
