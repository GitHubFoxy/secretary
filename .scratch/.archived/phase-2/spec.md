# Independent Secretary: первый thin vertical slice

Triage: ready-for-agent

## Problem Statement

AgentHub и Bridge доказали, что persistent Worker, Dispatch, Result и Follow-up работают, но они не являются продуктовой архитектурой. Нужен независимый personal Secretary, который хранит одну непрерывную Personal Conversation, управляет persistent Workers и работает через заменяемые Channel adapters и Execution nodes.

Первый slice должен дать владельцу работающий web client: поручить Secretary задачу, открыть конкретного Worker, наблюдать live activity, изменить условие в ходе работы или остановить Worker, а затем увидеть terminal Result в общей Conversation. Всё это должно переживать обычные ошибки Dispatch и не дублировать входящие или terminal events.

## Solution

Поставить Go Secretary server с SQLite, встроенным local Execution node и persistent Codex ACP runtime. Web client является первым Channel adapter. Secretary является отдельной persistent Codex session и вызывает только capability-scoped `secretaryctl` для управления Task lifecycle.

Server владеет Person, Personal Conversation, Conversation entries, Task, Worker binding, Attempt, Result и lifecycle. Node владеет Execution environment, Codex ACP runtime, live Worker activity и Cancel. Web client владеет только presentation и input/output transport.

Первый acceptance proof: web owner login → Secretary создаёт local Worker без Project → acknowledgement даёт ссылку → owner наблюдает Worker, направляет Steering или Stop → terminal Result verbatim появляется в Personal Conversation.

## User Stories

1. Как владелец Secretary, я хочу один раз войти в web client, чтобы открыть свою Personal Conversation.
2. Как владелец, я хочу видеть одну непрерывную Conversation после reconnect, чтобы не терять историю и Result.
3. Как владелец, я хочу отправить обычное сообщение Secretary, чтобы оно стало Steering message, если Secretary уже работает.
4. Как владелец, я хочу отправить `/q` сообщение, чтобы оно дошло до target runtime только после idle.
5. Как владелец, я хочу получить короткое acknowledgement после Dispatch, чтобы знать, что Worker действительно готов.
6. Как владелец, я хочу открыть ссылку Worker из acknowledgement, чтобы наблюдать его работу отдельно от Personal Conversation.
7. Как владелец, я хочу увидеть initial Worker status при открытии observer, чтобы понимать, active ли Worker до подключения к live stream.
8. Как владелец, я хочу получать новые Worker activity events в observer, чтобы видеть работу без broadcast raw tool output во все clients.
9. Как владелец, я хочу изменить активному Worker условие через observer, чтобы он получил Steering на ближайшей safe boundary.
10. Как владелец, я хочу отправить Worker `/q` сообщение, чтобы создать Follow-up только после idle.
11. Как владелец, я хочу остановить active Worker кнопкой Stop, чтобы прекратить неверную, опасную или ненужную работу.
12. Как владелец, я хочу видеть `stopping` до terminal canceled Result, чтобы интерфейс не утверждал отмену раньше факта.
13. Как владелец, я хочу получить Worker Result verbatim в Personal Conversation, чтобы видеть итог во всех clients без отдельного Secretary turn.
14. Как владелец, я хочу, чтобы terminal Result не закрывал Task автоматически, чтобы persistent Worker принимал Follow-up.
15. Как владелец, я хочу, чтобы Task closure останавливал active Attempt и ждал Result, чтобы Worker не продолжал менять среду после закрытия Task.
16. Как владелец, я хочу, чтобы временная ошибка Node или Codex сохраняла Task как `dispatch_failed`, чтобы моя просьба не исчезала.
17. Как владелец, я хочу, чтобы Secretary мог повторить Dispatch той же Task, чтобы failure не создавал дубликат пользовательского intent.
18. Как владелец, я хочу, чтобы повтор Telegram-style inbound event не создавал вторую Conversation entry или второй Secretary turn.
19. Как владелец, я хочу, чтобы reconnect Node не повторял active Attempt автоматически, чтобы command не был выполнен дважды.
20. Как владелец, я хочу, чтобы Worker после interrupted Attempt пытался загрузить исходную runtime session, а не незаметно начинал новую.
21. Как разработчик adapter, я хочу ordered WebSocket catch-up по последнему `entry_seq`, чтобы reconnect не создавал окно потери сообщений.
22. Как разработчик future runtime adapter, я хочу, чтобы core не зависел от Codex app-server details, чтобы добавить другой ACP runtime.
23. Как разработчик future Node, я хочу, чтобы Node сам открывал outbound connection к server, чтобы remote Node позднее работал за NAT.
24. Как пользователь с Git workflow, я хочу при необходимости попросить Worker выполнить `git clone` или создать worktree через shell, не получая Project management в базовом продукте.

