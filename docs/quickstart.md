# Быстрый старт

Этот quickstart запускает локальный Secretary на macOS с Codex через native `codex-acp`. Новые Secretary и Workers используют модель и reasoning по умолчанию выбранного harness. Claude Code доступен при явном выборе и собственной авторизации. Настройка outbound Nodes описана в [Private Node deployment](node-deployment.md). Полная live приёмка описана в [Phase 5](../.scratch/phase-5/issues/05-deploy-and-live-acceptance.md) и ещё не завершена.

## Требования

Нужны macOS, `zsh`, `mise`, Node.js/npm, установленный Codex и его рабочая авторизация на машине исполнения. Проверьте её без передачи credentials между машинами:

```sh
codex login status
```

Если вход требуется, выполните `codex login`. Setup устанавливает отсутствующий `@agentclientprotocol/codex-acp@1.12.0` и запускает readiness фактического adapter. Readiness проверяет auth и native capabilities; успешный status не заменяет реальную проверку model call, если provider отклоняет запросы.

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

Команда собирает `secretaryd`, `secretaryctl`, `secretary-mcp` и `secretary-node`, создаёт локальный конфиг, внешние Profiles, bootstrap token и co-located Node config. Новые defaults: `codex`, model/reasoning `default`; setup не создаёт OpenCode native store и не переписывает глобальную конфигурацию Codex.

Проверьте установленный runtime:

```sh
secretary doctor
secretary node doctor
```

Co-located Workers используют durable outbound `secretary-node`, как remote Workers. Для запуска Node требуется enrollment; выполните pairing по [руководству Node](node-deployment.md). На каждой машине harness авторизуется самостоятельно. Для выбранного Claude Code проверьте `claude auth status`; ordinary active input требует реального native steering и не заменяется очередью при unsupported operation.

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
4. Отправьте обычный текст для steering активной Attempt; `/q текст` сохраняет очередь следующего Follow-up. Очередь сразу видна в observer. Stop отменяет Attempt, Close закрывает Task и отменяет оставшуюся очередь.
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

В новой конфигурации Secretary и Workers задаются независимо:

```toml
[secretary]
harness = "codex"
model = "default"
reasoning = "default"

[worker_policy]
default_harness = "codex"
preferred_harnesses = ["codex", "claude_code"]
model = "default"
reasoning = "default"
```

`default` означает native выбор harness. Явные Worker preferences и Project pins имеют приоритет; существующие bindings не меняются при смене defaults. Codex получает профиль и pins через session-scoped `CODEX_CONFIG`; чужие OpenCode provider prefixes не наследуются. Конкретные pins требуют подтверждения adapter, ошибки не подменяются другим model ID. Секция `[models]` сохраняет aliases для существующих конфигураций; новые defaults не требуют OpenCode.

После изменения `config.toml` перезапустите сервер или нажмите `Validate and apply` в Config. Markdown Profiles можно менять в Control Room во вкладке Profiles.

## Названия Worker Topics

В новом default Topic получает сохранённый Worker title либо безопасно сокращённый Task prompt, без дополнительного model turn:

```toml
[telegram]
title_harness = "deterministic"
```

Историческое `title_harness = "opencode"` остаётся совместимым и требует собственного OpenCode runtime/auth. Его `title_prompt`, `title_model` и `title_model_reasoning` сохраняются; подробности — в [справочнике конфигурации](configuration.md).

## Историческая конфигурация OpenCode

Существующие OpenCode/FX bindings и данные сохраняются. Для явно настроенного OpenCode прежний setup и auth выполняются в выбранном native store:

```sh
secretary opencode login
secretary doctor
secretary node doctor
```

Этот login авторизует OpenCode, а Codex использует собственную авторизацию. Исторические provider-qualified model IDs и reasoning variants относятся к OpenCode и не являются новым default Codex/Claude Code.

## Локальные данные

Secretary хранит данные в:

```text
~/.local/share/secretary/
```

Основные файлы:

- `config.toml`: runtime, модели, tools и пути Profiles;
- `profiles/`: `secretary.md`, `worker.md`, `child-worker.md`;
- `secretary.db`: durable Conversation, Tasks, Attempts и Events;
- `opencode-native/`: исторический OpenCode native store, если OpenCode явно настроен;
- `logs/` и `secretaryd.log`: runtime logs;
- `environment`: bootstrap token и локальные capability values;
- `node/`: non-secret deployment config, per-Node identity, continuation mappings, local outbox и logs. Remote Node хранит свою native историю на машине исполнения и подключается исходящим соединением.

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

- Codex не авторизован или provider отклоняет запросы: проверьте `codex login status`, при необходимости выполните `codex login`;
- выбран Claude Code без рабочей авторизации или квоты: проверьте `claude auth status` и доступность provider;
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
