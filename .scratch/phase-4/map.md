# Map: Secretary phase 4

## Destination

Построить persistent personal AI поверх компьютеров и harnesses. Secretary хранит intent и Personal Conversation, а Workers выполняют работу на выбранных Projects, Nodes и HarnessInstances. Phase 4 должна дать один coherent API для Web и Telegram, два Execution Nodes, обязательные `fx`, Claude Code и Codex, безопасное восстановление и явную observability.

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
- Pi Client подключается после стабилизации общего Client API и не добавляет новую domain entity.

## Decisions so far

- `spec.md` является утверждённым Phase 4 contract для domain model, lifecycle, Node boundary, Client API, deployment, migration и acceptance.
- Secretary владеет intent, Worker владеет execution.
- `message_worker` сам выбирает steer для active Worker, ответ на pending `needs_input`, Follow-up для idle Worker или resume после interrupted state.
- `respond_worker { request_id, response }` является единой Node command для Approval и `needs_input`.
- Worker или harness не получают server callback capability. Terminal events идут через Node adapter и authenticated Node connection.
- `fx`, Claude Code и Codex обязательны для MVP acceptance. OpenCode остаётся compatibility target и не заменяет Claude Code.
- `sex` и `sex setup` сохраняются как исторический CLI contract.
- [Ticket 01](issues/01-worker-first-core-state-and-migration-contract.md) закрепляет Worker-first core persistence в `internal/core`: новые Worker, Turn, AttemptOutcome и Result records отделены от legacy Task tables, а recovery и retry имеют явные terminal semantics.
- [Ticket 06b](issues/06b-dispatch-resolver-and-binding.md) закрепляет immutable Worker binding транзакционным сравнением Project revision, Node state и observed inventory; replay `worker.create` обходит текущие Project и inventory.

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
17. [16: Pi Client integration](issues/16-pi-client-integration.md) идёт после 09 параллельно и не блокирует основной MVP gate.

## Fog

Fog содержит только implementation-level вопросы. Product decisions из `spec.md` не переоткрываются.

- Точные версии и wire details установленных Claude Code, Codex, `fx` и OpenCode проверяются на manual acceptance, а не меняют Phase 4 model.
- Конкретная SQLite migration sequence и формат read-only legacy Task rows выбираются в ticket 01 и ticket 14.
- Формат Node enrollment и transport credentials выбирается при реализации, но Node всегда использует outbound authenticated connection и отдельное pairing.
- Tailscale, launchd и operator packaging остаются в 13b после появления настоящего Node executable в 13a.
- Telegram bot provisioning остаётся polling-first и private, без public webhook в MVP.

## Release gate

Phase 4 готова после прохождения acceptance scenario из `spec.md` на чистой конфигурации и после restart:

- два Nodes с observed HarnessInstances;
- manual Project с разными path mappings;
- Web и Telegram с одной Personal Conversation;
- полный Secretary stream, ordered input queue и direct Result delivery без дополнительного Secretary turn;
- изменение `user.md`, видимое в следующем Secretary context;
- Worker-first lifecycle без Task и child Workers;
- AttemptOutcomes для retries и ровно один Result на Turn;
- default `fx`, explicit Claude Code, Codex и visible error для неизвестного model без fallback;
- Approval и `needs_input` через Node `respond_worker`;
- immutable Worker binding без migration;
- durable Node outbox после network loss;
- command dedupe без второго process;
- Telegram aggregation/throttling;
- security, revoke, restart, frontend и Go checks из ticket 15.
