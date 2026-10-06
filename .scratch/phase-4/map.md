# Map: Secretary phase 4

## Destination

Построить persistent personal AI поверх компьютеров и harnesses. Secretary хранит intent и Personal Conversation, а Workers выполняют работу на выбранных Projects, Nodes и HarnessInstances. Phase 4 должна дать один coherent API для Web и Telegram, два Execution Nodes, OpenCode v2 по умолчанию и selectable `fx`, Claude Code и Codex, безопасное восстановление и явную observability.

`Task` не входит в новую product model. Он остаётся только legacy migration data.

## Notes

- Server является source of truth для Person, Secretary identity, Personal Conversation, Secretary turns, Workers, Turns, Attempts, AttemptOutcomes, Results, Projects, Nodes, Clients, Approvals, events и deliveries.
- Worker является основной пользовательской execution entity и живёт до explicit close.
- `final` AttemptOutcome закрывает Turn и создаёт один Result. Retry создаёт новый Attempt того же Turn только через internal-only `retry_attempt` после terminal `retryable` AttemptOutcome. Uncertain execution не retry-ится автоматически.
- Worker после создания навсегда привязан к одному Node и HarnessInstance. Недоступный Node не вызывает migration.
- Native runtime session IDs принадлежат только Node и никогда не попадают в server Worker record или Worker envelope.
- Secretary runtime и default Worker harness независимы. В конфигурации это `secretary.harness` и `worker_policy.default_harness`.
- Secretary обрабатывает один turn за раз. Входящие сообщения при active turn попадают в durable ordered queue. Workers работают параллельно.
- Static HarnessInstance/capability contract создаётся до Node transport. Node transport только перевозит этот contract, а реальные probes выполняются отдельно.
- Node protocol использует authenticated outbound connection, durable local outbox и `command_id` dedupe.
- Normalized activity публикуется только в пределах capabilities конкретного HarnessInstance.
- `user.md` остаётся external Markdown file с durable revision, atomic write и следующим-turn context visibility.
- Profiles остаются внешними Markdown files. Product deployment self-hosted/private-network-first.
- Control Room доступен только при `--debug`.
- Telegram входит в MVP: General chat принадлежит Secretary, отдельный Topic соответствует каждому Worker. Secretary stream и Worker activity агрегируются, а не превращаются в raw message spam.
- Paired Clients используют общий API с explicit scopes и не добавляют отдельную domain entity.

## Decisions so far

- `spec.md` является утверждённым Phase 4 contract для domain model, lifecycle, Node boundary, Client API, deployment, migration и acceptance.
- Secretary владеет intent, Worker владеет execution.
- `message_worker` сам выбирает steer для active Worker, ответ на pending `needs_input`, Follow-up для idle Worker или resume после interrupted state.
- `respond_worker { request_id, response }` является единой Node command для Approval и `needs_input`.
- Worker или harness не получают server callback capability. Terminal events идут через Node adapter и authenticated Node connection.
- OpenCode v2 является default Worker и Secretary harness; `fx`, Claude Code и Codex остаются selectable adapters с explicit coverage.
- `sex` и `sex setup` сохраняются как исторический CLI contract.
- [Ticket 01](issues/01-worker-first-core-state-and-migration-contract.md) закрепляет Worker-first core persistence в `internal/core`: новые Worker, Turn, AttemptOutcome и Result records отделены от legacy Task tables, а recovery и retry имеют явные terminal semantics.
- [Ticket 06b](issues/06b-dispatch-resolver-and-binding.md) закрепляет immutable Worker binding транзакционным сравнением Project revision, Node state и observed inventory; replay `worker.create` обходит текущие Project и inventory.
- [Ticket 17](issues/17-generate-worker-topic-titles.md) использует отдельный OpenCode v2 для названий Telegram Topics. Harness/model/effort и внешний `title-generation-prompt.md` задаются в `[telegram]`, не меняют Secretary/Worker profiles и имеют детерминированный fallback.
- [Ticket 19](issues/19-show-worker-prompt-in-topic.md) публикует dispatched intent первым сообщением Worker Topic через durable outbox; флаг доставки в TopicMapping предотвращает дубли при restart/replay и сохраняется после отправки последней части.
- [Ticket 20](issues/20-map-acp-progress-titles-correctly.md), `resolved`: отделяет explicit ACP tool identity от progress `title`, коррелирует lifecycle updates по invocation и показывает в Telegram/Web только безопасный компактный preview без tool output.
- [Ticket 22](issues/22-investigate-oversized-telegram-messages.md): owner подтвердил полный inline Result в Topic первым, затем General, с semantic/grapheme-aware splitting без file/preview. Local implementation и Bot API HTTP fixtures PASS; live Telegram acceptance pending.
- [Ticket 30](issues/30-diagnose-worker-web-fetch-failure.md), `resolved`: Worker Activity принимает только явную allowlisted category/code/HTTP status при `failed`; свободные error text, arguments и outputs не публикуются. Историческая причина `web_fetch` остаётся неизвестной: сохранён только `failed`, поздний shell HTTP 200 не подтверждает native path.

