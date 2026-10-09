# Быстрый старт

Этот quickstart запускает локальный Secretary на macOS с OpenCode v2. Secretary использует `openai/gpt-6.1-sol` / `xhigh`; новые Workers — OpenCode с `openai/gpt-6-luna` / `xhigh`. Настройка двух outbound Nodes описана в [Private Node deployment](node-deployment.md).

## Требования

Нужны:

- macOS и `zsh`;
- установленный `mise`;
- установленный OpenCode v2.

Личная OpenCode authentication не используется. После setup owner один раз войдёт в provider для общего store Secretary и local Worker Node. Database создаётся локальной native serve operation без auth/provider call; setup не завершится успешно, если DB не появилась. Co-located Node setup/login/Doctor проверяют пару с Secretary, а standalone Node остаётся независимым. Duplicate и malformed deployment fields отклоняются до native CLI. Удалённый Node авторизуется в своём отдельном store.

## Запуск

Перейдите в корень репозитория и добавьте локальные бинарники в `PATH`:

```sh
cd /Users/beruseruko/projects/secretary-v2
export PATH="$HOME/.local/bin:$PATH"
```

Для постоянного PATH добавьте эту строку в `~/.zshrc`.

Запустите setup:

```sh
./secretary setup
```

Команда:

- собирает `secretaryd`, `secretaryctl` и `secretary-mcp`;
- создаёт локальный конфиг и внешние Profiles;
- создаёт локальный bootstrap token;
- собирает `secretary-node` и показывает явную full-access trusted Node policy;
- проверяет выбранный harness;
- проверяет наличие настроенного harness binary;
- создаёт один persistent native store для Secretary и co-located Worker Node; запускает `opencode serve --port 0 --stdio` с закрытым stdin, проверяет появление DB и останавливает setup с ошибкой, если инициализация не удалась; provider call/login не запускаются;
- задаёт Secretary `openai/gpt-6.1-sol` / `xhigh`, а новым Workers — `openai/gpt-6-luna` / `xhigh`.

Выполните provider login как явное действие owner; команда откроет OpenCode auth flow в общем store, не импортируя личные credentials:

```sh
secretary opencode login
```

Проверьте общий runtime store с обеих сторон:

```sh
secretary doctor
secretary node doctor
```

После явного provider login обе команды Doctor должны завершиться без ошибок. На чистой установке без login обе показывают отсутствие auth в общем store; выполните одну команду `secretary opencode login`. Для старого managed state вместо этого появляется migration requirement.

Запустите Secretary:

```sh
secretary start
```

Откроется User UI в браузере. Если браузер не открылся автоматически, команда сообщает адрес локального UI без печати bootstrap token. Bootstrap fragment передаётся только системному браузеру. Не публикуйте этот URL.

Откройте Control Room только для debug-сеанса:

```sh
secretary restart --debug
```

После запуска Control Room доступен по адресу:

```text
http://127.0.0.1:8081/control-room
```

## Первый Worker

1. Напишите сообщение в Conversation.
2. Если Secretary создаст Task, Worker появится в списке Workers.
3. Нажмите `Observe`, чтобы открыть activity и thread Worker.
4. Используйте Steering для текущего turn, Queue для следующего idle turn и Stop для отмены.
5. Terminal Result появится в Conversation и сохранится в SQLite.

## Управление сервером

```sh
secretary status          # состояние и режим
secretary logs            # поток server log, остановить Ctrl-C
secretary restart         # перезапуск в normal mode
secretary restart --debug # перезапуск с Control Room
secretary stop            # остановка
```

Обслуживание always-on сервера: data directory, backup, restore-check, Tailscale Serve и health checks описаны в `docs/always-on-runbook.md`.

## Модели и reasoning

В `config.toml` секция `[models]` задаёт не список моделей, а четыре alias:

```toml
[models]
secretary = "provider/model-id"
fast = "provider/fast-model-id"
smart = "provider/smart-model-id"
cheap = "provider/cheap-model-id"
```

