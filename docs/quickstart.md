# Быстрый старт

Этот quickstart запускает локальный Secretary на macOS с fx и gpt-5.6-luna по умолчанию.

## Требования

Нужны:

- macOS и `zsh`;
- установленный `mise`;
- установленный fx;
- выполненный вход в нужный provider, например `fx login codex`.

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
- создаёт bootstrap token;
- проверяет выбранный harness;
- проверяет доступность fx;
- сохраняет model ID `gpt-5.6-luna` в настройках Secretary.

Проверьте окружение:

```sh
secretary doctor
```

Ожидаемый результат:

```text
Doctor found no problems.
```

Запустите Secretary:

```sh
secretary start
```

Откроется User UI в браузере. Если браузер не открылся автоматически, команда напечатает pairing URL с bootstrap fragment. Не публикуйте этот URL.

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

## Модели и reasoning

В `config.toml` секция `[models]` задаёт не список моделей, а четыре alias:

```toml
[models]
secretary = "provider/model-id"
fast = "provider/fast-model-id"
smart = "provider/smart-model-id"
cheap = "provider/cheap-model-id"
```

Левая часть (`secretary`, `fast`, `smart`, `cheap`) сохраняется в UI и в вызовах MCP. Правая часть является настроенным значением модели. OpenCode применяет provider ID в native agent config. Codex получает совместимый `AGENTS.md`, а конкретные model ID и reasoning effort передаются через `CODEX_CONFIG`; для fx применение зависит от его ACP-адаптера. В шаблоне справа стоят `default`, `fast`, `smart` и `cheap`, поэтому вы видите именно эти слова. `default` означает выбор модели самим harness, а не модель с именем `default`. Для OpenCode укажите provider ID, например `openai/gpt-5-codex`, а для Codex его собственный model ID, например `gpt-5.6-terra`, если такой ID доступен в вашей установке.

`[runtime].reasoning` сейчас является общей настройкой усилия рассуждения для новой версии всех Profiles. Это не идентичность модели: одна и та же модель может работать с разным effort, поэтому параметр оставлен отдельно. Для OpenCode он попадает в native agent config. Для Codex и fx он сохраняется в Profile metadata и совместимом delivery, но ACP-адаптер не обещает, что конкретный harness применит этот параметр.

После изменения `config.toml` перезапустите сервер или нажмите `Validate and apply` в Config. Markdown Profiles можно менять в Control Room во вкладке Profiles.

## Локальные данные

Secretary хранит данные в:

```text
~/.local/share/secretary/
```

Основные файлы:

- `config.toml`: runtime, модели, tools и пути Profiles;
- `profiles/`: `secretary.md`, `worker.md`, `child-worker.md`;
- `secretary.db`: durable Conversation, Tasks, Attempts и Events;
- `logs/` и `secretaryd.log`: runtime logs;
- `environment`: bootstrap token и локальные capability values.

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

- fx не авторизован: выполните вход в нужный provider через `fx login`;
- отсутствует выбранный harness: установите его или исправьте `runtime.harness` в `config.toml`;
- порт `127.0.0.1:8081` занят другим процессом;
- выбран debug-сеанс, но Control Room запрашивается у normal-сервера.

Для проверки самого CLI без изменения пользовательского состояния:

```sh
./scripts/secretary-cli-test.sh
```

Тесты используют временный `HOME`, fake ACP и fake `launchctl`.

## Обновление имени CLI

CLI называется `secretary`. Если ранее установлены macOS LaunchAgents, перед обновлением удалите их командой `uninstall-service` из прежней версии CLI, затем установите заново через `secretary install-service`. Данные и конфигурация остаются в прежних каталогах.