## Work order

### Foundation and transport

1. [01: Worker-first core state and migration contract](issues/01-worker-first-core-state-and-migration-contract.md)
2. [02: Durable events, idempotency and server delivery](issues/02-durable-events-idempotency-and-server-delivery.md)
3. [03a: Secretary identity, stream and input queue](issues/03a-secretary-identity-stream-and-input-queue.md) и [04a: HarnessInstance static contract](issues/04a-harness-instance-static-contract.md) можно выполнять параллельно.
4. [04: Node protocol and local reliability](issues/04-node-protocol-and-local-reliability.md)
5. [05b: Harness adapter discovery and real probes](issues/05b-harness-adapter-discovery-and-real-probes.md)
6. [13a: Node executable, pairing and reconnect](issues/13a-node-executable-pairing-and-reconnect.md)

### Execution

7. [07: Projects and workspace policy](issues/07-projects-and-workspace-policy.md)
8. [06b: Dispatch resolver and immutable binding](issues/06b-dispatch-resolver-and-binding.md)
9. [06a: Worker lifecycle and Secretary tools](issues/06a-worker-lifecycle-and-secretary-tools.md)
10. [08: Approval and input round trip](issues/08-approval-and-input-round-trip.md)
11. [03b: Secretary context reconstruction](issues/03b-secretary-context-reconstruction.md)

### Clients and operations

12. [09: Client API, pairing and replay](issues/09-client-api-pairing-and-replay.md)
13. [10: Web Worker-first UI and observers](issues/10-web-worker-first-ui-and-observers.md), [11: Debug-only Control Room](issues/11-debug-only-control-room.md) и [12: Telegram Topics adapter](issues/12-telegram-topics-adapter.md) можно выполнять параллельно после общего API.
14. [13b: Node packaging and private deployment](issues/13b-node-packaging-and-private-deployment.md)

### Migration and proof

15. [14: Phase 3 migration](issues/14-phase3-migration.md)
16. [15: Phase 4 acceptance gate](issues/15-phase4-acceptance-gate.md)

### Telegram and Worker presentation follow-ups

- [18: Configure the Worker Topic title model](issues/18-configure-topic-title-model.md), `resolved`. Секция `[telegram]` задаёт `title_model = "gpt-6-luna"` и `title_model_reasoning = "minimal"`; validation и defaults независимы от Secretary/Worker profiles.
- [17: Generate readable Worker Topic titles](issues/17-generate-worker-topic-titles.md), `resolved`. Название получается из dispatched intent через OpenCode v2 `--standalone`; `title_harness` и `title_prompt` явно задаются в config. Внешний prompt поддерживает reload, failures/timeout дают 60-rune fallback. Go/race/vet/build и native OpenCode с локальным HTTP fixture проверены.
- [19: Show the dispatched Worker prompt in its Topic](issues/19-show-worker-prompt-in-topic.md), `resolved`. Показывает задачу без внутренних инструкций; retry/replay и порядок сообщений покрыты тестами.
- [20: Map ACP progress titles correctly](issues/20-map-acp-progress-titles-correctly.md), `resolved`.
- [21: Keep Worker progress out of the final Result](issues/21-separate-worker-progress-from-telegram-result.md) исправляет смешивание progress, потерю переносов и неотформатированные Markdown-ссылки.
- [22: Decide how to deliver oversized Worker messages](issues/22-investigate-oversized-telegram-messages.md), `claimed`: owner-approved inline delivery реализована локально с parsed UTF-16 limit, Markdown/grapheme boundaries и source-byte checkpoints; ждёт live Telegram acceptance.