## Implementation Decisions

- Go реализует Secretary server и local Node. SQLite является единственным durable store первого slice.
- Server является единственным source of truth. Он serializes Conversation entries global order и хранит inbound idempotency по `(adapter_id, external_message_id)`.
- Один configured Person владеет одной Personal Conversation. Account linking не входит в slice.
- Web setup создаёт owner bootstrap token. Web client обменивает его на HttpOnly session cookie.
- Conversation WebSocket subscription принимает последний `entry_seq`, server atomically replay-ит более новые entries и только затем начинает live delivery.
- Secretary является persistent Codex ACP session на default local Node. `secretaryctl` получает rotatable Secretary capability. Server хранит только её hash. CLI ограничен Task lifecycle operations: создать, повторить Dispatch, закрыть, показать и перечислить Task.
- Node принимает Dispatch только после создания Execution environment и готовой Codex session. Local Node живёт в `secretaryd`, но соблюдает тот же semantic contract, что future remote Node.
- Первый Execution environment даёт configured OS user full access и отдельный Workspace Worker. Sandbox остаётся future implementation того же Node boundary.
- Node запускает pinned upstream `codex-acp`. Standard ACP Cancel направляется в Codex interrupt. Negotiated `_session/steering` extension направляет active input в Codex steering. Если runtime временно non-steerable, Node хранит Steering pending до idle.
- Worker observer сначала получает current Worker status, затем live-only activity. Activity не является durable Conversation history и не broadcast-ится другим clients.
- Observer может направить Steering, Queued message и Cancel существующего Worker. Input сохраняется в Personal Conversation compact entry. Cancel останавливает только active Attempt, не закрывает Task или binding.
- Result принимается idempotently по `(worker_ref, attempt)`, verbatim добавляется в Personal Conversation и не запускает новый Secretary turn.
- Task lifecycle: `dispatching`, `dispatch_failed`, `open`, `closing`, `closed`. Attempt lifecycle: `starting`, `active`, `succeeded`, `failed`, `canceled`, `interrupted`.
- Terminal Attempt оставляет Task `open`. Task closure active Attempt сначала направляет Cancel, затем ожидает terminal Result.
- Server или Node restart переводит active Attempt в `interrupted` без automatic retry. Follow-up после этого сначала пытается загрузить прежнюю runtime session; при неудаче возвращается `runtime_session_unavailable`.
- Project registry, `create_project`, checkout lease, clone control-plane и worktree management отсутствуют. Worker может выполнить Git operation как обычную shell Task.

## Testing Decisions

- Главный automated seam: настоящий Go server, temporary SQLite и local Node с fake Codex ACP process. Тест проверяет внешнее поведение, не private methods и не SQL implementation.
- Один end-to-end test покрывает owner login, ordered Conversation sync, Dispatch acknowledgement, Worker observer, Steering, Cancel и terminal Result.
- Отдельные tests покрывают inbound deduplication, Result idempotency, `dispatch_failed`, interrupted recovery и Task closure.
- Fake ACP process должен воспроизводить session readiness, activity, steerable/non-steerable turn, Cancel и terminal Result. Он не должен имитировать reasoning модели.
- Manual smoke использует реальный `codex-acp`, local Codex login и web client. Он подтверждает ACP compatibility и live behavior, но не является частью deterministic test suite.

## Out of Scope

- AgentHub и Bridge как dependencies.
- Второй Channel adapter: Telegram, Slack, Discord или native mobile client.
- Account linking, multi-user sharing, ACLs и complex authorization.
- Remote Node implementation, TLS, health/reconnect protocol и public deployment.
- Sandbox, isolated worktree, merge/publish flow и permission approvals.
- Project registry, `create_project`, checkout lease, clone control-plane и GitHub App credential brokering.
- Postgres, Kafka, event sourcing, activity replay и queue для нескольких Workers.
- OpenCode runtime implementation.

## Further Notes

Codex ACP выбран как первый runtime не потому, что core привязан к ACP, а потому что upstream adapter уже поддерживает standard Cancel и negotiated `_session/steering`. Runtime без Steering capability остаётся совместимым через idle fallback.

Full access является осознанным ограничением первого slice. Future sandbox должен быть Node-owned Execution environment, чтобы не переписывать server, adapters, Conversation или Worker observer.
