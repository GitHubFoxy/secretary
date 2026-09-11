# Map: Secretary phase 4

## Destination

Построить persistent personal AI поверх компьютеров и harnesses. Secretary хранит intent и Personal Conversation, а Workers выполняют работу на выбранных Projects, Nodes и HarnessInstances. Phase 4 должна дать один coherent API для Web и Telegram, два Execution Nodes, обязательные `fx`, Claude Code и Codex, безопасное восстановление и явную observability.

`Task` не входит в новую product model. Он остаётся только legacy migration data.

## Notes

- Server является source of truth для Person, Secretary identity, Personal Conversation, Secretary turns, Workers, Turns, Attempts, AttemptOutcomes, Results, Projects, Nodes, Clients, Approvals, events и deliveries.
- Worker является основной пользовательской execution entity и живёт до explicit close.
- Retry создаёт новый Attempt того же Turn. Каждый Attempt получает AttemptOutcome. Turn получает один пользовательский Result.
- Worker после создания навсегда привязан к одному Node и HarnessInstance. Недоступный Node не вызывает migration.
- Native runtime session IDs принадлежат только Node и никогда не попадают в server Worker record или Worker envelope.
- Secretary runtime и default Worker harness независимы. В конфигурации это `secretary.harness` и `worker_policy.default_harness`.
- Secretary обрабатывает один turn за раз. Входящие сообщения при active turn попадают в durable ordered queue. Workers работают параллельно.
- Node protocol использует authenticated outbound connection, durable local outbox и `command_id` dedupe.
- Normalized activity публикуется только в пределах capabilities конкретного HarnessInstance.
- Profiles остаются внешними Markdown files. Product deployment self-hosted/private-network-first.
- Control Room доступен только при `--debug`.
- Telegram входит в MVP: General chat принадлежит Secretary, отдельный Topic соответствует каждому Worker.
- Pi Client подключается после стабилизации общего Client API и не добавляет новую domain entity.

## Decisions so far

- `spec.md` является принятым Phase 4 contract для domain model, lifecycle, Node boundary, Client API, deployment, migration и acceptance.
- Secretary владеет intent, Worker владеет execution.
- `message_worker` сам выбирает steer для active Worker, ответ на pending `needs_input`, Follow-up для idle Worker или resume после interrupted state.
- `respond_worker { request_id, response }` является единой Node command для Approval и `needs_input`.
- Worker или harness не получают server callback capability. Terminal events идут через Node adapter и authenticated Node connection.
- `fx`, Claude Code и Codex обязательны для MVP acceptance. OpenCode остаётся compatibility target и не заменяет Claude Code.
- `sex` и `sex setup` сохраняются как исторический CLI contract.

## Work order

### Foundation

1. [01: Worker-first core state and migration contract](issues/01-worker-first-core-state-and-migration-contract.md)
2. [02: Durable events, idempotency and server delivery](issues/02-durable-events-idempotency-and-server-delivery.md)
3. [03: Secretary runtime, stream and input queue](issues/03-secretary-runtime-stream-and-input-queue.md)
4. [04: Node protocol and local reliability](issues/04-node-protocol-and-local-reliability.md)

### Execution

5. [05: HarnessInstance inventory and adapter capabilities](issues/05-harness-instance-inventory-and-adapter-capabilities.md)
6. [07: Projects and workspace policy](issues/07-projects-and-workspace-policy.md)
7. [06: Worker lifecycle, dispatch and Secretary tools](issues/06-worker-lifecycle-dispatch-and-secretary-tools.md)
8. [08: Approval and input round trip](issues/08-approval-and-input-round-trip.md)

### Clients and deployment

9. [09: Client API, pairing and replay](issues/09-client-api-pairing-and-replay.md)
10. [10: Web Worker-first UI and observers](issues/10-web-worker-first-ui-and-observers.md)
11. [11: Debug-only Control Room](issues/11-debug-only-control-room.md)
12. [12: Telegram Topics adapter](issues/12-telegram-topics-adapter.md)
13. [13: Node packaging and private deployment](issues/13-node-packaging-and-private-deployment.md)

### Migration and proof

14. [14: Phase 3 migration](issues/14-phase3-migration.md)
15. [15: Phase 4 acceptance gate](issues/15-phase4-acceptance-gate.md)
16. [16: Pi Client integration](issues/16-pi-client-integration.md)

## Fog

Fog содержит только implementation-level вопросы. Product decisions из `spec.md` не переоткрываются.

- Точные версии и wire details установленных Claude Code, Codex, `fx` и OpenCode проверяются на manual acceptance, а не меняют Phase 4 model.
- Конкретная SQLite migration sequence и формат read-only legacy Task rows выбираются в ticket 01 и ticket 14.
- Формат Node enrollment и transport credentials выбирается при реализации, но Node всегда использует outbound authenticated connection и отдельное pairing.
- Telegram bot provisioning остаётся polling-first и private, без public webhook в MVP.
- Pi Client не блокирует основной Worker/API gate, если общий Client contract уже стабилен.

## Release gate

Phase 4 готова после прохождения acceptance scenario из `spec.md` на чистой конфигурации и после restart:

- два Nodes с observed HarnessInstances;
- manual Project с разными path mappings;
- Web и Telegram с одной Personal Conversation;
- полный Secretary stream и ordered input queue;
- Worker-first lifecycle без Task и child Workers;
- AttemptOutcomes для retries и ровно один Result на Turn;
- Approval и `needs_input` через Node `respond_worker`;
- immutable Worker binding без migration;
- durable Node outbox после network loss;
- command dedupe без второго process;
- обязательные real harness checks для `fx`, Claude Code и Codex;
- security, revoke, restart, frontend и Go checks из ticket 15.
