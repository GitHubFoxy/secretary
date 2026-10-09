# 15 Phase 4 acceptance gate

Type: task
Status: claimed
Blocked by: 03a, 03b, 04, 04a, 05b, 06a, 06b, 07, 08, 09, 10, 11, 12, 13a, 13b, 14

## Work

Создать deterministic release gate и manual real-harness proof для всего Phase 4 contract.

- Поднять чистый Secretary server, MacBook Node и home server Node.
- Проверить observed HarnessInstances, Projects с разными path mappings и Web/Telegram Personal Conversation.
- Проверить полный Secretary stream, один active Secretary turn, durable input queue и параллельные Workers.
- Проверить изменение `user.md` и использование новой preference следующим Secretary turn.
- Проверить Worker-first lifecycle без Task и child Workers.
- Проверить отсутствие override → OpenCode v2 с `openai/gpt-6-luna` / `xhigh`, explicit `fx`, Claude Code и Codex, а также unknown model → visible error без подмены harness/model.
- Проверить несколько Attempts одного Turn, AttemptOutcomes и ровно один финальный Result.
- Проверить direct Result delivery в Conversation без дополнительного Secretary turn и unseen Result в следующем Secretary context.
- Проверить Approval и `needs_input` через `respond_worker` из другого Client.
- Проверить immutable Worker binding, offline Node и отсутствие smart migration.
- Прервать сеть после завершения harness и подтвердить Node outbox replay.
- Повторить server→Node command и подтвердить отсутствие второго process.
- Выполнить real acceptance default OpenCode v2 и explicit adapters `fx`, Claude Code и Codex. Все четыре проверки обязательны; отсутствие native binary/access/auth не делает harness optional.
- Запустить Go tests, race tests, vet, frontend checks, builds, embedded assets и `git diff --check`.

## Acceptance

- Проходит acceptance scenario из раздела 24 `spec.md` на чистой конфигурации и после restart.
- Claude Code, Codex и `fx` реально выполняют предусмотренные flows; OpenCode не используется как замена Claude Code.
- Node network loss не теряет terminal AttemptOutcome и не создаёт duplicate execution.
- Один Turn публикует один Result, даже если было несколько failed Attempts.
- Terminal Result не вызывает дополнительный Secretary model turn, а следующий Secretary turn получает его как unseen Result.
- Web, Telegram и debug-only Control Room показывают согласованный server state.
- Telegram агрегирует/throttle-ит Secretary stream и readable Worker activity.
- Revoke Client/Node, secret redaction, offline state и no-silent-fallback доказаны.
- Единственный финальный deterministic gate выполняет `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`, frontend tests/builds, embedded assets, CLI/deployment scripts и `git diff --check`.
- Manual matrix и diagnostic evidence записаны в `docs/phase4-release-gate.md`.

## Answer

Добавлены `scripts/phase4-release-gate.sh`, его shell contract test, `scripts/phase4-manual-acceptance-wizard.sh` и `docs/phase4-release-gate.md`. Gate выполняет gofmt, Go test/race/vet/build, frontend lock/test/build, embedded asset verification, профильные CLI/Node tests и `git diff --check`. Wizard создаёт защищённый redacted ledger, не принимает credentials и не объявляет acceptance успешной при неполной matrix. Ручная матрица сценария 24 разделена от автоматических проверок и содержит evidence ledger и BLOCKED/UNAVAILABLE правила.

