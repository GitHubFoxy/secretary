# Pi read-only viewer contract

Контракт первого usable Secretary client: Pi на MacBook Air только читает состояние always-on Laptop server. Документ фиксирует topology, credential model, screens, reconnect и privacy boundary. Тикеты `phase-5-pi-viewer` 01-05 ссылаются на него.

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

`GET /v1/bootstrap` отказ не получает: он открыт тем же `conversation:read`, что и остальная snapshot surface, и возвращает те же Workers. Он не входит в snapshot по решению контракта, но запретить его Pi без отдельного scope server не может.

Вариант A (path allowlist только для Pi read endpoints и pairing routes) отклонён для первого релиза: он требует второго listener и отдельной конфигурации proxy без выигрыша в покрытии, если negative tests проходят.

### Чем Pi не является

- Не Execution node: у Pi нет Node pairing token, Node credential и доступа к `/v1/nodes/connect`. Dispatch и Node protocol недоступны.
- Не Worker harness: Pi не открывает ACP session, не держит Workspace и не выполняет Task.
- Не второй source of truth: у Pi нет локальной durable базы. Presentation state, последний confirmed cursor и файл credential - всё, что он хранит между запусками.
- Не owner management UI: после pairing Pi не получает bootstrap token, `client:manage` и `user:write`.

По модели repo Pi viewer - отдельный Client с read-only scopes, а не Channel adapter с write-правами.

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

Явно не выдаются: `conversation:write`, `worker:write`, `approval:write`, `project:*`, `node:*`, `client:manage`, `user:read`, `user:write`.

Требование контракта: pairing без явного scope list запрещён, server отклоняет такой запрос. Server-side проверку и regression tests выполняет тикет 02.

Сегодня requirement нарушен: пустой `scopes` в `POST /v1/clients/pair` даёт полный default набор из 13 scopes (`normalizeClientScopes`, `internal/core/client.go`), а `SecretaryPairOptions.scopes` в Pi optional и `client.ts` не отправляет поле, если оно не задано. Паринг Pi без явного списка сейчас выдаёт full-control credential.

Revoke выполняет owner: `POST /v1/clients/{id}/revoke`. Server отменяет активные streams этого Client и запрещает новые HTTP и WS запросы. Повторное подключение требует нового pairing.

## Read surface

Список endpoints, которые viewer может вызывать Client credential'ом:

| Endpoint | Scope | Что получает Pi |
| --- | --- | --- |
| `GET /v1/conversation?after_seq=N` | `conversation:read` | entries Personal Conversation после cursor |
| `GET /v1/conversation/ws?after_seq=N` | `conversation:read` | replay после cursor, затем live entries |
| `GET /v1/workers` | `worker:read` | список Workers со status |
| `GET /v1/workers/{worker_ref}` | `worker:read` | status и durable WorkerDetails |
| `GET /v1/workers/{worker_ref}/activity?after_seq=N` | `worker:read` | батч activity events, максимум 500 |
| `GET /v1/workers/{worker_ref}/activity/ws?after_seq=N` | `worker:read` | live activity |
| `GET /v1/secretary/turns/{turn_id}/stream?after_seq=N` | `conversation:read` | события активного Secretary turn |
| `GET /v1/secretary/turns/{turn_id}/stream/ws?after_seq=N` | `conversation:read` | live события Secretary turn |
| `GET /v1/secretary/models` | `conversation:read` | выбранная model и catalog имён |
| `GET /v1/approvals` | `approval:read` | pending Approval summary, см. требуемую работу |

Сюда не входят `GET /v1/bootstrap` и `GET /v1/workers/{worker_ref}/diagnostics`: snapshot viewer собирает из объявленных выше read endpoints.

### Требуемая работа по surface

Часть контракта пока не реализована server'ом. До закрытия соответствующих тикетов эти пункты считаются долгом, а не гарантией:

- **Bounded snapshot.** `GET /v1/conversation?after_seq=0` отдаёт все entries после cursor, `GET /v1/workers` и `GET /v1/approvals` отдают всё без limit. Server-side limit обязателен: выбор контракта `GET /v1/conversation?before_seq=N&limit=N` либо `GET /v1/conversation/tail?limit=N`, плюс limit для Workers и Approvals. Выполняет тикет 03. Пока параметр не реализован, snapshot считается unbounded и client-side обрезка хвоста ограничением не является.
- **Public approval DTO.** `GET /v1/approvals` возвращает `[]core.Approval` целиком и не вызывает sanitizer: уходят `request_id`, `worker_id`, `turn_id`, `attempt_id`, `node_id`, `project_id`, `response`, `resolved_by`, `audit_event_id`. Нужен отдельный allowlisted DTO с полями из раздела Privacy boundary. Выполняет тикет 03.
- **Фильтр approvals.** `Store.Approvals` читает `phase4_approvals` без фильтра по Person или Conversation, поэтому любой Client с `approval:read` видит все Approvals. Фильтр обязателен до закрепления endpoint как Pi public surface. Выполняет тикет 03.

