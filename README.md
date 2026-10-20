# Secretary v2

**Secretary** — персональный ассистент и оркестратор задач на Go, объединяющий долгосрочную контекстную переписку с надежным делегированием поручений фоновым воркерам.

Система устраняет необходимость вручную переносить рабочий контекст, историю и результаты между разными сессиями, терминалами и физическими машинами.

---

## Архитектура

```mermaid
flowchart TD
    subgraph ClientLayer ["Интерфейсы"]
        Web["Web Client (Svelte / Control Room)"]
        CLI["secretaryctl (CLI)"]
        MCPClient["Внешние клиенты (MCP)"]
    end

    subgraph CoreLayer ["Центральный сервер"]
        Daemon["secretaryd"]
        DB[(SQLite / modernc.org)]
    end

    subgraph ExecutionLayer ["Исполнение (Execution Nodes)"]
        Node1["secretary-node (Локальная машина)"]
        Node2["secretary-node (Удаленный хост)"]
        Runtime1["Agent Runtime (ACP / Codex)"]
        Runtime2["Agent Runtime (ACP / Codex)"]
    end

    Web <-->|HTTP / WebSocket| Daemon
    CLI <-->|HTTP API| Daemon
    MCPClient <-->|MCP Protocol| Daemon

    Daemon <--> DB

    Daemon <-->|WebSocket / JSON-RPC| Node1
    Daemon <-->|WebSocket / JSON-RPC| Node2

    Node1 --> Runtime1
    Node2 --> Runtime2
```

### Компоненты системы

- **`secretaryd`** — центральный сервер и координатор: управление очередью задач, сессиями переписки, ролевым доступом и оркестрацией воркеров.
- **`secretaryctl`** — CLI-утилита для администрирования, запуска задач и инспекции текущего состояния системы.
- **`secretary-node`** — агент выполнения воркеров на целевой машине. Подключается к серверу по исходящему защищенному каналу, исключая необходимость входящих портов.
- **`secretary-mcp`** — реализация протокола Model Context Protocol (MCP) для предоставления инструментов внешним LLM-клиентам.
- **`internal/core`** — ядро бизнес-логики: хранилище SQLite, миграции схемы, жизненный цикл задач (Task, Worker, Attempt, Result).
- **`internal/acp`** — слой адаптера для взаимодействия с агентскими средами по протоколу Agent Client Protocol (ACP).
- **`web/`** — адаптивный веб-интерфейс и Control Room (Svelte, Vite, Tailwind CSS).

---

## Ключевые возможности

1. **Непрерывная долговременная переписка.** История диалога сохраняется в локальной базе данных и доступна со всех подключенных клиентов. Основной контекст беседы не теряется при смене сессий или перезапуске воркеров.
2. **Память и персонализация.** Явно запрошенные владельцем факты и предпочтения сохраняются в редактируемых `memory.md` и `user_preferences.md`; существующий `user.md` сохраняется и остаётся доступным для совместимости. [Документация памяти](docs/memory.md).
3. **Асинхронное делегирование воркерам.** Пользователь может продолжать общение с ассистентом, пока воркер выполняет объемную фоновую задачу. Промежуточный прогресс доступен в отдельном потоке, а финальный результат возвращается в общий чат.
4. **Управление несколькими машинами.** Единый сервер `secretaryd` может распределять нагрузку и направлять задачи на разные подключенные узлы (`secretary-node`).
5. **Гибкость сред исполнения.** Поддержка запуска агентских runtime с выбором провайдеров и моделей через стандартизированный протокол ACP.

---

## Архитектурные особенности и ограничения

- **Модель доверия и изоляция:** В базовой локальной конфигурации воркер выполняется с правами текущего пользователя. Для изолированного запуска команд изоляция настраивается на уровне целевого узла (контейнеры, cgroups).
- **Среды исполнения:** Основной рабочий harness оптимизирован под Codex через `codex-acp`. Архитектура спроектирована модульно и предусматривает подключение других сред через общий контракт runtime.
- **Сетевая связанность:** Узлы `secretary-node` инициируют исходящие соединения к `secretaryd`, что обеспечивает надежную работу через NAT без проброса портов.

---

## Быстрый старт и запуск

### Установка одной командой (Linux/macOS)

Пользовательская установка берёт готовый релизный бинарник. Go, mise, npm и клонирование репозитория не нужны. Скрипт выбирает платформу и последнюю опубликованную версию с полным набором файлов для неё, включая prerelease. Для фиксации версии можно задать `SECRETARY_VERSION=0.1.0-alpha.5`.

```sh
curl -fsSL --retry-all-errors --retry 5 --retry-delay 2 https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install.sh | bash
```

На Linux требуются systemd user session, zsh, Python 3 и OpenSSL; на macOS — штатные системные утилиты. Подробнее о подписи приложения и Gatekeeper: [macOS binary installation](docs/macos-binary-install.md).

Установка разработчика **из исходников** по-прежнему доступна по отдельному адресу; она требует Git, Go 1.27.1+ или mise:

```sh
curl -fsSL https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install-from-source.sh | bash
```

На Linux повторная установка автоматически обновляет **собственные** user units `secretary.service` и `secretary-node.service`, если появление чужого файла в `~/.local/bin` требует перехода на приватный релизный bin. Это обновление перезапускает оба сервиса — завершите или сохраните активную работу перед переходом.

**Если ранее ставили Secretary из исходников с systemd:** бинарный установщик не заменяет чужие `secretary.service` и `secretary-node.service` и откажется с `Conflicting systemd unit`. Сначала завершите активную работу, убедитесь, что оба юнита принадлежат старой source-установке, и вручную сохраните их вне каталога активных units:

```bash
systemctl --user disable --now secretary-node.service secretary.service
backup="$HOME/.local/state/secretary-source-units-backup"
mkdir -p "$backup"
for unit in secretary.service secretary-node.service; do
  source_unit="$HOME/.config/systemd/user/$unit"
  if [ -e "$source_unit" ] || [ -L "$source_unit" ]; then
    if [ -e "$backup/$unit" ] || [ -L "$backup/$unit" ]; then
      echo "Backup already exists: $backup/$unit; resolve manually" >&2
      exit 1
    fi
    mv "$source_unit" "$backup/$unit"
  fi
done
systemctl --user daemon-reload
```

Теперь повторите бинарную установку. Это **не автоматическая миграция** состояния: старые unit-файлы остаются в резервной копии, и одновременно запускать два Server с одной базой SQLite нельзя. Установщик автоматически обновляет **только собственные** systemd units.

В существующем checkout: `./secretary setup --interactive`. Можно выбрать **Codex или Claude Code** — оба одновременно не требуются. `secretary config` открывает терминальную конфигурацию harness, локального Node и Telegram; браузер и отдельный аккаунт Secretary не нужны.

```sh
secretary start --no-open
secretary node start
secretary doctor
secretary install-service
```

На Linux используется systemd user, на macOS — launchd. Подробнее: [терминальная установка](docs/terminal-install.md) и [быстрый старт](docs/quickstart.md).

### Тестирование и сборка

```sh
# Тесты backend (Go)
go test ./...

# Тесты и сборка веб-интерфейса
npm test --prefix web
npm run build:all --prefix web
```

Подробная документация: [быстрый старт](docs/quickstart.md), [конфигурация](docs/configuration.md), [глоссарий домена](GLOSSARY.md), [архитектурные контракты](docs/architecture/runtime-contracts.md).