Проверка `./scripts/phase4-release-gate.sh` прошла. После production MCP fix настоящий `fx` на чистой локальной конфигурации создал Worker через server-owned proxy, выполнил read-only flow и вернул terminal Result. Локальный MacBook Node и отдельный Tailscale home-server Node сообщили inventory и Project mappings; настоящий `fx` Worker на MacBook выполнил read-only flow и вернул terminal Result. На home-server успешно выполнен настоящий Codex flow через upstream `@agentclientprotocol/codex-acp@1.10.0`; явный `home-server/codex` Worker вернул terminal Result без `fx` fallback. Попытка Approval попросила запись внутри workspace, что для upstream `read-only` с `workspace-write` не является permission stimulus, поэтому Approval evidence не получено. Корректный внешний путь вне workspace ещё не проверен. В новом изолированном локальном прогоне настоящий `fx` и явный `local-real/codex` Worker завершили read-only `pwd` в mapped workspace без fallback. В том же прогоне revision 2 `user.md` дошла до следующего Secretary turn, Client B после revoke получил 401, а revoked Node отклонил Dispatch. Queue attempt завершился `FAIL` из-за invalid workspace и не считается успешным parallel flow. После исправления capabilities настоящий Codex в отдельном изолированном прогоне запросил permission для внешнего пути, Client B одобрил его через API, и Worker завершился `succeeded`; `needs_input` не создавался, потому что runtime сообщил о недоступности этого механизма. Для этого Approval прогона `fx` probe был fixture-only и не считается `fx` evidence. Полная real-harness acceptance всё ещё не выполнена: Claude Code недоступен; Telegram подтверждён только на уровне pairing и inbound boundary, без полного General chat/Worker Topic/terminal Result flow; `needs_input` и multi-Attempt остаются недоказанными. Restart recovery остаётся частично подтверждённым, потому что Node после restart не переподключился и outbox остался buffered. Поэтому обязательные строки matrix остаются `BLOCKED`, `FAIL` или `NOT RUN`, а статус Ticket 15 остаётся `ready-for-agent`. Дополнительно internal Telegram credential переведён на scoped principal `telegram-adapter` с `conversation:write` и `worker:write`; deterministic tests подтверждают отказ в чтении Workers, неверный credential и replay dedup. Latest isolated run с Execution Node подтвердил pairing и inbound boundary, но при доступных Node и `fx` настоящий `fx` Secretary не вернул terminal response в отведённое время. Это `FAIL` для проверенного flow, а не `BLOCKED`; General chat, Worker Topic и terminal Result по-прежнему не объявляются принятыми. Replay/process count в этом real harness не запускались. Authorization scopes и replay dedup подтверждены отдельно deterministic test-ами и не выдаются за real evidence.

### Актуализация Ticket 15

Исторический отчёт выше сохраняет результаты и статус на момент того прогона; он не является текущим PASS ledger. Текущий contract следует утверждённым разделам 8 и 24 `spec.md`: OpenCode v2 — обязательный default Secretary/Worker, а `fx`, Claude Code и Codex — обязательные explicit adapters. OpenCode не optional и не заменяет Claude Code.

Локальный deterministic gate и public wizard обновлены: suite запускается один раз и последовательно; manual matrix принимает `PASS`, `FAIL`, `BLOCKED`, `UNAVAILABLE` и `NOT RUN`, но только `PASS` закрывает обязательную строку. Исполненный локальный gate: `p4-ticket15-deterministic-459709c-20261004T210801Z`, команда `./scripts/phase4-release-gate.sh`, exit 0 на uncommitted tree от base `459709c`.

Точные полные команды и результат:

```sh
mise exec go@1.27.1 -- go test -p 1 ./...       # PASS, все пакеты
mise exec go@1.27.1 -- go test -race -p 1 ./... # PASS, все пакеты
mise exec go@1.27.1 -- go vet -p 1 ./...        # PASS
mise exec go@1.27.1 -- go build -p 1 ./...      # PASS
(cd web && npm ci --ignore-scripts --no-audit --no-fund) # PASS
(cd web && npm test)                              # PASS, 10/10
(cd web && ./node_modules/.bin/vite build --outDir "$BUILD_TMP/dist") # PASS
(cd web && node scripts/clean-vite-assets.mjs "$BUILD_TMP/dist")
(cd web/control-room && ../node_modules/.bin/vite build --config vite.config.js --outDir "$BUILD_TMP/dist-control") # PASS
(cd web/control-room && node ../scripts/clean-vite-assets.mjs "$BUILD_TMP/dist-control")
# все 6 generated assets совпали с embedded files
mise exec go@1.27.1 -- go test ./web            # PASS
./scripts/secretary-cli-test.sh                       # PASS
./scripts/node-deployment-test.sh               # PASS
./scripts/node-revoke-test.sh                   # PASS
git diff --check                                # PASS
```