### Сквозная продуктовая проверка

- [23: Чистая машина, установка, CLI и реальные harness adapters](issues/23-clean-machine-product-e2e-journey.md) задаёт один поэтапный E2E-прогон с агентами-исполнителями и независимыми проверками runner. Дополняет tickets 15, 25 и 26; требует фиксации installer/release contract и тестового provisioning.

### Тикеты, перенесённые из GitHub

Все перенесённые тикеты имеют статус `needs-triage`. Их исходные требования сохранены для сверки с текущим contract, а не автоматически добавлены в обязательный Phase 4 gate.

- [24: Самообновление через supervisor](issues/24-self-update-supervisor.md), ранее GitHub #1.
- [25: Prompt-driven E2E](issues/25-prompt-driven-e2e-acceptance-suite.md), ранее GitHub #2.
- [26: Полный пользовательский путь](issues/26-full-product-user-journey.md), ранее GitHub #3.

Оригинальные API snapshots, комментарии и timeline сохранены в [`github-import/`](github-import/).

### Снятая поддержка Pi viewer

- Ticket 16 ([Pi Client integration](issues/16-pi-client-integration.md)) оставлен только как историческая запись. Ticket 28 снял эту поддержку; его прежнее acceptance evidence не является текущим обязательством.
- [28: Удаление Pi viewer и Secretary extension](issues/28-remove-pi-viewer-extension.md), `resolved`. Специализированные docs, Phase 5 gate и effort удалены; общие Client API и privacy coverage сохранены. Будущий Pi Worker harness из ticket 27 остаётся отдельной задачей.

## Инциденты и изменения после deploy 459709c

