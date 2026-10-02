# Phase 1: описание удалённого Pi source snapshot

Дата: 2026-10-02.

Этот документ сохраняет назначение, устройство, реализованные возможности и ограничения каталога `phase-1/` перед его удалением по просьбе пользователя. Это описание, а не резервная копия исходников. Исходники остаются в Git history.

## Происхождение и точка восстановления

Согласно `phase-1/SECRETARY-SNAPSHOT.md`, исходная база была снимком Pi fork:

- Repository: `git@github.com:GitHubFoxy/pi.git`.
- Branch: `secretary/full-remote-worker`.
- Source commit: `032d98982daebb51fba0f8d63400259ed78ffebd`.
- Дата исходного commit: 2026-09-06.

Каталог хранился как обычный source snapshot, не как Git subtree. Позже в него добавлялись Secretary Client и Pi viewer/extension для следующих фаз. Поэтому исходный Pi commit описывает базу, но не все изменения на момент удаления.

Точная версия удаляемых tracked files в Secretary repository:

- Branch: `phase4-implementation`.
- Commit: `5bad653c33c20dc18b889bdef19e26df11984624`.
- Git tree `HEAD:phase-1`: `9e07a533d1743ebf31e37881d642e32ce05c5c14`.
- 1 680 tracked files.
- Размер каталога перед удалением: 356 МБ по `du -sh`, включая локальные зависимости и сборки.
- `git status --short --untracked-files=all -- phase-1` был пустым. Несохранённых tracked изменений и untracked files вне ignore rules не было.

Историю отдельных файлов можно читать через `git show <commit>:phase-1/<path>`. Для извлечения tracked snapshot в отдельный каталог, без восстановления его в рабочем проекте:

```sh
mkdir -p /tmp/secretary-phase1-recovery
git archive 5bad653c33c20dc18b889bdef19e26df11984624 phase-1 \
  | tar -x -C /tmp/secretary-phase1-recovery
```

Эта команда приведена для будущего восстановления. При удалении она не выполнялась. `node_modules` и прочие ignored артефакты Git archive не восстанавливает.

## Назначение

Phase 1 дала Pi-side runtime для persistent Workers, удалённый viewer и поведение TUI, близкое к обычному Pi. Позднейшие изменения использовали этот snapshot как место разработки отдельного Client для Go Secretary server.

Нужно различать два слоя:

1. Экспериментальный Pi client/server runtime запускал Pi sessions и обеспечивал remote presentation.
2. Secretary Client обращался к самостоятельному Go server через `/v1/` HTTP и WebSocket API. Он не запускал Worker и не подключался к Node protocol.

Основной Secretary server находится в корне проекта. Удаляемый TypeScript monorepo не является его заменой.

## Состав monorepo

В корневом `package.json` были npm workspaces, ESM и требование Node.js `>=22.19.0`. Root package имел версию `0.0.3`; отдельные пакеты на момент удаления имели версию `0.85.0`.

| Каталог внутри phase-1 | Назначение |
| --- | --- |
| `packages/ai` | Общий LLM API, provider configuration и model discovery. |
| `packages/agent` | Agent runtime, состояние, tool calling и abstractions Session/Harness. |
| `packages/coding-agent` | Pi CLI, файловые и shell tools, интерактивный режим, extensions и экспериментальные remote clients. |
| `packages/tui` | Терминальный интерфейс с differential rendering. |
| `packages/chord` | Композиция services/facets, RPC, replicated state и plugins. |
| `packages/protocol` | Runtime-neutral routed envelopes, CBOR и framing. |
| `packages/client` | Transport-neutral client экспериментального Pi protocol. |
| `packages/server` | Экспериментальный сервер Pi sessions и presentation attachments. |
| `packages/session-backends/sqlite-node` | Durable Session backend на `node:sqlite`. |
| `packages/telemetry` | Независимые от vendor telemetry contracts и typed schemas. |
| `packages/evals` | Private workspace для evaluations. |