Gate включает `gofmt` и свой shell contract test; отдельного frontend `typecheck` script в JS/Svelte проекте нет, compilation проверяет Vite build. Первый gate attempt остановился до tests: `internal/telegram/long_message_research_test.go` имел только форматировочное расхождение. После подтверждения `gofmt -d` применён gofmt без изменения fixture/expectations; последующий полный gate выполнен один раз и прошёл. Manual matrix не запускалась и не изменена. В новой approved implementation-фазе tickets22/29 owner-approved semantic/grapheme inline и opt-in addressed-reply v1 локально реализованы; public HTTP/Core/MCP tests прошли, но real Telegram acceptance остаётся `NOT RUN` (22 live Topic/General — `BLOCKED` без bot/topic). Для29 crash gap закрыт exact pending-intent → canonical Worker Turn/Result join при записи Result до `MarkWorkerCommandDelivered`; historical `Node is offline` timing failure сохранён и не объявляется гарантированно устранённым. Private unpaid native Ticket33 fixtures на OpenCode v2.0.22 прошли в изолированных `t.TempDir`; owner login, authenticated inventory/Worker run и migration approval остаются `NOT RUN`. Owner login/migration для новых native stores, incomplete real scenarios и все существующие `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` остаются честными blockers; Ticket 15 остаётся `claimed` до полной real acceptance.

### Общий final gate новой approved implementation-фазы

- `run_id`: `p4-ticket15-shared-459709c-20261005T052525Z`; base `459709c`; branch `phase4-implementation`, working tree uncommitted.
- `./scripts/phase4-release-gate.sh` выполнен один раз после private native Ticket33 fixtures; exit 0.
- PASS: `mise exec go@1.27.1 -- go test -p 1 ./...`; `mise exec go@1.27.1 -- go test -race -p 1 ./...`; `mise exec go@1.27.1 -- go vet -p 1 ./...`; `mise exec go@1.27.1 -- go build -p 1 ./...`.
- PASS: frontend `npm test` — 10/10; main и Control Room Vite builds; все 6 embedded assets совпали; `go test ./web`.
- PASS: gate contract, `gofmt`, `scripts/secretary-cli-test.sh`, `scripts/node-deployment-test.sh`, `scripts/node-revoke-test.sh`, `git diff --check`; итоговое время фиксации `2026-10-05T05:25:25Z`.
- Manual matrix остаётся неполной: 22 live Telegram — `BLOCKED`; 29 addressed-reply live Telegram — `NOT RUN`; 33 owner login/authenticated inventory/Worker/migration approval — `NOT RUN`; прочие historical `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` сохраняются. Ticket 15 не resolved.

### Combined gate после Ticket 29 late-ACK fix