- [21: Чистый краткий Worker Result и Telegram formatting](issues/21-separate-worker-progress-from-telegram-result.md), `claimed`. Screenshot погоды сохранён. ACP test теперь проверяет публичную progress availability и final/turn boundaries без cross-channel order assertion; `-count=20` и race в пяти пакетах `-count=2` прошли. Финальный полный gate `p4-ticket15-domain-drain-459709c-20261005T105601Z` завершился FAIL на `TestTwoSecretaryNodeProcessesPairInventoryAndReconnect` (authenticated reconnect timeout); один изолированный rerun прошёл, но не отменял FAIL полного gate. Позднее корректный полный gate `p4-ticket15-opencode-probe-scope-459709c-20261005T113150Z` прошёл все 9 stages; process reconnect regression прошёл `-count=3`, combined race-пакет — `-count=2` (подробности в `docs/phase4-release-gate.md`). Ручная матрица остаётся pending: Ticket 29 General → Worker Topic → следующий turn — `NOT RUN`; Ticket 22 live Topic/General — `BLOCKED`; остальные `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` сохранены.
- [29: Не повторять Worker Result ответом Secretary](issues/29-stop-secretary-echo-after-worker-result.md), `claimed`. Owner одобрил additive opt-in `addressed-reply v1`; локально реализованы exact Core reply/origin links, atomic command-origin intent→delivery linking, pending/uncertain command→exact Worker Turn→Result join, queued spawn link в Worker/Turn transaction, server-owned MCP `reply_to_user`, managed profile instructions и transactional post-Result suppression. Исправлен late `respond_worker` ACK blocker: Runtime timeout остаётся uncertain, production ServerManager подключён к durable Core outcome sink, а authenticated receipt сверяется по Node/command/Turn/Attempt даже после удаления waiter; denial сохраняется до явного retry, terminal Turn не оживляется. Core/CTL/Node focused package tests, выбранные `-race`, `go vet` затронутых пакетов и build `secretaryd` прошли. Combined gate после owner-retry, strict-DTO, revoke-race и owner observer refresh fixes прошёл: `p4-ticket15-approval-auth-refresh-459709c-20261005T102708Z`, exit 0. Более поздний финальный gate после ACP test correction завершился FAIL на Node authenticated reconnect timeout; см. Ticket 21/Ticket 15 evidence. Real Telegram General → Worker Topic → следующий user turn остаётся pending; ранее один полный `internal/node` run имел `Node is offline` failure с успешным isolated rerun. External profiles и production не менялись; Standards/Spec reviews отложены. Дополнительный локальный `addressed-reply-v1` completion допускает zero assistant chunks только при verified OpenCode v2 end_turn/RPC/drain evidence и одной exact durable reply identity; Core проверяет turn/input атомарно. Typed metadata не попадает в public DTO; Worker и legacy gate не расширены. Focused Go/race и private unpaid native HTTP fixtures прошли. Прежний вызов `umask 0022 ./scripts/phase4-release-gate.sh` был NOT RUN. Корректный запуск `p4-29-addressed-reply-gate-20261006T090424Z` завершился exit 1: stages 1–2 PASS, Stage 3 Go suite FAIL на `TestOpenCodeConfigurationRegistrationAndFailClosed/missing-mode/resume=true` (`safe protocol counts unavailable`), stages 4–9 не запускались; private log указан в release-gate ledger. Повторного full gate пока не было. Focused diagnosis установила startup/counters race и затенение runtime error тестовым file-read error; loopback ready barrier и отдельные counters устранили fixture race без production timeout change. Full matrix, `-count=5`, `-race -count=3` прошли. Финальный корректный gate до review `p4-29-addressed-reply-finalgate-20261006T094440Z` exit0 сохранён. Оба независимых review позднее выявили один deduplicated blocker: непроверенный OpenCode response.Summary обходил assistant-chunk/exact-reply evidence. Summary-only public RED→GREEN теперь fail-closed; actual parent messageId/end_turn и exact durable MCP reply успешны, legacy summary behavior сохранён, Start/Resume используют общий validator. Intermediate gate `p4-29-summary-evidence-finalgate-20261006T101820Z` Stage3 FAIL (устаревшее strict fixture ожидало summary); corrected gate `p4-29-summary-evidence-corrected-finalgate-20261006T102309Z` exit0, stages1–9 PASS. Independent parent re-reviews pending; Ticket остаётся claimed, Telegram acceptance NOT RUN.
  - Дополнительно устранены оба approval blocking findings из Spec review: decision/response/actor/command ID durable до handoff; accepted exact receipt завершает исходный intent, а resolving не истекает. Conflicting retry возвращает 409; UI показывает `resolving` и использует публичный Approval ID; wrong Node/Turn/Attempt отвергается; resolution и late ACK не возобновляют terminal Turn и не заменяют canonical Result. README приведён в соответствие §4.6 Worker-first. Core/CTL/WebAPI packages, selected Node tests, race regressions, vet/build и `git diff --check` прошли. В последующем re-review fix resolving Approval после refresh/restart получил owner-only `POST /v1/approvals/{id}/retry`: только saved decision/response и прежний command/Attempt/Turn, без автоматического повтора или client payload; UI показывает явную disabled-while-in-flight кнопку, public DTO скрывает response. RED→GREEN Core/CTL/WebAPI/UI regressions покрывают restart, потерю connection/ACK, authenticated receipt, Result и независимую queued Turn, owner scope, отказ подмены payload и stale duplicate. Credential Client responses по всем approve/deny/retry branches общего handler-а используют strict allowlist `publicWorkerDetailsStrictFromDetails`; raw idempotency ledger не выходит в HTTP. Только approval:write не даёт worker/approval read; scopes не расширялись. Public HTTP regression с безопасными булевыми assertions покрывает resolving retry, duplicate idempotency, resolved no-handoff и соседний deny; отдельно UI передаёт public `approval.id`. Combined suite/gate15 после strict DTO прошёл в `p4-ticket15-approval-retry-privacy-459709c-20261005T095131Z`, exit 0; latest revoke/refresh gate — `p4-ticket15-approval-auth-refresh-459709c-20261005T102708Z`, exit 0. Revoke между auth checks теперь завершает запрос до mutation; owner refresh показывает saved approved/denied без response/actor/private command ID; approval:write scope, Owner capability, Client idempotency и public `approval.id` сохранены. RED public HTTP tests не используют sleep. Real Telegram General → Worker Topic → следующий user turn — `NOT RUN`; Ticket 22 Topic/General — `BLOCKED`; остальные manual `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` сохранены. UI tests/build прошли. FX compatibility, profiles, restart/credential/permission поведения не менялись.
  - Standards vocabulary finding grounded in `docs/agents/domain.md` and current GLOSSARY; added one definition for existing spec/Core `Approval`, no new entity/scope and no test rename. Re-review не запускался.
