# 28 Удаление Pi viewer и Secretary extension

Type: task
Status: resolved
Related: 16, 27

## Цель

Полностью удалить поддержку Pi как Secretary viewer/Client extension из актуального кода, документации, scripts и планов проекта. Не разрабатывать замену терминального клиента в рамках этого тикета.

Pi как будущий Worker harness является отдельной задачей [27](27-add-pi-as-harness.md). Этот тикет её не отменяет и не удаляет общие механизмы harness adapters.

## Исходное состояние

- `phase-1/` уже удалён по просьбе пользователя. Его описание и точка восстановления сохранены в `.scratch/.archived/phase-1-summary.md`; исходники доступны в Git history.
- Старый `secretary.ts` снят с Pi autoload. В проекте не осталось его source directory, но остались связанные docs, gate, tests, comments и планы.
- `scripts/phase5-release-gate.sh` всё ещё требует `phase-1/node_modules` и запускает TypeScript check. После удаления snapshot этот шаг не работает.
- Runbook всё ещё предлагает загрузку extension из удалённого `phase-1/`.

## Работа

### 1. Удалить специализированные артефакты

Удалить:

- `docs/pi-viewer.md`.
- `docs/pi-viewer-runbook.md`.
- `docs/pi-viewer-release-note.md`.
- `docs/phase5-release-gate.md`.
- `scripts/phase5-release-gate.sh`.
- Любые оставшиеся Pi viewer/extension-only source, configs, install hooks, CLI examples и test fixtures, найденные при проверке repository.

Не восстанавливать `phase-1/` ради этих действий. Не оставлять broken gate или рекомендации запускать extension из отсутствующего каталога.

### 2. Убрать Pi Client из действующих contracts и runbooks

- В `docs/always-on-runbook.md` убрать Pi extension setup, viewer credential path, Pi messaging-scope migration, MacBook viewer-specific examples и ссылки на удалённые docs.
- Сохранить общие инструкции Client pairing/revoke, explicit grants, Idempotency-Key, service management, backup, health checks, loopback listener и Tailscale Serve. При необходимости изложить pairing как generic Client flow, без Pi branding и специальных viewer commands.
- В `.scratch/.archived/phase-4/spec.md` убрать требования сохранять `phase-1/` и подключать Pi Client extension как продуктовую поверхность.
- В `.scratch/.archived/phase-4/map.md` и других актуальных plans убрать Pi viewer work order и зависимости на него.
- Исторический [ticket 16](16-pi-client-integration.md) пометить как снятую поддержку со ссылкой на этот тикет. Не превращать его старые acceptance результаты в действующие обещания.
- Удалить `.scratch/phase-5-pi-viewer/` как отдельный действующий effort. Перед удалением перенести ещё актуальные общие требования к always-on deployment, Client privacy, DTO и Worker isolation в соответствующие Phase 4 docs/tickets, если они не покрыты там уже.
- Устранить broken links и references во всех актуальных docs, scripts, configs и generated architecture documentation.

История остаётся в Git и `.scratch/.archived/phase-1-summary.md`. Не переписывать исходные GitHub import snapshots и не стирать историческое evidence из этого summary. Упоминания Pi в новом harness ticket 27 и его будущей реализации не относятся к viewer.

### 3. Удалить Pi-specific branches, сохранить общую безопасность

Проверить в том числе:

- `internal/webapi/pi_readonly_surface_test.go`.
- Pi/viewer fixtures и terminology в `internal/webapi/client_api_test.go` и других tests.
- Pi-specific comments/assumptions в `internal/webapi/approvals.go`, public Worker DTO и WebSocket authentication.

Удалить код и tests, обслуживающие только снятый Pi viewer. Если код обеспечивает общий Client contract, оставить его без привязки к Pi.

В частности, не удалять вместе с viewer:

- Client identity, explicit scopes, pairing, revoke и authorization checks.
- Allowlisted DTO, redaction и отсутствие утечек credentials, native session IDs, raw ACP и chain-of-thought.
- Bounded snapshots, ordered replay, cursors, deduplication и reconnect.
- Conversation/Worker APIs и WebSocket transports, используемые Web, Telegram или общими Clients.

Общие assertions из Pi-branded tests перенести или переименовать в generic Client tests. Не ослаблять read-only permissions и не выдавать `worker:write` как замену узких grants. Scope или endpoint нельзя удалять только потому, что Pi был одним из его потребителей; сначала проверить общих consumers.

### 4. Проверить результат

- Поиск по `Pi viewer`, `Pi Client`, `pi-viewer`, `/secretary`, `secretary.ts`, `PI_EXPERIMENTAL`, `SECRETARY_CLIENT_CREDENTIAL_FILE`, `viewer-credential` и `phase-1` не находит действующих Pi viewer/extension путей. Исторический summary, этот removal ticket и явно архивные оригиналы являются допустимыми исключениями.
- Поиск не должен удалять Pi harness terminology из ticket 27 или generic использование слова viewer в Web observer.
- Существующие release gates не вызывают удалённый Phase 5 script и не требуют `phase-1/`.
- Проверить актуальные ссылки в docs и локальных tickets.
- Запустить изменённые focused tests, `go test ./...`, `go test -race ./...`, `go vet ./...`, `go build ./cmd/...` и `git diff --check`.
- Проверить Web tests/build, если изменены Web code или assets.
- В `## Answer` записать удалённые artifacts, перенесённые generic tests и результаты реально выполненных проверок. Не утверждать новый PASS исторического Pi gate.