- `run_id`: `p4-ticket15-lateack-459709c-20261005T070605Z`; base `459709c`; branch `phase4-implementation`; изменения не закоммичены.
- Единственный запуск `./scripts/phase4-release-gate.sh` завершился `PASS`, exit 0; записано `2026-10-05T07:06:05Z`.
- `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...` — PASS, включая Core/CTL/Node/server integration late receipt regressions.
- Frontend `npm test` — 10/10; обе Vite production builds и сравнение всех 6 embedded assets — PASS. `go test ./web`, release-gate contract, `gofmt`, `secretary-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh` и `git diff --check` — PASS.
- Native/live E2E opt-in flags были unset. Isolated store/privacy и CLI integration прошли; authenticated native login/acceptance не запускались.
- Ticket 29 regressions для durable authenticated exact Node/command/Turn/Attempt receipts, late ACK после удаления waiter/reopen/replay, authoritative denial и запрета оживления terminal Turn входят в пройденный Go suite. Это не real Telegram proof.
- Real matrix остаётся pending: 22 live Topic/General — `BLOCKED`; 29 General → Worker Topic → следующий turn — `NOT RUN`; owner login/authenticated native inventory/Worker и migration approval — `NOT RUN`. Исторический `Node is offline` timing failure не объявляется гарантированно исправленным. Ticket 15 остаётся `claimed`.

### Combined gate после Approval fixes Ticket 29

- `run_id`: `p4-ticket15-approvalfix-459709c-20261005T081907Z`; base `459709c`; branch `phase4-implementation`; changes uncommitted. `./scripts/phase4-release-gate.sh` запускался один раз после этих fixes и завершился `PASS`, exit 0; результат записан `2026-10-05T08:19:07Z`.
- PASS: `mise exec go@1.27.1 -- go test -p 1 ./...`; `mise exec go@1.27.1 -- go test -race -p 1 ./...`; `mise exec go@1.27.1 -- go vet -p 1 ./...`; `mise exec go@1.27.1 -- go build -p 1 ./...`.
- PASS: frontend `npm test` — 10/10; обе Vite production builds и точное сравнение всех 6 embedded assets; `go test ./web`.
- PASS: gate contract, `gofmt`, `secretary-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`; тесты использовали isolated HOME/temp outputs.
- Approval intent/resolving, exact Node/command/Turn/Attempt receipt handling, response privacy, conflict behavior и отсутствие terminal-turn revival покрыты прошедшими Core/CTL/Node/WebAPI и frontend suites. Все native/live E2E opt-in flags были unset; provider login/paid calls/production mutation не выполнялись.
- Manual gates остаются неизменными: real Ticket 29 flow — `NOT RUN`; live Ticket 22 Topic/General — `BLOCKED`; owner login, authenticated native inventory/Worker и migration approval — `NOT RUN`; остальные recorded `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` не повышались до PASS. Ticket 15 остаётся `claimed`.

### Combined gate после owner retry fix Ticket 29

- `run_id`: `p4-ticket15-approval-retry-459709c-20261005T091712Z`; base `459709c`; branch `phase4-implementation`; working tree uncommitted.
- `./scripts/phase4-release-gate.sh` выполнен после финального recovery regression, exit 0; результат зафиксирован `2026-10-05T09:17:12Z`.
- PASS: `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`; все пакеты прошли, включая Core recovery, CTL saved-intent retry, WebAPI restart/connection-loss/authenticated-ACK regression и Node protocol tests.
- PASS: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, main и Control Room Vite production builds, все 6 embedded assets, `go test ./web`.
- PASS: release-gate contract, `gofmt`, `secretary-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`.
- Local recovery evidence: явный owner retry через `POST /v1/approvals/{id}/retry` повторно использует сохранённые decision/response и прежние command/Turn/Attempt IDs после refresh/reconnect; auto-retry, новая команда и client payload replacement не допускаются. Public WebAPI/CTL regressions указаны в Ticket 29. Это не real Node/Telegram acceptance.
- Synthetic protocol/API evidence не заменяет real acceptance. Row 17 (`needs_input`/`respond_worker`) остаётся `BLOCKED`: `p4-real-input7-20260914` не создал `user_input_request`; Ticket 29 General → Worker Topic → следующий turn — `NOT RUN`; Ticket 22 live Topic/General — `BLOCKED`. Другие `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` не менялись. Production, provider login и auth mutation не выполнялись; Ticket 15 остаётся `claimed`.

### Combined gate после Ticket 29 Approval retry privacy fix

