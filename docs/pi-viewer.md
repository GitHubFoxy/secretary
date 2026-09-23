# Pi Secretary client contract

Исходный Pi viewer был read-only. Стандартное TypeScript-расширение дополнительно поддерживает отправку Conversation и Worker message, но не control actions. Этот документ описывает topology, scopes, credential lifecycle, live state и privacy boundary.

Контракт не вводит нового domain state и нового транспорта. Viewer работает поверх существующих `/v1` HTTP read endpoints и WebSocket подписок. Требуемые limit/cursor параметры и public DTO не меняют ни domain state, ни transport.

## Topology

```text
Always-on Laptop: Secretary server, SQLite, Node daemon, Codex/fx, Tailscale
MacBook Air: Pi viewer, Tailscale
Phone/Telegram: out of scope
```

Base URL viewer'а - HTTPS-адрес Secretary server в сети Tailscale. Inbound port на MacBook Air не открывается, соединение всегда идёт от Pi к server.

Supported implementation:

```text
Secretary: loopback-only listener 127.0.0.1:8081
Tailscale Serve: HTTPS proxy на этот loopback адрес
Tailscale ACL: доступ к serve name только с owner devices, включая MacBook Air
Прямой bind на LAN или public адрес запрещён
```

Сейчас `secretaryd` слушает `127.0.0.1:8081` по умолчанию (`-listen`), `sex` хардкодит тот же адрес. Запуск с `-listen 0.0.0.0:8081` нарушает security model контракта и запрещён, даже когда Tailscale Serve уже настроен.

Control Room и `/v1/control/*` отдаются только при `-debug`, поэтому в Serve config их быть не может: `GET /control-room` и `/v1/control/*` возвращают `404` без debug.

Serve surface - полный `/v1` и статика User UI поверх одного loopback server, без отдельного viewer listener. Это осознанный выбор варианта B: routes закрыты credential'ами, поэтому вместо path allowlist контракт требует negative tests. Pi credential обязан получать отказ на `/v1/nodes/connect`, `/v1/internal/secretary/tools/call`, `/v1/telegram/pairing`, control routes и любых `client:manage`/write routes.

Через тот же proxy публикуется `GET /v1/health`: liveness-проба для runbook, не требует credential, возвращает только поле `status` и не входит в snapshot surface.

`GET /v1/bootstrap` остаётся доступен любому credential с `conversation:read`, включая viewer credential: контракт говорит, что Pi его не вызывает, а не что сервер route запрещает. Полный запрет возможен только через отдельный viewer scope или отдельный viewer DTO, и это решение тикета 03. До тех пор route отдаёт viewer те же Workers, что и `/v1/workers`.

Вариант A (path allowlist только для Pi read endpoints и pairing routes) отклонён для первого релиза: он требует второго listener и отдельной конфигурации proxy без выигрыша в покрытии, если negative tests проходят.

### Чем Pi не является

- Не Execution node: у Pi нет Node pairing token, Node credential и доступа к `/v1/nodes/connect`. Dispatch и Node protocol недоступны.
- Не Worker harness: Pi не открывает ACP session, не держит Workspace и не выполняет Task.
- Не второй source of truth: у Pi нет локальной durable базы. Presentation state, последний confirmed cursor и файл credential - всё, что он хранит между запусками.
- Не owner management UI: после pairing Pi не получает bootstrap token, `client:manage` и `user:write`.

По модели repo Pi остаётся отдельным Client, не Channel adapter. Обычный pairing default остаётся read-only; write scopes выдаются только после явного owner-approved pairing с точным scope list.

## Credential model

Три значения участвуют в pairing и никогда не смешиваются:

1. **Bootstrap token** живёт на always-on Laptop и передаётся в Pi вручную только для одной команды `POST /v1/clients/pair`. Он принимается только на этом route, не является Client credential и не сохраняется клиентом.
2. **Pending handoff token** возвращает server на pair. Pi держит его в памяти, пока owner не подтвердит pairing через `GET /v1/clients/{id}/poll` и одноразовый `POST /v1/clients/{id}/redeem`.
3. **Client credential** (`cli_...`) выдаётся после approve или redeem. Только он хранится на MacBook Air: отдельный файл с правами `0600`. Server хранит лишь hash credential.

Scopes Pi на первый релиз:

| Scope | Зачем |
| --- | --- |
| `conversation:read` | Personal Conversation, live stream, Secretary turn stream |
| `worker:read` | список Workers, статус, детали, Worker activity |
| `approval:read` | summary pending Approvals для отображения |

Для обычного read-only viewer остаются только `conversation:read`, `worker:read`, `approval:read`. Отправляющий Pi extension требует owner-approved credential с точными scopes `conversation:read`, `conversation:write`, `worker:read`, `worker:message`, `approval:read`. `worker:message` разрешает только POST `/v1/workers/{worker_ref}/message`; никогда не выдавать extension `worker:write`. Не выдаются `approval:write`, `project:*`, `node:*`, `client:manage`, `user:read`, `user:write`.

