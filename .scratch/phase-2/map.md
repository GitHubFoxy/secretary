# Map: Independent Secretary phase 2

## Destination

Зафиксировать architecture независимого Secretary и затем собрать thin local vertical slice без AgentHub: Go Secretary server, Codex runtime, один local Node и один Channel adapter.

## Notes

- AgentHub и Bridge остаются только POC, а не частью продукта.
- Один Person имеет одну Personal Conversation во всех clients.
- Go: Secretary core и Node runtime. Channel adapters могут быть на любом языке.
- Первый runtime: Codex. ACP-compatible OpenCode остаётся будущим runtime adapter.
- SQLite, один configured owner, local Node и full access входят в первый slice.
- Remote Node, enrollment и sandbox должны быть предусмотрены contract-ами, но не реализуются в первом vertical slice.
- Использовать `CONTEXT.md` как canonical glossary.

## Decisions so far

- [Record the Phase 2 architectural foundation](issues/01-record-phase-2-foundation.md): server-owned state, одна Personal Conversation, pluggable adapters и Nodes, Codex, SQLite, opt-in Worker observer и безопасные lifecycle rules уже выбраны.
- [Preserve Task after Dispatch failure](issues/02-define-core-state-and-command-contract.md): неудачный Dispatch сохраняет Task как `dispatch_failed`, но не создаёт Worker binding и не повторяется автоматически.
- [Research Codex ACP Steering and Cancel](issues/05-research-codex-acp-steering.md): upstream Codex ACP adapter даёт standard Cancel и negotiated `_session/steering` поверх Codex app-server `turn/steer`.
- [Define the Node and Codex runtime contract](issues/03-define-node-and-runtime-contract.md): Node владеет environment и Codex ACP runtime, принимает Dispatch только после session readiness и безопасно восстанавливается через `interrupted`.
- [Define the Channel adapter and Worker observer contract](issues/04-define-channel-adapter-and-observer-contract.md): первым client будет web с cookie session, ordered WebSocket sync и opt-in live Worker observer.
- [Define the minimal Task state machine](issues/06-define-minimal-task-state-machine.md): Task переживает terminal Attempt и dispatch failure, а closure безопасно отменяет active Attempt перед архивированием binding.
- [Defer Project management from the core](issues/08-defer-project-management.md): Worker сам может clone или создать worktree через shell; Project registry и `create_project` не входят в core или первый slice.
- [Define the first thin vertical slice](issues/07-define-thin-vertical-slice.md): web → Secretary → local Codex Worker → observer Steering/Stop → Result, без Project management.
- [Define the Secretary runtime and authority](issues/09-define-secretary-runtime-and-authority.md): persistent Codex Secretary управляет lifecycle только через capability-scoped `secretaryctl`.
- [Prove the first thin vertical slice](issues/15-prove-thin-vertical-slice.md): real pinned `codex-acp` v1.10.0 with local Codex auth and `gpt-5.6-luna` прошёл login, Secretary, Worker Result, Steering, Stop, recovery и durable SQLite proof.

## Not yet specified

- Account linking между identities разных Channel adapters после single-owner slice.
- Нужна ли позже отдельная Projects capability для repository registry, checkout lease и isolated worktree.
- GitHub App credential brokering с short-lived repo-scoped credentials вместо Node-local Git auth, если появится Projects capability.
- Sandbox Execution environment и явный merge/publish flow.
- Remote Node implementation, TLS и node health/reconnect recovery.
- Bounded replay Worker activity для observer.
- Queue policy для нескольких modifying Tasks после первого `busy` ответа.

## Out of scope

- AgentHub и Bridge как production dependencies.
- Postgres, Kafka и event sourcing.
- Sandbox implementation, TLS, ACLs и multi-user sharing в первом thin slice.