- [30: Причина web_fetch failure](issues/30-diagnose-worker-web-fetch-failure.md), `resolved`. Реализована безопасная public-boundary metadata; historical incident остался без safe category/code/status, поэтому причина не установлена и ticket31 не объявлен исправлением.
- [31: OpenCode v2 вместо default fx](issues/31-make-opencode-v2-default-harness.md), `claimed`; clean-install defaults, dispatch и observed v2 inventory локально реализованы. Auth readiness исправлена: Node и оба `sex doctor` используют `auth list --format json --standalone` в Go-validated selected store, требуют stored `connections[].type=credential` и fail-closed при пустом/ошибочном JSON, неверной версии/контексте или ambient-only auth. Final deterministic release gate PASS; native selected-store inventory/ACP initialize подтвердили ready и точные model/reasoning metadata. Ticket остаётся открытым: нужны independent review, rollout, Telegram и live existing-fx Follow-up/restart; ACP v2.0.22 не публикует explicit tool identity. Ticket29 addressed-reply-v1 использует только проверенный v2 terminal adapter для opt-in Secretary и не меняет Worker defaults/pins, Node inventory, native store или production auth; private permissions/Resume/MCP fixtures выполнены локально. Упомянутая Ticket29 попытка с неверным `umask` синтаксисом была NOT RUN; корректный gate `p4-29-addressed-reply-gate-20261006T090424Z` завершился Stage 3 FAIL (details в `docs/phase4-release-gate.md`); последующая focused diagnosis/fix test-only readiness seam прошла matrix и finite count/race repeats; после summary-evidence review fix gate `p4-29-summary-evidence-corrected-finalgate-20261006T102309Z` exit0, stages 1–9 PASS. No new Ticket31 review/deployment evidence; rollout и live Telegram остаются pending.
- [32: Telegram /model и /reasoning](issues/32-telegram-model-and-reasoning-commands.md), `needs-info`. Group/Topic routing, owner authorization и runtime application требуют согласованного UX.
- [33: Отдельное persistent native state OpenCode](issues/33-isolate-opencode-native-state.md), `claimed`. Secretary и co-located Worker Node используют одну stable native DB, remote Node — собственную; owner-only legacy transition fail-closed при sessions/conflicts. Strict Go decoder отклоняет duplicate/escaped/case aliases, invalid UTF-8, unpaired surrogates и malformed deployment/manifests до selection; raw Unicode и valid surrogate pairs сохраняются. Public RED показал успешный selection `node-�/data/opencode-native` с raw `0xFF`; GREEN и public config/manifest Unicode matrix, CLI fail-closed и native unpaid v2.0.22 acceptance прошли. Возможный Middle Man устранён узко: wrapper без production callers удалён, тесты используют typed selection seam. Предыдущие gate FAIL сохранены; старые logged PASS перечислены в issue. Последний full gate после fix под `umask 0022` прошёл все 9 stages (ledger label `p4-33-invalid-utf8-fullgate-20261006`, 6 Oct 2026, exit 0); gate script не печатает run ID, отдельный logfile не сохранялся. Owner login/authenticated live acceptance, production install/restart и independent re-review ожидают; ticket остаётся claimed. Ticket 31 и manual Ticket 15 acceptance остаются открыты.
- [34: OpenCode → Codex и полный пользовательский путь](issues/34-switch-opencode-to-codex-and-check-full-path.md), подготовительный прогон заблокирован tickets31/21/22/29. На целевом Codex Node не наблюдаются exact pins `openai/gpt-6.1-sol` и `openai/gpt-6-luna`; не подменять модели и не выполнять owner login без решения. Прогон не заменяет текущую работу OpenCode.

## Ближайший milestone: первая простая рабочая версия