Также удаляются исходные README/AGENTS, changelogs, примеры extensions/plugins, package locks, TypeScript/Vitest configs, npm scripts, CI workflows, документация Pi и локальные build artifacts. Это был полный monorepo, а не только Secretary extension.

## Экспериментальный remote Worker runtime

Основные компоненты находились в `packages/coding-agent/src/experimental/`:

- `coordinator.ts`, `coordinator-entry.ts`: стабильная точка локальной маршрутизации и смена server generation.
- `server.ts`, `session-worker-manager.ts`, `session-worker.ts`: server/session lifecycle и worker ownership.
- `client.ts`, `client-runtime.ts`: подключение удалённой presentation.
- `client-tui.ts`, `client-tui-chat.ts`: remote chat TUI.
- `full-worker-resources.ts`: bridge worker-side extensions и UI.
- `services/`: application-owned contracts и providers для sessions, transcripts, models, controllers, plugins и UI.

Внутренний Pi protocol и Secretary `/v1/` API были разными транспортами и не должны смешиваться.

### Pi protocol и ownership

Экспериментальный protocol version был `8`. Сообщения кодировались в CBOR и имели четырёхбайтовый big-endian length prefix.

Маршруты:

- Server target: `{ serverId }`.
- Session target: `{ serverId, sessionId, attachmentId }`.

`attachmentId` защищал от запоздавших запросов после переключения или повторного подключения presentation. Несколько presentations могли наблюдать одну Session, не создавая второго владельца её исполнения.

`pi-protocol` валидировал envelope, framing и strict JSON. Chord владел service payloads, catalogues, subscriptions, snapshots и delta encoding. Session и Harness оставались внутри worker process и не передавались через RPC как объекты.

Низкоуровневый `pi-client` после disconnect не повторял принятые запросы автоматически. Reconnect и повтор только заведомо безопасных операций были обязанностью приложения. Отсоединение viewer не означало автоматической отмены уже принятой работы.

Experimental Unix transport сам по себе не реализовывал peer authentication. Этот runtime нельзя считать готовой security boundary для недоверенных удалённых клиентов.

### Services и TUI

Services включали `SessionDirectory`, `SessionManagement`, `PresentationPlugins`, `SessionPlugins`, `Models`, `AgentController`, `Transcript`, `SlashCommands` и `PresentationUI`.

`AgentController` предоставлял presentation-safe управление prompt, queue, abort, resume и compaction. `Transcript` передавал replicated state; Chord отвечал за hydration, sequencing и gap detection.

Plugins могли иметь отдельные session и TUI facets. `/reload` пересобирал выбранные ресурсы, переключал generations и освобождал старые. Remote presentation использовала общий Pi renderer, editor, message/tool components, themes, footer и widgets вместо управления execution напрямую.

Bridge поддерживал extension commands/tools, lifecycle, autocomplete, dialogs, status/title/footer/widgets, custom editor и `ui.custom()`.

### Durable Session storage

SQLite backend обычно создавал отдельную базу на Session; shared container тоже поддерживался. Durable Session ID был отделён от безопасного имени файла.

Backend обеспечивал локальную сериализацию ownership и read-only fork snapshots. Он не предоставлял cross-process lease, heartbeat или takeover. Единственного writable owner гарантировал host lifecycle.

## Secretary Client, добавленный в следующих фазах

Основные файлы:

```text
packages/coding-agent/
├── src/secretary/
│   ├── client.ts
│   ├── presentation.ts
│   ├── runtime.ts
│   └── index.ts
├── src/extensions/secretary.ts
├── src/experimental/secretary-client.ts
├── src/experimental/secretary-tui.ts
├── src/cli/experimental/commands/secretary.ts
├── docs/secretary-client.md
└── test/
    ├── secretary-client.test.ts
    ├── secretary-client-runtime.test.ts
    └── secretary-extension.test.ts
```

