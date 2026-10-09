# Phase 5: исправления review

База первого review: `43b9a8a...acdba0c`; Claude increment: `acdba0c...c76434a`. Реализация в `p5/review-fixes` от `acdba0c`, с merge integration `c76434a`.

## Подтверждённые ошибки и исправления

- Close/handoff: public WorkerService + удержанный runtime handoff воспроизвёл canceled queue перед фактическим acceptance и `runtime_session_unavailable` при Cancel. Shared per-Worker Store gate сериализует queue handoff и Close, учитывает context и timeout 30 секунд. Prepared queue cancellation атомарно отменяет command intent; Claim не принимает cancelled intent. `TestCloseOrdersQueuedHandoffBeforeCancellation`, `TestCloseCancelsPreparedQueueBeforeAnyRuntimeHandoff` — red → green, включая deadline ожидающего Close.
- Prepared queue restart: production recovery переводил ещё не переданную starting Attempt в interrupted и blocked. Теперь только queue-owned unclaimed pending intent без lease/error сохраняется для pump; claimed/unknown execution остаётся interrupted/blocked, без replay. `TestPreparedQueueSurvivesProductionRecovery` — red → green; `TestClaimedQueueIsNotReplayedAfterProductionRecovery` — green.
- Live owner cookie regression: при remote manager `GET /v1/nodes` отвечал 401, ломая загрузку Web. Публичный Nodes read и drain/revoke идут через существующую scoped owner/client authorization; owner cookie не даёт Node admin access. `TestOwnerCookieReadsNodesWithRemoteManagerWithoutAdminAccess` — red 401 → green 200, admin/anonymous boundaries сохранены.
- Live Codex writer: прежний process держал writer после terminal; следующий native load отказывал с active-writer error. Codex/Claude/OpenCode Session закрывается до QueueOutcome, ошибка Close становится failed Outcome. External ACP fixture держит OS writer lock до завершения process; public enrolled Codex idle/queued Follow-up тесты — red timeout/runtime_session_unavailable → green с прежней session.
- Node lifecycle receipts: production sink игнорировал dispatch/resume, оставляя Worker queued/starting при native failure. Authenticated receipts проверяют durable command/Node/Turn/Attempt bindings, активируют accepted starting Attempt и создают один Result для denied/unknown execution. Native receipt не заменяется поздним transport успехом; поздний acceptance не оживляет terminal. `TestAuthenticatedLifecycleReceiptsExposeAcceptanceAndFailure` — red → green, с replay/wrong-Node/late-transport checks. Failed queue receipt сохраняет blocked state. Remote steering/cancel ожидают bounded Node ACK; cancellation receipt не является terminal.
- CC stdin backpressure: blocked Prompt удерживал mutex, Close не мог остановить process. Context-aware write закрывает process/pipe при отмене; Close не ждёт writer mutex и ждёт process termination. `TestReviewClaudePromptCancellationUnderBackpressure` — red зависшие Prompt/Close → green. Resume initialization использует тот же writer.
- CC Cancel: server создавал canceled Result после interrupt receipt, освобождая `/q` раньше native terminal. Cancel теперь сохраняет command intent/receipt и оставляет Attempt active; native Outcome владеет Result. Close ждёт terminal в bounded context. Только доказанное отсутствие handoff prepared queue Attempt позволяет server-only cancellation. `TestCCCancelDoesNotDeliverQueueBeforeNativeTerminal` — red dispatches=2 → green dispatches=1, active Attempt/no Result до native terminal; Close deadline и последующее native completion проверены. Lifecycle fixtures публикуют отдельный controlled native Outcome вместо опоры на server synthetic terminal.
- Удалены только новые Phase 5 explanatory code comments; исходные toolchain directives, shebang и fixture strings сохранены. README/quickstart описывают Codex/native defaults/auth, deterministic topics и отдельный исторический OpenCode путь.

## Проверки

`go test ./...` — PASS; `go build ./...` — PASS. `go test -race ./internal/core ./internal/ctl ./internal/node ./internal/webapi ./cmd/secretaryd` дал PASS для core/node/webapi/cmd; ctl при одновременной нагрузке упал в существующем Approval fixture с TTL 120 ms и response deadline 300 ms (normal WebSocket close до ReceiveCommand, data race не обнаружена). Отдельный `go test -race ./internal/ctl -count=1` — PASS. Все новые queue/recovery/native/cancellation/owner-cookie regression tests проходят. Ticket 03 повторно resolved по scoped criteria после этих проверок.

Проверка added Phase 5 code comments по diff `43b9a8a` — новых explanatory comments нет; `git diff --check` — PASS.

Worktree потребовал `mise trust` для штатного process integration build; первоначальный failure был настройкой нового worktree, после trust полный набор прошёл. Предыдущие Web tests (14) и production build на объединённом 01/03/04 коде прошли; данный batch не меняет renderer behavior.

## Что не закрыто

01/02/04 остаются claimed, 05 blocked. Live rerun исправленной сборки, enrolled topology/MCP/channel matrix и CC same-Attempt steering ещё не доказаны. Omarchy Codex token refresh и CC proxy quota403 — внешние auth blockers. Scope не меняет глобальный harness config, credentials или пользовательские spec diffs. Worker MCP delivery реализуется отдельной веткой после этого batch.