- Приоритет пользователя — довести Secretary до понятной, полностью end-to-end проверенной работы хотя бы на одном harness, а не расширять продукт новыми системами.
- Утверждённый порядок: закончить ticket31 (OpenCode), затем tickets21/22/29 — чистый текст и оформление Result, понятная доставка длинных сообщений, отсутствие дополнительного пересказа Secretary; после этого human review с пользователем. Проверить живой Telegram путь General → Worker Topic → реальные tools/activity → terminal Result → следующий пользовательский запрос/Follow-up.
- Worker Topic должен показывать настоящие identities используемых tools и исходное поручение; General остаётся доступным для сообщений Secretary. Не угадывать tool names из progress titles и не раскрывать внутренние инструкции, секреты или raw ACP.
- Пользователь подтвердил: довести ticket31 с OpenCode; если полный путь работает правильно — остаёмся на OpenCode. Локальную реализацию продолжил субагент `gpt-6.1-sol` / `xhigh`; родитель проверил diff и повторил native/live acceptance. Resume исправлен; неоднозначный live test prompt выявлен независимым review и исправлен без ослабления gate. Ticket31 остаётся claimed до production/Telegram acceptance; explicit tool identities у native ACP пока отсутствуют. Далее локальная presentation работа21/22/29, без автоматического deployment. Rollout остаётся acceptance-gated, existing fx bindings/history не мигрировать. Проверка Codex заведена отдельно в ticket34 и сейчас не запускается.
- Это ближайший операционный milestone, а не отмена остальных требований полного Phase4 release gate.

## Будущие направления — после первой рабочей версии

1. **Memory system** — долговременная память пользователя, предпочтений и значимого контекста между поручениями и сессиями. Нужна в будущем; scope, хранение и правила обновления пока не выбраны.
2. **Skill system с самообновлением** — общая библиотека пользовательских способов выполнения задач: сохранять объяснённые процедуры, загружать их при следующих подходящих задачах и улучшать по опыту/исправлениям. Пользовательские инструкции предполагаются универсальными, без отдельных текстовых версий на каждый harness. Автоматическое создание/обновление и границы одобрения пока не выбраны.

Вопросы GrillMe отложены, не являются принятыми решениями: записывать/обновлять skills автоматически или с подтверждением; как отличать новое поручение в General от уточнения предыдущего. До выбора сохраняется текущий утверждённый routing/ordered-input-queue contract. Реализацию этих систем не включать в текущие tickets21/22/29/31.

## Fog

Fog содержит только implementation-level вопросы. Product decisions из `spec.md` не переоткрываются.

- Точные версии и wire details установленных Claude Code, Codex, `fx` и OpenCode проверяются на manual acceptance, а не меняют Phase 4 model.
- Конкретная SQLite migration sequence и формат read-only legacy Task rows выбираются в ticket 01 и ticket 14.
- Формат Node enrollment и transport credentials выбирается при реализации, но Node всегда использует outbound authenticated connection и отдельное pairing.
- Tailscale, launchd и operator packaging остаются в 13b после появления настоящего Node executable в 13a.
- Telegram bot provisioning остаётся polling-first и private, без public webhook в MVP.

## Release gate

Ticket15 остаётся `claimed`, пока реальная обязательная matrix не полна. Локально подготовленные реализации31/21, private unpaid tests33 и deterministic suite не заменяют native owner-auth acceptance. Tickets22/29 получили owner approval; ticket29 остаётся claimed до отложенных Standards/Spec reviews, общего suite и real Telegram acceptance. Ticket30 остаётся resolved с историческим evidence gap; ticket34 заблокирован перечисленными dependencies и отсутствием точных target Codex pins. Manual evidence сохраняет наблюдавшиеся `FAIL`, `BLOCKED`, `UNAVAILABLE` и `NOT RUN`; fixtures и CLI help не дают real-harness `PASS`.

Phase 4 готова после прохождения acceptance scenario из `spec.md` на чистой конфигурации и после restart:

- два Nodes с observed HarnessInstances;
- manual Project с разными path mappings;
- Web и Telegram с одной Personal Conversation;
- полный Secretary stream, ordered input queue и direct Result delivery без дополнительного Secretary turn;
- изменение `user.md`, видимое в следующем Secretary context;
- Worker-first lifecycle без Task и child Workers;
- AttemptOutcomes для retries и ровно один Result на Turn;
- default OpenCode v2, explicit `fx`, Claude Code, Codex и visible error для неизвестного model без model substitution;
- Approval и `needs_input` через Node `respond_worker`;
- immutable Worker binding без migration;
- durable Node outbox после network loss;
- command dedupe без второго process;
- Telegram aggregation/throttling;
- security, revoke, restart, frontend и Go checks из ticket 15.