Левая часть (`secretary`, `fast`, `smart`, `cheap`) сохраняется в UI и в вызовах MCP. Правая часть является настроенным значением модели. OpenCode применяет provider-qualified model ID и reasoning variant в native V2 agent config. Новые Workers без явного preference или Project pin получают `openai/gpt-6-luna` / `xhigh`; эти значения проверяются по observed inventory и не заменяются при ошибке. fx, Claude Code и Codex остаются доступными при явном выборе. Codex получает совместимый `AGENTS.md`, а конкретные model ID и reasoning effort передаются через `CODEX_CONFIG`; для fx применение зависит от его ACP-адаптера. В шаблоне справа стоят `default`, `fast`, `smart` и `cheap`, поэтому вы видите именно эти слова. `default` означает выбор модели самим harness, а не модель с именем `default`. Для OpenCode укажите provider ID, например `openai/gpt-5-codex`, а для Codex его собственный model ID, например `gpt-5.6-terra`, если такой ID доступен в вашей установке.

`secretary.reasoning` задаёт effort Secretary; `worker_policy.reasoning` независимо задаёт default новых Workers. Clean-install defaults: Secretary — `openai/gpt-6.1-sol` / `xhigh`, новые Workers — `openai/gpt-6-luna` / `xhigh`. Явные preferences и Project pins имеют приоритет над Worker defaults; существующие bindings не меняются. Конкретные model и reasoning pins проверяются по observed HarnessInstance inventory; OpenCode получает V2 variant, Codex - `CODEX_CONFIG`, а применение для fx зависит от ACP-адаптера.

После изменения `config.toml` перезапустите сервер или нажмите `Validate and apply` в Config. Markdown Profiles можно менять в Control Room во вкладке Profiles.

## Названия Worker Topics

Модель названий задаётся отдельно от Secretary и Worker runtime:

```toml
[telegram]
title_harness = "opencode"
title_prompt = "title-generation-prompt.md"
title_model = "gpt-6-luna"
title_model_reasoning = "minimal"
```

На Secretary server нужен OpenCode v2 с provider authentication в общем native store. Генератор запускает отдельный `--standalone` вызов без tools и не меняет Secretary/Worker runtime. Bare model ID означает `openai/<model>`; другой provider задаётся явно.

`title-generation-prompt.md` создаётся рядом с `config.toml`; существующий файл не перезаписывается. Меняйте его и применяйте config reload, чтобы новые Topics использовали обновлённый prompt. Старые configs получают эти defaults автоматически. При ошибке или таймауте Topic получает название из текста задачи. Детали описаны в [справочнике конфигурации](configuration.md).

## Локальные данные

Secretary хранит данные в:

```text
~/.local/share/secretary/
```

Основные файлы:

- `config.toml`: runtime, модели, tools и пути Profiles;
- `profiles/`: `secretary.md`, `worker.md`, `child-worker.md`;
- `secretary.db`: durable Conversation, Tasks, Attempts и Events;
- `opencode-native/`: общий persistent OpenCode native store Secretary и co-located Node;
- `logs/` и `secretaryd.log`: runtime logs;
- `environment`: bootstrap token и локальные capability values;
- `node/`: non-secret deployment config, per-Node identity, local outbox и logs. Co-located Node использует тот же OpenCode native store, что Secretary; remote Node хранит собственный store локально и подключается только исходящим соединением.

Конфиг и Profiles можно менять вручную. Для применения изменений перезапустите сервер или используйте Config в Control Room.

## Автозапуск после входа в macOS

```sh
secretary install-service
```

Для debug-режима:

```sh
secretary install-service --debug
```

Удалить LaunchAgent:

```sh
secretary uninstall-service
```

## Если запуск не удался

Сначала выполните:

```sh
secretary doctor
secretary logs
```

Частые причины:

- OpenCode provider не авторизован: завершите вход для нужного provider средствами OpenCode;
- отсутствует выбранный harness: установите его или исправьте `secretary.harness` в `config.toml`;
- порт `127.0.0.1:8081` занят другим процессом;
- выбран debug-сеанс, но Control Room запрашивается у normal-сервера.

Для настройки Node на второй машине используйте отдельные pairing и admin credentials. Client credential, Secretary runtime credential и Telegram token не подходят для Node. См. [Private Node deployment](node-deployment.md).

Для проверки самого CLI без изменения пользовательского состояния:

```sh
./scripts/secretary-cli-test.sh
```

Тесты используют временный `HOME`, fake ACP и fake `launchctl`.

## Обновление имени CLI

CLI называется `secretary`. Если ранее установлены macOS LaunchAgents, перед обновлением удалите их командами `uninstall-service` и `node uninstall-service` из прежней версии CLI. Затем установите службы заново через `secretary install-service` и при необходимости `secretary node install-service`. Данные и конфигурация остаются в прежних каталогах.