### HTTP/WebSocket adapter

`SecretaryClient` использовал публичный `/v1/` API и injected HTTP/WebSocket transports для tests.

В коде были методы для:

- Pairing: bootstrap → pending handoff → owner approval → одноразовый redeem отдельного Client credential.
- Чтения bounded Conversation tail и постраничного replay.
- Отправки Conversation message с external message ID и Idempotency-Key.
- Worker list/details/turns/activity, выбора и открытия observer.
- Secretary turn stream и Conversation/Worker WebSocket subscriptions.
- Worker message/respond/cancel/close и Approval approve/deny.
- Чтения Projects/Nodes и чтения/изменения user document.

Наличие метода не означает разрешение для Pi extension. Сервер проверял grants; более широкий API adapter не должен был расширять narrow Client scopes.

HTTP использовал Bearer credential. Стандартный WebSocket передавал credential в запрошенном subprotocol вместе с `secretary.v1`, а не в URL query. Это было необходимо из-за отсутствия произвольного Authorization header в стандартном Node WebSocket.

Подписки выполняли HTTP replay перед открытием socket, сортировали и дедуплицировали события, отслеживали sequence gaps и reconnect с exponential backoff. При `401`, `403` или `1008 Client revoked` reconnect прекращался.

### Presentation и lifecycle

`SecretaryPresentation` хранил только локальное представление:

- Conversation entries, Workers и Approval summaries.
- Выбранный Worker и его activity.
- Secretary turn events.
- Connection state и время последнего snapshot.

Состояния подключения: `starting`, `connected`, `reconnecting`, `offline`, `revoked`. Gap приводил к canonical resync: новый snapshot, новые cursors и повторное открытие subscriptions. `SecretaryClientRuntime` объединял client/presentation, запуск, reconnect и disposal.

Worker/domain state оставался на Secretary server. Reconnect Client не должен был создавать Worker или новый execution cycle.

### Стандартный Pi extension

`src/extensions/secretary.ts` регистрировал `/secretary` в обычном Pi, без обязательного experimental CLI.

Пользователь выбирал Secretary или существующий открытый Worker, затем вводил сообщение в compose editor. Текст уходил напрямую по HTTP, без `sendUserMessage` и без передачи локальной модели Pi. Обычный ввод после закрытия editor по-прежнему принадлежал локальному Pi.

Worker selector:

- Скрывал archived/closed Workers.
- Показывал `worker.title`, с fallback на `worker_ref`.
- Различал одинаковые названия порядковым номером.

Live widget показывал Conversation body, connection state, ограниченный Worker status и tool name/start/finish. Tool arguments/output, raw events, credentials и reasoning в нём не отображались. Это описание стандартного extension, а не гарантия для старого experimental TUI, который сериализовал activity через JSON.

Настройка использовала `SECRETARY_BASE_URL` и `SECRETARY_CLIENT_CREDENTIAL_FILE`. По умолчанию credential читался из `~/.config/secretary/viewer-credential`; файл должен был иметь права `0600`.

### Grants и ограничения

Default pairing оставался read-only:

```text
conversation:read
worker:read
approval:read
```

Для отправки через стандартный extension требовался отдельно одобренный точный grant:

```text
conversation:read
conversation:write
worker:read
worker:message
approval:read
```

Нельзя заменять `worker:message` широким `worker:write`. Extension не должен был управлять Node, исполнять Dispatch, отменять/закрывать Workers, решать Approvals или становиться owner management UI.

На момент решения об удалении текущий Pi extension уже был снят с autoload в `~/dotfiles`. Новый terminal UX не был выбран. Worker messaging оставался заблокирован отсутствием narrow `worker:message` на развёрнутом сервере; наличие client-side метода не закрывало эту серверную зависимость.

## Историческое acceptance evidence

