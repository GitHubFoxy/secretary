# Архитектура Secretary

Документ описывает, как Secretary App устроен сейчас. Почему приняты ключевые решения, записано в [adr/](adr/). История обсуждений и отвергнутые варианты — в [discovery.md](discovery.md). Термины — в [GLOSSARY.md](../GLOSSARY.md).

## Коротко

Secretary — одна программа на Bun, которая работает на Host. Она запускает Claude Code и Codex по подписке пользователя, хранит их историю в SQLite и даёт пользователю один чат с Secretary Agent. Secretary Agent отвечает сам или поручает работу Specialists через MCP-инструмент `delegate`. Своей модели у продукта нет: весь интеллект живёт в harness.

```
Телефон (Capacitor) / браузер
        │ HTTPS + WebSocket
        ▼
Cloudflare Tunnel (app.scrtrbot.ru) ─▶ 127.0.0.1:3000 на Host
        │
┌──────────────────── secretary (Bun + TypeScript) ────────────────────┐
│ index.ts    HTTP, WebSocket, вход по коду сопряжения, MCP на :3001   │
│ session.ts  очередь сообщений, ход, прерывание, статус               │
│ claude.ts / codex.ts  протоколы harness (один интерфейс Turn)        │
│ chat.ts     сырые события harness → сообщения чата                   │
│ SQLite      sessions, events, pending_messages                       │
└──────────────────────────────────────────────────────────────────────┘
        │ stdin/stdout, по процессу на ход          ▲ MCP: delegate, send, list, read, stop
        ▼                                           │
  Secretary Agent (сессия #1, ~/.secretary) ────────┘
        │ delegate
        ▼
  Specialists: сессии Claude Code / Codex в папках проектов
```

## Части системы

### Клиент (`web/`)

Svelte 5 + Vite + Tailwind, без SvelteKit, код на JavaScript. Клиент не знает протоколов harness. Он получает от сервера готовые сообщения чата (`{kind, text, images, files}`), очередь и список сессий, а отправляет команды по WebSocket. Вложения сохраняются на Host в `~/.secretary/files/`; модель получает путь к ним, а фото и кадры видео — ещё и прямо в сообщении ([ADR 0009](adr/0009-media-originals-on-host.md)). В обратную сторону Secretary Agent показывает файл с Host инструментом MCP `send_file`: копия ложится туда же и появляется в чате как его сообщение.

Android-приложение — оболочка Capacitor, которая загружает `https://app.scrtrbot.ru` ([ADR 0004](adr/0004-capacitor-loads-the-site.md)). Поэтому новая версия интерфейса доходит до телефона без переустановки: `bun run --cwd web build` публикует сборку, а открытые клиенты проверяют `build-version.json` и перезагружаются, когда пользователь ничего не набирает. Нативная часть (`MainActivity.kt`) нужна для роли системного помощника: приложение открывается удержанием кнопки Power.

### Сервер (`server/`)

Один процесс Bun без npm-зависимостей ([ADR 0002](adr/0002-bun-without-dependencies.md)).

- `index.ts` — точка входа и адаптер браузера: открывает базу, раздаёт `web/dist`, пускает только устройства, прошедшие сопряжение (`auth.ts`), и переводит WebSocket-команды в вызовы `Sessions`. На каждую команду клиент получает ровно один ответ с его `requestId`: `reply` или `error`.
- `mcp.ts` — адаптер Secretary Agent: MCP-сервер на `127.0.0.1:3001` поверх тех же `Sessions`. MCP подключён только к Secretary Agent, поэтому дерево делегирования одноуровневое.
- `sessions.ts` — все Session на Host и правила для них: загрузка после перезапуска, создание с проверкой модели и reasoning, настройки, удаление, отчёт Specialist родителю.
- `session.ts` — Session: ход, прерывание, Immediate message, остановка, статусы `idle` / `running` / `interrupted` / `stopped`.
- `queue.ts` — очередь сообщений одной Session. Сообщение покидает её ровно один раз: попадает в историю, отменяется (`cancelled`) или помечается `delivery_uncertain`, и всё это одной транзакцией.
- `harness.ts`, `claude.ts`, `codex.ts` — адаптеры. Каждый реализует один ход (`Turn`) по контракту из `harness.ts`: сообщить об отправке и приёме, выдать события, по `AbortSignal` прервать ход (до отправки — не отправлять), вернуть «ход завершён успешно или нет».
- `chat.ts` — единственное место, которое знает, как событие Claude Code или Codex превращается в сообщение чата, и формат отчёта `[report #id harness folder]` ([ADR 0007](adr/0007-chat-messages-built-on-server.md)).
- `models.ts` — списки моделей и уровней reasoning у самих harness, с кэшем в памяти.