`POST /v1/clients/pair` требует явный scope list. Без scopes `defaultPairScopes` остаётся read-only. Для extension scopes должны быть заданы явно при pairing; revoke старого credential и повторное pairing обязательны при смене grants.

Revoke выполняет owner: `POST /v1/clients/{id}/revoke`. Server отменяет активные streams этого Client и запрещает новые HTTP и WS запросы. Повторное подключение требует нового pairing.

## Read surface

Список endpoints, которые viewer может вызывать Client credential'ом:

| Endpoint | Scope | Что получает Pi |
| --- | --- | --- |
| `GET /v1/conversation?limit=N` | `conversation:read` | последние N entries, ascending by seq |
| `GET /v1/conversation?before_seq=N&limit=N` | `conversation:read` | entries with `seq < N`, ascending by seq |
| `GET /v1/conversation?after_seq=N&limit=N` | `conversation:read` | entries with `seq > N`, ascending by seq, постраничное дочитывание до live cursor |
| `GET /v1/conversation/ws?after_seq=N` | `conversation:read` | replay после cursor, затем live entries |
| `GET /v1/workers?limit=N` | `worker:read` | список Workers со status, bounded |
| `GET /v1/workers/{worker_ref}` | `worker:read` | status и durable WorkerDetails |
| `GET /v1/workers/{worker_ref}/activity?after_seq=N` | `worker:read` | батч activity events, максимум 500 |
| `GET /v1/workers/{worker_ref}/activity/ws?after_seq=N` | `worker:read` | live activity |
| `GET /v1/secretary/turns/{turn_id}/stream?after_seq=N` | `conversation:read` | события активного Secretary turn |
| `GET /v1/secretary/turns/{turn_id}/stream/ws?after_seq=N` | `conversation:read` | live события Secretary turn |
| `GET /v1/secretary/models` | `conversation:read` | выбранная model и catalog имён |
| `GET /v1/approvals?limit=N` | `approval:read` | pending Approval summary в allowlisted DTO |

Сюда не входят `GET /v1/bootstrap` и `GET /v1/workers/{worker_ref}/diagnostics`: snapshot viewer собирает из объявленных выше read endpoints.

### Требуемая работа по surface (выполнено тикетом 03)

- **Bounded snapshot.** Snapshot читается хвостом `GET /v1/conversation?limit=N` и страницами `before_seq`/`after_seq`. Ответ всегда envelope `{entries, next_before_seq, next_after_seq}`: entries ascending by seq, неиспользуемый cursor `null`, пустая страница несёт оба `null`. Store выбирает `DESC LIMIT` и переворачивает сам, порядок не перекладывается на viewer. Лимиты едины для conversation, workers и approvals: default 100, maximum 500, `limit > max` clamp до 500, `limit <= 0` и malformed дают `400`. Forward replay (`after_seq`) дочитывается страницами до live cursor, поэтому полнота replay сохраняется при ограниченном response.
- **Public approval DTO.** `GET /v1/approvals` возвращает allowlisted `publicApprovalDTO` (`id`, `kind`, `action_summary`, `risk_category`, `state`, `requested_at`, `expires_at`); `node_id`, `request_id`, `worker_id`, `turn_id`, `attempt_id`, `project_id`, `response`, `resolved_by`, `audit_event_id` за границу не уходят.
- **Фильтр approvals.** Endpoint читает только Approvals, чей worker принадлежит текущей Conversation (JOIN через `workers.conversation_id`).

Разрешённые write routes ограничены двумя операциями: `conversation:write` только `POST /v1/messages`; `worker:message` только `POST /v1/workers/{worker_ref}/message`. Остальные Worker mutations получают `403`, включая `respond`, `steer`, `queue`, `stop`, `cancel`, `close` и `approve`. Также запрещены:
- `POST /v1/approvals/{approval_id}/{approve,deny}`;
- `PUT /v1/user`, `POST /v1/projects`, `POST /v1/secretary/model`;
- `POST /v1/clients/*` кроме pair/poll/redeem;
- `POST /v1/telegram/pairing`.

## Screens

Стандартное расширение вызывается `/secretary`, открывает recipient selector и compose editor, затем шлёт текст серверу напрямую, не передавая его модели Pi. Live view ограничен Conversation body, Worker status и tool name/start/finish:

Расширение показывает compact live widget с Conversation body, connection state, allowlisted Worker status и tool name с отметкой start/finish. Compose editor отправляет тело напрямую серверу. Это не built-in CLI command и не команда экспериментального режима.

## Snapshot, live, reconnect, resync

**Initial snapshot.** Extension читает bounded Conversation tail, Workers и approvals, затем открывает выбранный Worker observer при необходимости.

