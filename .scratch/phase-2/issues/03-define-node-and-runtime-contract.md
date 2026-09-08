# Define the Node and Codex runtime contract

Type: grilling
Status: resolved
Blocked by: 02

## Question

Какой gRPC contract связывает Secretary server и Execution node для enrollment, Dispatch, Project checkout creation, Steering, queued Follow-up, Cancel, activity и terminal Result?

Нужно отделить Node operations от Codex ACP runtime adapter, включая fallback, когда active turn не steerable.

## Answer

Node держит outbound gRPC stream к Secretary server. Dispatch считается accepted только после того, как Node создал Execution environment и готовую Codex session, способную принять observer, Steering и Cancel. Server сохраняет binding до отправки Dispatch.

Node владеет Workspace, Worker process, runtime session identifiers, activity и Cancel. Server владеет state, Conversation entries, Task и Result routing. FullAccessEnvironment является первым Node implementation; sandbox останется реализацией той же границы. Project checkout и isolated worktree не входят в первый slice.

Node запускает pinned upstream `codex-acp`. Standard ACP `session/cancel` обрывает current turn через Codex `turn/interrupt`. Negotiated `_session/steering` extension проводит active input в Codex `turn/steer`. Если runtime не steerable в compaction или review, Node сохраняет Steering pending и доставляет его после idle.

Node отправляет live-only Worker activity только по active observer subscription. Terminal Result отправляется server idempotently по `(worker_ref, attempt)`.

Restart Node переводит active Attempt в `interrupted`, без automatic retry. После этого Follow-up сначала делает `session/load` сохранённой runtime session; при неудаче Node возвращает `runtime_session_unavailable`, а не создаёт silent новую session.