Запросы на запись для Pi получают отказ `403` (не хватает scope), pairing routes без bootstrap или pending token - `401`:

- `POST /v1/messages`;
- `POST /v1/workers/{worker_ref}/{message,respond,steer,queue,stop,cancel,close,approve}`;
- `POST /v1/approvals/{approval_id}/{approve,deny}`;
- `PUT /v1/user`, `POST /v1/projects`, `POST /v1/secretary/model`;
- `POST /v1/clients/*` кроме pair/poll/redeem;
- `POST /v1/telegram/pairing`.

## Screens

Первый релиз описывает четыре экрана, без изменения Web UI и Telegram:

1. **Snapshot** - `pi --experimental secretary --once`. Без открытия socket'ов: только HTTP read запросы, затем завершение. Печатает:
   - Secretary state: состояние активного Turn и выбранную model;
   - хвост Conversation;
   - для каждого Worker: `status`, `last_result_summary`, последнюю безопасную activity summary;
   - pending Approval summary.

   Result и activity попадают в snapshot напрямую, а не только после открытия Worker detail.
2. **Conversation** - интерактивный режим. Personal Conversation в порядке `seq`, события активного Secretary turn, индикатор connection state.
3. **Worker detail** - выбранный Worker по `worker_ref`: status, Turns и Results summary, live activity.
4. **Approval summary** - pending Approvals: `id`, `kind`, `action_summary`, `risk_category`, `state`. Без кнопок approve/deny.

Layout и keybindings оставлены тикету 03. Контракт требует только состав данных и состояния связи.

## Snapshot, live, reconnect, resync

**Initial snapshot.** До открытия подписок viewer читает `GET /v1/conversation`, `GET /v1/workers`, `GET /v1/approvals` и, если нужен выбранный Worker, `GET /v1/workers/{worker_ref}`. Snapshot обязан быть bounded server'ом: limit и cursor задаёт API, а не viewer. См. раздел "Требуемая работа по surface" - до реализации limit'ов snapshot считается unbounded.

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

- Conversation entries, Worker details и события activity проходят sanitizer.
- `GET /v1/approvals` sanitizer не вызывает вообще, поэтому его public DTO - работа тикета 03, а не сегодняшняя гарантия.
- Sanitizer не трогает `node_id`, `project_id`, `harness_instance_id`, `workspace` и `policy_snapshot`: server может их отправить, а viewer обязан их не рендерить. Обе стороны ограничения остаются обязательными.

Список полей, которые viewer рендерит:

| Сущность | Рендерит |
| --- | --- |
| Conversation entry | `id`, `seq`, `kind`, `body`, `worker_ref`, `turn_id`, `result_id`, `created_at` |
| Secretary state | состояние Turn (`queued`, `starting`, `active`, `waiting_approval`, `needs_input`, `succeeded`, `failed`, `canceled`, `interrupted`), `seq` и kind события, имя выбранной model |
| Worker | `worker_ref`, `title`, `intent`, `status`, `last_result_summary`, `created_at`, `updated_at`, `closed_at`, `archived` |
| Worker activity | `seq`, тип события, sanitized text |
| Result | `status`, `summary`, `failure_code`, `artifact_refs`, `created_at` |
| Approval summary | `id`, `kind`, `action_summary`, `risk_category`, `state`, `requested_at`, `expires_at` |

Не показывать нигде: native и runtime session IDs, `harness_instance_id`, `node_id`, пути `workspace`, `policy_snapshot` и `context_snapshot`, `diagnostics`, credentials, tokens и bootstrap fragment, tool arguments и raw ACP frames, chain of thought и reasoning, prompt'ы runtime.

## Не-цели первого релиза

- Отправка сообщений: `/v1/messages` и Secretary turn input.
- Worker commands: `message`, `respond`, `steer`, `queue`, `stop`, `cancel`, `close`, `approve`.
- Approval decisions: `approve` и `deny`.
- Node control и Node protocol, включая `node:read` inventory.
- Model control: `POST /v1/secretary/model`.
- Управление Clients: `client:manage`.
- Telegram и Phone client.
- Изменения Web UI и Control Room.
- Локальная durable база viewer'а и второй source of truth.

## Определяют последующие тикеты

- `01-always-on-server-baseline.md`: documented data directory, backup/restore-check, launchd runbook, loopback-only listener с Tailscale Serve HTTPS proxy и ACL, запрет прямого bind, Serve surface с negative tests против Node/internal/control routes, health/status checks.
- `02-pi-read-only-credential.md`: pairing path, запрет pairing без явного scope list, storage credential на MacBook Air, revoke flow, regression tests на HTTP и WS authorization и revoke streams.
- `03-pi-viewer-snapshot-and-live-state.md`: bounded snapshot с limit/cursor, allowlisted public DTO включая approval summary и фильтр approvals, реализация screens, cursor replay, resync и connection states в `pi --experimental secretary`.
- `04-pi-viewer-real-acceptance.md`: проверка на реальных машинах, redacted evidence ledger.
- `05-pi-viewer-runbook-and-release.md`: install/runbook, automated release check, release note `Pi read-only viewer`.