## Границы

- Только изменения проекта. Не удалять пользовательскую установку Pi, `~/dotfiles`, provider credentials, Client records или данные Secretary на сервере.
- Не закрывать и не удалять Workers, Projects, Conversation или историю.
- Не перезапускать и не обновлять omarchy без отдельного разрешения.
- Не реализовывать новый TUI, Pi harness или дополнительные broad grants.

## Acceptance

- В проекте нет активной поддержки Pi viewer/Secretary extension, специализированных docs и gate.
- Нет runnable команд с путями к удалённым source files или обязательных checks отсутствующего `phase-1/`.
- Phase 4 contracts и work order больше не требуют Pi Client integration.
- Отдельный Phase 5 viewer effort удалён; его актуальные общие требования не потеряны.
- Общие Client API, Web/Telegram, Node boundary и privacy/authorization coverage сохранены.
- Pi harness ticket 27 не изменён и не блокируется очисткой viewer.
- Исторический summary и Git history сохраняют возможность понять и восстановить снятую реализацию.
- Проверки выполнены, evidence записано, без доступа к реальным пользовательским секретам и без server deployment.

## Answer

Снята поддержка Pi как Secretary viewer/Client extension. Удалены `docs/pi-viewer.md`, `docs/pi-viewer-runbook.md`, `docs/pi-viewer-release-note.md`, `docs/phase5-release-gate.md`, `scripts/phase5-release-gate.sh` и весь `.scratch/phase-5-pi-viewer/`. Устаревший Phase 5 gate не запускался и его прежнее PASS не переобъявляется.

Обновлены `docs/always-on-runbook.md` и `docs/current-architecture.html`: оставлены generic Client pairing/revoke, explicit scopes, idempotency, loopback/Tailscale Serve, services, backup, health и privacy guidance; Pi-specific setup, credential path и broken doc links удалены. В `.scratch/.archived/phase-4/spec.md`, map и tickets 07/09/14 сохранены общие Client API/privacy/DTO и ACP workspace isolation требования. Ticket 16 помечен историческим со ссылкой на этот тикет; его прежние acceptance results не являются текущими обязательствами. Map обновлён. Ticket 27 не менялся.

`internal/webapi/pi_readonly_surface_test.go` переименован в `internal/webapi/client_readonly_surface_test.go`; Pi fixtures и названия в Client/daemon/Telegram tests стали generic. Assertions сохранены: Client identity, pairing, explicit exact scopes, отказ за пределами read surface, owner-only pairing/revoke, HTTP и WebSocket authorization, revoke streams кодом 1008 с запретом reconnect, allowlisted Approval/Worker DTO, recursive redaction private fields, bounded snapshots с лимитами, forward/backward cursors и invalid cursor checks. Replay boundary, delayed-notify deduplication, reconnect/cursor, Worker/Conversation/Secretary APIs и credential separation от Node protocol остаются покрыты.

Первый `mise exec -- go test -race ./...` выявил гонку: revoke мог отменить Secretary stream во время store read, обработчик закрывал socket до отправки кода 1008, и тест получал EOF. Добавлен общий `closeRevokedStream`; stream handlers теперь отправляют revoke close code при гонке с read/write, без ослабления assertions. Повторный стресс-тест прошёл 50 раз.

Проверки:

- `mise exec -- gofmt -w internal/webapi/client_readonly_surface_test.go internal/webapi/client_api_test.go internal/webapi/server.go internal/webapi/approvals.go internal/webapi/public_worker.go internal/core/approval.go cmd/secretaryd/main.go cmd/secretaryd/main_test.go cmd/secretaryd/telegram_test.go` и `mise exec -- gofmt -w internal/webapi/server.go internal/webapi/secretary.go internal/webapi/observer.go`: PASS.
- `mise exec -- go test ./internal/webapi ./cmd/secretaryd` для изменённых Client API/security, revoke, Node-auth и Telegram tests: PASS. Дополнительные отобранные `-run` прогоны также PASS.
- `mise exec -- go test -race ./internal/webapi -run '^(TestRevokeClientTerminatesSecretaryTurnStream|TestRevokeClientTerminatesWorkerActivityStream|TestRevokeClientTerminatesAlreadyConnectedConversationStream)$' -count=50`: PASS.
- `mise exec -- go test ./...`: PASS.
- Финальный `mise exec -- go test -race ./...`: PASS.
- `mise exec -- go vet ./...`: PASS.
- `mise exec -- go build ./cmd/...`: PASS.
- Локальная проверка Markdown links в `docs/` и `.scratch/.archived/phase-4/` без оригинальных GitHub snapshots: PASS, 54 файла, broken targets не найдены.
- Поиск активных references к `pi-viewer`, `secretary.ts`, Phase 5 gate и удалённым `phase-1/` путям: PASS, совпадений в активных коде/docs/scripts нет. Исторические ticket 16, removal ticket 28, ticket 27 и сохранённые summaries/snapshots исключены из этого критерия.
- `git diff --check`: PASS.
