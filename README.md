# Secretary v2

Secretary — персональный ассистент, который сохраняет одну непрерывную переписку и делегирует отдельные задачи постоянным Workers. Человеку не нужно самому переносить контекст между каналами, сессиями и машинами.

## Ключевые преимущества

1. **Одна непрерывная переписка.** Долговременная Personal Conversation — общий источник истории для подключённых каналов. Контекст модели может сжиматься, но сохранённая история и связи с Workers не исчезают. «Бесконечная» означает возможность продолжать переписку со временем, а не бесконечное окно контекста модели.
2. **Базовая память о владельце.** Редактируемый `user.md` хранит важные сведения и предпочтения, чтобы Secretary мог учитывать их в следующих разговорах.
3. **Свобода выбора модели и провайдера.** Модель задаётся через выбранный harness; OpenCode использует provider-qualified ID, а другие поддерживаемые harnesses можно выбирать отдельно. «Любой провайдер» означает совместимый с выбранным runtime, а не буквально каждый существующий API.
4. **Один Secretary управляет несколькими машинами.** Secretary server остаётся источником истины и направляет Workers на подключённые Execution Nodes. Разные машины работают с общей Personal Conversation и единым состоянием Workers.
5. **Worker — основная сущность работы.** У Worker есть собственные Turns, Attempts и Results; Follow-up продолжает существующего Worker, а новое поручение может создать другого. Worker остаётся доступен до явного закрытия.
6. **Работа не захватывает основную переписку.** Пользователь может продолжать разговор с Secretary, пока Worker занят. Подробную активность можно открыть отдельно; итоговый Result возвращается в Personal Conversation.

## Важная граница

Это описание продуктового обещания; доступность каналов, harnesses и нескольких Execution Nodes зависит от конфигурации и этапа развёртывания. Новая конфигурация по умолчанию использует Codex через `codex-acp` и native model/reasoning `default`; Claude Code выбирается отдельно. [Живая приёмка Codex Minimal MVP](.scratch/phase-5/reports/codex-mvp-final-20261010.md) завершена: Web/Telegram, Workers на omarchy и Mac, steering, очередь и resume. Claude Code отложен по решению пользователя; его приёмка не объявлена завершённой. Первый trusted-local deployment даёт Worker полный доступ в своём окружении и сам по себе не является sandbox или гарантией изоляции.

См. [быстрый старт](docs/quickstart.md), [конфигурацию](docs/configuration.md), [словарь домена](GLOSSARY.md), [runtime-контракты](docs/architecture/runtime-contracts.md) и [критерии релизной проверки](docs/phase4-release-gate.md).

Локальный CLI: `./secretary setup`, затем `./secretary start`. После setup команда доступна как `secretary` в `PATH`.


## Стек и архитектура

Go, SQLite (`modernc.org/sqlite`), HTTP API, WebSocket, JSON-RPC, MCP и ACP; Svelte, Vite и Tailwind CSS для веб-интерфейса.

- `secretaryd` — сервер, веб-сессии, переписка и управление Workers.
- `secretaryctl` — CLI для операций с состоянием Secretary.
- `secretary-node` — выполнение Workers на локальной или удалённой машине через исходящее соединение с сервером.
- `secretary-mcp` — MCP-инструменты Secretary с проверкой роли и capability.
- `internal/core` — SQLite, миграции и жизненный цикл Task, Worker, Attempt и Result.
- `internal/acp` — JSON-RPC-соединение с native runtime агента.
- `web/` — пользовательский интерфейс и Control Room для диагностики.

## Проверки

```sh
mise exec -- go test ./...
npm test --prefix web
npm run build:all --prefix web
```
