# Record the Phase 2 architectural foundation

Type: grilling
Status: resolved

## Question

Какие architecture decisions должны ограничить independent Secretary до начала thin implementation?

## Answer

### Scope revision

Раннее решение включало Project и Project checkout в core. Оно отменено решением [Defer Project management from the core](08-defer-project-management.md): первый slice не создаёт Project registry или `create_project`.

Secretary server является единственным source of truth для Person, Personal Conversation, Task, Worker binding, Result и lifecycle. Все clients одного Person видят одну непрерывную Personal Conversation.

Channel adapters являются отдельными processes/packages. Они используют HTTP JSON для commands, WebSocket для live updates и server-issued credentials. Первый slice имеет одного configured owner; explicit account linking остаётся следующей работой.

Execution node сам держит outbound connection к server. Local Node встроен в `secretaryd` как semantic implementation того же contract, что будущий remote Node. Remote Node pairing использует owner-approved одноразовый enrollment code.

Project является logical repository, а Project checkout является node-local копией. Node создаёт checkout внутри configured `projects_root`; Git credentials остаются на Node. Один modifying Worker держит exclusive lease checkout. Coding Worker работает в checkout; Task без Project получает отдельный Workspace.

Core и Node runtime пишутся на Go. Channel adapters не привязаны к языку. Codex является первым Worker runtime через ACP. OpenCode поддерживает ACP и может стать будущим runtime adapter. Runtime harness владеет compaction, server сохраняет durable историю независимо от неё.

SQLite хранит durable state thin slice. Server restart помечает active local Attempt как `interrupted` и не повторяет её автоматически.

Обычные сообщения являются Steering messages и приходят active Secretary или Worker на ближайшей safe boundary. `/q` создаёт Queued message, доставляемое только idle target. Follow-up означает Queued message idle Worker и создаёт новую Attempt.

Worker observer открывается по Worker reference, получает live-only activity и позволяет направить Steering, Queued message или Cancel. Cancel best-effort останавливает только active Attempt, а Task и Worker остаются. Terminal Result verbatim добавляется в Personal Conversation без automatic Secretary turn.

Execution environment принадлежит Node. Первый implementation даёт Worker full access configured OS user. Будущий sandbox или isolated worktree не должен менять Secretary core contract.