- `run_id`: `p4-ticket15-approval-retry-privacy-459709c-20261005T095131Z`; base `459709c`; branch `phase4-implementation`; working tree uncommitted.
- `./scripts/phase4-release-gate.sh` выполнен один раз после последних API/UI patches, exit 0; `observed_at_utc`: `2026-10-05T09:51:31Z`.
- PASS: `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`; narrow approval-write-only HTTP privacy test входит в Go suite.
- PASS: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, обе Vite production builds, все шесть embedded assets и `go test ./web`.
- PASS: gate contract, `gofmt`, `secretary-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`.
- Regression проверяет, что credential только с `approval:write` получает exact strict allowlisted response для resolving retry, cached duplicate, resolved no-handoff и соседнего deny; `/v1/workers` и `/v1/approvals` остаются `403`. Marker/shape assertions выводят только булевы итоги. Это deterministic test, не live acceptance.
- Live Ticket 29 General → Worker Topic → следующий turn — `NOT RUN`; Ticket 22 Topic/General — `BLOCKED`; прочие manual statuses не менялись. Ticket 15 остаётся `claimed`.

### Combined gate после Ticket 29 auth rejection и observer refresh fixes

- `run_id`: `p4-ticket15-approval-auth-refresh-459709c-20261005T102708Z`; base `459709c`; branch `phase4-implementation`; рабочее дерево uncommitted.
- `./scripts/phase4-release-gate.sh` запущен один раз после последних API/UI-test patches; exit 0; `observed_at_utc`: `2026-10-05T10:27:08Z`.
- PASS: `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`.
- PASS: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, обе Vite production builds, все 6 embedded assets, `go test ./web`.
- PASS: release-gate contract, `gofmt`, `secretary-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`.
- Public HTTP race test делает реальный Client revoke между route auth checks без sleep; прежний handler воспроизводил продолжение после 401, mutation и второй body. Новый handler завершается после explicit auth rejection. Invalid scoped Client остаётся 403 без mutation/owner DTO. Public owner refresh показывает оба сохранённых решения, но не saved response, user/actor или private command ID; Client permissions/idempotency и public retry ID не менялись.
- Pending: Ticket 29 live General → Worker Topic → следующий user turn — `NOT RUN`; Ticket 22 Topic/General — `BLOCKED`; остальные recorded `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` не повышались до PASS. Ticket 15 остаётся `claimed`. Production/auth login/deploy не выполнялись.

### Previous gate после ACP output-drain test fix — FAIL

- `run_id`: `p4-ticket15-domain-drain-459709c-20261005T105601Z`; base `459709c`; branch `phase4-implementation`; рабочее дерево uncommitted; запись `2026-10-05T10:56:01Z`.
- На тот момент единственный запуск `./scripts/phase4-release-gate.sh`: gate contract и `gofmt` PASS; Stage 3 `go test -p 1 ./...` FAIL в `internal/node`: `TestTwoSecretaryNodeProcessesPairInventoryAndReconnect` timed out after 30.03s at `process_integration_test.go:89`, waiting for first Node authenticated reconnect. На момент этой записи full gate ещё не повторяли; последующий результат записан ниже.
- Оставшиеся stages (полный race, vet/build, frontend/assets, CLI/deployment/revoke и gate diff-check) после failure не исполнялись этим gate run — PASS им не присваивать.
- Один isolated diagnostic `go test ./internal/node -run '^TestTwoSecretaryNodeProcessesPairInventoryAndReconnect$' -count=1 -v` — PASS, 21.38s. Он не отменяет полный gate failure и не доказывает, что reconnect timeout устранён.
- Новый ACP test `go test ./internal/node -run '^TestACPRuntimeDrainKeepsProgressAvailableAndTurnScoped$' -count=20` — PASS, 0.400s. `go test -race -p 1 ./internal/node ./internal/acp ./internal/core ./internal/ctl ./internal/webapi -count=2` — PASS (59.647s/1.361s/41.682s/13.741s/36.245s). Эти targeted repeats не заменяют final gate.
- Manual matrix не менялась: Ticket 29 real Telegram — `NOT RUN`; Ticket 22 live Topic/General — `BLOCKED`; прочие `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` сохранены. Ticket 15 — `claimed`; production, paid calls и auth state не менялись.
- После evidence updates отдельно выполнен `git diff --check` — PASS; это не означает PASS для оставшихся этапов full gate.