**Live updates.** После snapshot открывается `GET /v1/conversation/ws?after_seq=<последний confirmed seq>`. Server сначала отдаёт durable entries после cursor, затем live. Доставка строго по возрастанию `seq`: server держит pending map и не отправляет entry с `seq <= cursor`. Те же правила у Secretary turn stream и Worker activity.

**Дедупликация.** Viewer дедуплицирует по `id` (Conversation, Secretary events) и по `seq` (activity), затем сортирует по `seq`. Это уже делает `SecretaryPresentation`, поэтому повторная доставка не создаёт duplicate presentation entries.

**Reconnect.** Любое падение WebSocket - сеть, restart server, restart Pi - приводит к повторному dial с последним confirmed cursor. Conversation replay не ограничен временном окном: сервер читает из SQLite через `EntriesAfter`, поэтому gap возможен только при недоступности базы. Медленный subscriber получает close `1008` с текстом `reconnect with after_seq` и переподключается тем же способом.

**Resync.** Secretary turn stream и Worker activity читаются из таблицы events батчами по 500 событий, а таблицу чистит retention (`PruneEvents`). Если cursor старше retention, либо viewer видит разрыв `seq`, выполняется canonical snapshot resync: заново читается Conversation, Workers и Approvals, выставляются новые cursors и подписки открываются заново. Server не хранит replay state за пределами durable tables, поэтому resync - единственный способ восстановить полную картину.

**Revocation.** Revoke отменяет активные streams (`1008 Client revoked`) и запрещает новые запросы (`401`). Viewer переходит в состояние `revoked` и прекращает reconnect до нового pairing.

**Connection states.** Viewer обязан показывать все четыре:

| State | Условие |
| --- | --- |
| `connected` | snapshot загружен, conversation subscription открыта |
| `reconnecting` | подписка потеряна, идёт повтор с cursor; snapshot показывается как stale |
| `offline` | server или Tailscale недоступны после retries; показывается последний snapshot с временем чтения |
| `revoked` | credential отклонён (`401`, `403` или `1008 Client revoked`); reconnect остановлен |

## Privacy boundary

Каждый Pi endpoint возвращает отдельный allowlisted public DTO. Generic sanitizer (`sanitizePublicJSON`, `sanitizePublicConversationEntry`, `sanitizePublicEvent`) остаётся defence-in-depth, а не API contract: он вырезает ключи `secret`, `credential`, `token`, `policy`, `context`, `diagnostics`, идентификаторы Task и session, маркеры reasoning и паттерны `bearer`/`api_key`/`sk-`, заменяя их на `[redacted]`, а `RuntimeSessionID` очищает вовсе.

Что это значит на практике:

- Conversation entries, Worker details и activity проходят sanitizer; extension рендерит только Conversation body, allowlisted Worker status и tool name/start/finish.
- `GET /v1/approvals` отдаёт allowlisted DTO и sanitizer не требует: private fields не сериализуются вовсе.
- Sanitizer не трогает `node_id`, `project_id`, `harness_instance_id`, `workspace` и `policy_snapshot`: server может их отправить, а viewer обязан их не рендерить. Обе стороны ограничения остаются обязательными.

Список полей, которые viewer рендерит:

| Сущность | Рендерит |
| --- | --- |
| Conversation entry | `body` |
| Worker status | ограниченное enum-значение |
| Worker activity | allowlisted tool name и start/finish marker |

Не показывать нигде: native и runtime session IDs, `harness_instance_id`, `node_id`, пути `workspace`, `policy_snapshot` и `context_snapshot`, `diagnostics`, credentials, tokens и bootstrap fragment, tool arguments и raw ACP frames, chain of thought и reasoning, prompt'ы runtime.

## Ограничения Pi extension

- Worker commands `respond`, `steer`, `queue`, `stop`, `cancel`, `close`, `approve` недоступны. Отправка обычного сообщения Worker разрешена через narrow `worker:message`.
- Approval decisions: `approve` и `deny`.
- Node control и Node protocol, включая `node:read` inventory.
- Model control: `POST /v1/secretary/model`.
- Управление Clients: `client:manage`.
- Telegram и Phone client.
- Изменения Web UI и Control Room.
- Локальная durable база viewer'а и второй source of truth.

## Определяют последующие тикеты

- `01-always-on-server-baseline.md`: documented data directory, backup/restore-check, launchd runbook, loopback-only listener с Tailscale Serve HTTPS proxy и ACL, запрет прямого bind, Serve surface с negative tests против Node/internal/control routes, health/status checks.
- `02-pi-read-only-credential.md`: историческая read-only credential схема; текущая messaging миграция описана выше и в `docs/always-on-runbook.md`.
- `03-pi-viewer-snapshot-and-live-state.md`: bounded snapshot с limit/cursor, allowlisted public DTO, replay и connection states.
- `04-pi-viewer-real-acceptance.md`: проверка на реальных машинах, redacted evidence ledger.
- `05-pi-viewer-runbook-and-release.md`: install/runbook и automated release check.