Тесты при подготовке этого документа не запускались. Приведённые ниже результаты записаны в существовавших ledger files, а не измерены заново.

### Ранний remote runtime

`SECRETARY-ACCEPTANCE.md` фиксировал:

- `subpi --view --name NAME -q TASK`: один viewer в caller cmux workspace и вывод THREAD_ID.
- Attach к существующей Session без второго task owner.
- Quoting, exit status, workspace selection, cleanup при viewer failure и cancellation forwarding.
- Transcript streaming, model/thinking/compaction controls, extension UI и `/reload`.
- Escape cancellation без перехвата Escape в dialogs/autocomplete.
- Reconnect без повторного исполнения задачи.
- 7 passing launcher tests в `~/dotfiles/pi/agent/bin/subpi-with-view.test.mjs`.
- 131 passing experimental coding-agent tests и 1 intentionally skipped.
- 2 109 passing coding-agent tests и 51 intentionally skipped в указанном ledger прогоне.
- Успешные check/build и TUI tests в том историческом состоянии.

Live evidence относилось к 2026-09-05. Historical copied-transcript attach symptom не был воспроизводимым текущим defect. Provider rate-limit timing, длительный network outage, большая remote compaction и экстремальный burst tool results оставались soak investigations, а не доказанными сценариями.

### Более поздний read-only viewer

`docs/phase5-release-gate.md` фиксирует deterministic gate и manual proof 2026-09-22 на omarchy и MacBook Air через Tailscale: snapshot, live stream, reconnect после server restart, revoke, privacy boundary и reboot/service persistence.

Этот ledger относится к историческому read-only viewer. Он не подтверждает новые messaging grants или выбранный будущий TUI UX. Phase 4 real-harness acceptance тоже нельзя считать завершённым по этим результатам.

## Последствия удаления

По просьбе пользователя удаляется весь `phase-1/`, включая TypeScript source, docs, tests, локальные dependencies и build artifacts. Go code, Web, Telegram и существующие Worker records не изменяются. Удаление этого source snapshot не является командой закрытия Workers или удаления их истории.

Известные зависимости, оставшиеся вне удаляемого каталога:

- `scripts/phase5-release-gate.sh`: шаг 4 требует `phase-1/node_modules` и запускает `npm run check:readonly`. После удаления gate останавливается на этом шаге. Его успешность не подтверждается этим документом.
- `docs/pi-viewer-runbook.md`: команда `pi --extension .../phase-1/packages/coding-agent/src/extensions/secretary.ts` больше не указывает на существующий файл.
- `docs/phase5-release-gate.md`: остаётся историческим ledger, а не действующей инструкцией сборки удалённого viewer.
- `.scratch/phase-4/spec.md` и tickets 14/16: утверждения о сохранении snapshot и ссылки на его реализацию отражают старое состояние и требуют пересмотра при следующей работе над Pi.
- `.scratch/phase-5-pi-viewer/`: сохраняется как история задач и принятых решений.

Эти файлы не исправлялись автоматически: пользователь запросил описание и удаление snapshot, а не переработку gates или реализацию нового Pi Client.

Root Go build не включал `phase-1/` автоматически. Всегда работающий сервер omarchy не обновлялся и не перезапускался в рамках удаления.

## Что сохранить в будущей реализации

- Pi является Client, а не Node, Worker harness или второй source of truth.
- Сообщения пользователя идут непосредственно Secretary/Worker через явно выбранный remote editor, не через локальную модель Pi.
- Разрешения выдаются явно и узко; default pairing остаётся read-only.
- Reconnect восстанавливает presentation из server state, а не повторяет неизвестное execution.
- Progress, terminal Result и закрытие Worker являются разными вещами.
- Credentials, raw ACP, чувствительные tool arguments/output и chain-of-thought не отображаются.
- Новый terminal UX нужно определить отдельно. Удалённый extension не является согласованным окончательным дизайном.