Тесты: `session.test.ts` проверяет `Sessions`, `Session` и очередь в процессе, на базе `:memory:` и сценарном `Turn` вместо настоящих harness. `harness.test.ts` проверяет контракт адаптеров на фейковых `claude` и `codex` из `test-fixtures/`.

### Хранилище

SQLite в `server/secretary.sqlite`:

- `sessions` — Session: harness, модель, reasoning, папка, id сессии harness для resume, статус, родитель, имя, скрыта ли.
- `events` — сырые события harness как есть, плюс события самого Secretary (`user`, `status`, `error`, `cancelled`, `delivery_uncertain`). Агенты читают их через MCP `read`, браузер — через `chat.ts`.
- `pending_messages` — очередь сообщений, которые ещё не ушли в ход. Она переживает перезапуск.

Коды сопряжения и токены устройств лежат отдельно, в `~/.secretary/auth.sqlite`.

## Как проходит сообщение

1. Клиент шлёт `send`. Сервер сохраняет сообщение в `pending_messages` и только потом подтверждает отправку клиенту.
2. Если Session свободна, начинается ход. Адаптер запускает harness (Claude Code с `--resume`, Codex через `app-server` и `thread/resume`) и передаёт сообщение.
3. Когда harness принял сообщение, оно одной транзакцией переходит из очереди в историю. Клиент видит его как отправленное.
4. События хода пишутся в `events`. Те, из которых получается сообщение чата, сразу уходят клиенту.
5. После хода процесс harness закрывается, статус становится `idle`. Следующее сообщение снова поднимает harness через resume.
6. Если у Session есть родитель, последний ответ уходит Secretary Agent как `[report …]`. На неважный отчёт Secretary Agent отвечает `NO_REPLY`, и клиент его не показывает.

**Immediate message.** Если пользователь отправляет сообщение во время хода, ход прерывается, и сообщение уходит следующим. Claude Code прерывается control-запросом, Codex — `turn/interrupt`. Если harness не ответил за 10 секунд, процесс убивается. Сообщения, отправленные кнопкой «в очередь», ждут конца хода.

**Надёжность.** Сообщение, которое уже передано harness, но приём которого не подтвердился (например, Secretary упал), не отправляется повторно. Оно помечается как `delivery_uncertain`, чтобы агент не выполнил задачу дважды.

## Процессы

Каждый harness запускается через `setsid`, то есть в своей группе процессов. Остановка Session убивает всю группу вместе с инструментами, которые запустил агент. Если падает сам Secretary, остатки убивает systemd (`KillMode=control-group` в `deploy/secretary.service`) ([ADR 0006](adr/0006-systemd-owns-harness-processes.md)).

Агенты работают с полным доступом и без подтверждений: Claude Code с `--dangerously-skip-permissions`, Codex с `danger-full-access`. Песочницы пока нет.

## Развёртывание сейчас

- Host — ноутбук omarchy (Arch Linux), пользовательская служба `secretary.service`.
- Снаружи сервер доступен через Cloudflare Tunnel (`secretary-tunnel.service`) по адресу `app.scrtrbot.ru`.
- Вход: `secretary pair` выдаёт одноразовый код на 10 минут, браузер получает cookie на год.
- Пользователь один — владелец.

## Куда идём

Это направление, а не обещания. Задачи — в GitHub Issues.

- **Параллельная работа.** Secretary Agent раскладывает задачи по Specialists в папках проектов, Specialist распараллеливает работу в git worktree (реализация + ревью) и сам сливает готовое — #33, #30.
- **Голос и телефон.** Голосовой звонок по кнопке Power (#3), оверлей вместо полного приложения (#4), голосовые сообщения (#7), звонок по сотовой связи (#8), сценарий «Что за фильм?» (#9).
- **Наблюдаемость.** Запросы агентов через PAG в ClickHouse и отчёт «что мы сегодня сделали» (#10). Архив вместо удаления сессий (#38).
- **Другие пользователи.** Сейчас это Developer Edition: свой Host и своя подписка. Потом Hosted Edition: контейнер на пользователя, окружение и inference даёт продавец ([ADR 0008](adr/0008-one-core-two-editions.md), [ADR 0005](adr/0005-native-harness-on-user-subscription.md)). Монетизация — #15. Для этого понадобятся мультипользовательский вход, песочницы и защита локального MCP (#32).