### Latest gate после изоляции OpenCode probe в process acceptance — PASS

- Root cause: CLI default включает OpenCode inventory probe, а `Daemon.runConnection` синхронно вызывает `Inventory.Discover` до `DialProtocol`. Два process fixtures запускались с fake `fx`/Claude/Codex, но наследовали host `opencode`; каждая нативная auth probe задерживала handshake. Private temporary HOME/XDG diagnostic без credentials: `opencode --version` завершился за 0.601s, `opencode auth list` не завершился за 45s и был остановлен внешним diagnostic timeout; production probe step ограничен 10s. Лог `connecting outbound` печатается до `daemon.Run`, поэтому не подтверждал состоявшийся reconnect.
- RED: `go test ./internal/node -run '^TestTwoSecretaryNodeProcessesPairInventoryAndReconnect$' -count=1 -v` завершился ожидаемым assertion `transport integration unexpectedly ran the OpenCode inventory probe` при включённом default probe. Добавлен только явный `--include-opencode=false` для transport/pairing acceptance и tripwire, который обнаруживает попытку вызвать OpenCode без эмуляции native acceptance.
- GREEN: `go test ./internal/node -run '^TestTwoSecretaryNodeProcessesPairInventoryAndReconnect$' -count=3 -v` — PASS, три полных повтора. По-прежнему запускаются два настоящих `secretary-node` процесса; тест подтверждает inventory FX и authenticated reconnect по сохранённой identity без pairing token. Таймаут 30s и protocol assertions не менялись; production default и native acceptance не менялись.
- Combined-load: `go test -race -p 1 ./internal/node ./internal/acp ./internal/core ./internal/ctl ./internal/webapi -count=2` — PASS (18.787s/1.363s/41.128s/13.721s/36.451s).
- Промежуточный wrapper-run `p4-ticket15-opencode-probe-scope-459709c-20261005T112859Z` случайно выставил `umask 077`: Stages 1–2 PASS, Stage 3 остановился на mode-sensitive `TestOpenCodeNativeStorePreservesLegacySessionsUntilApprovedMigration` (`0755` fixture directory создалась как `0700`), stages 4–9 не исполнялись. Причина — окружение wrapper-а, не candidate code. Это не gate acceptance.
- Единственный корректный финальный gate-run: `p4-ticket15-opencode-probe-scope-459709c-20261005T113150Z`, base `459709c`, branch `phase4-implementation`, uncommitted tree, `umask=0022`, `./scripts/phase4-release-gate.sh`, exit 0. Stage 1 contract PASS; Stage 2 gofmt PASS; Stage 3 `go test -p 1 ./...` PASS (internal/node 5.449s); Stage 4 `go test -race -p 1 ./...` PASS (internal/node 9.658s); Stage 5 vet/build PASS; Stage 6 `npm ci`/`npm test` PASS; Stage 7 обе Vite builds, 6 embedded assets и `go test ./web` PASS; Stage 8 CLI/deployment/revoke scripts PASS; Stage 9 `git diff --check` PASS.
- Ticket 15 остаётся `claimed`: gate доказывает только автоматические checks. Ticket 29 real flow — `NOT RUN`; Ticket 22 live Topic/General — `BLOCKED`; owner login, authenticated native inventory/Worker run и migration approval — `NOT RUN`; прочие `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` не изменены. Native diagnostic timeout не является acceptance PASS; production/auth/paid mutations не было.