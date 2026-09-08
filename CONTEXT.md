# Secretary

Система принимает пользовательские запросы, создаёт для исполнения отдельные persistent workers и сохраняет адресацию для последующего follow-up. AgentHub пока даёт runtime и control plane, но не определяет продуктовый интерфейс.

## Language

**Secretary**:
Persistent agent identity, связанная с Personal Conversation. Только Secretary решает, ответить самому, создать Task, направить Follow-up или закрыть Task.
_Avoid_: coordinator, orchestrator, manager

**Secretary server**:
Единственный source of truth для Personal Conversation, Task, Worker binding, Result и lifecycle. Он не зависит от Channel adapter или Execution node.
_Avoid_: backend, coordinator process

**Secretary capability**:
Opaque capability, ограниченная Task lifecycle operations одной persistent Secretary identity. Она меняется при создании нового Secretary runtime.
_Avoid_: server token, adapter credential

**Person**:
Владелец Personal Conversation. В первом thin slice существует один configured owner; позже Channel adapter identities связываются с ним через explicit account linking.
_Avoid_: channel account, adapter user

**Personal Conversation**:
Одна непрерывная Conversation пользователя, общая для всех его Channel adapters. История, Task и Worker bindings переходят с ним между Telegram, Discord, Slack и web.
_Avoid_: channel thread, client session, per-app chat

**Channel adapter**:
Независимый client integration для Telegram, Slack, Discord или web. Он переводит входящие сообщения и показывает ответы, но не владеет Task, Worker binding или Dispatch rules. Внешний adapter регистрируется server-issued credential; web client аутентифицирует Person через owner session.
_Avoid_: UI, frontend, extension

**Execution node**:
Host, который запускает Worker по Dispatch от Secretary server и сам держит outbound connection к нему. Он не владеет Conversation или Result routing; в первом slice default node является local Secretary host.
_Avoid_: client, server replica, remote Worker

**Execution environment**:
Node-owned рабочая среда Worker: доступные файлы, capabilities и lifecycle. Первый вариант даёт full access; sandbox и isolated worktree могут стать другими вариантами.
_Avoid_: Execution node, Project checkout

**Node enrollment**:
Явное owner-approved pairing Execution node с Secretary server через одноразовый code. После pairing Node получает собственную identity.
_Avoid_: shared secret, automatic discovery

**Bridge**:
Одноразовый local POC adapter между Secretary и AgentHub. В первом vertical slice он работает как trusted-local component на текущем Unix user и принимает только ограниченные операции Secretary.
_Avoid_: product service, agent, worker, backend

**Trusted-local prototype**:
Первый deployment, в котором Bridge, Secretary и Worker используют текущий Unix user. Ограничение операций является contract, а не технической security boundary.
_Avoid_: sandbox, isolated deployment

**Bridge CLI**:
Локальная command-line interface Bridge для Secretary, Worker и operator действий. Она не является user-facing API.
_Avoid_: bridge server, public API

**Bridge operator**:
Отдельный AgentHub user с минимальными capabilities, чья обновляемая session используется только Bridge.
_Avoid_: root account, service token

**Conversation entry**:
Одна persist-строка Personal Conversation с global order: пользовательское сообщение, Secretary reply или Worker Result. Server сохраняет её один раз и синхронизирует во все Channel adapters Person.
_Avoid_: channel message, client event, chat bubble

**Steering message**:
Обычное входящее сообщение, направленное в active Secretary или Worker. Server сохраняет его сразу и передаёт target runtime при ближайшей safe boundary текущего turn или Attempt. Если runtime временно не steerable, Node доставляет его после idle.
_Avoid_: follow-up, interrupt

**Queued message**:
Сообщение с prefix `/q`. Оно сохраняется сразу, но передаётся target runtime только когда тот idle.
_Avoid_: steering, delayed send

**Task**:
Единица пользовательской работы, которой владеет один Worker до явного закрытия.
_Avoid_: request, run

**Worker**:
Persistent agent, созданный для исполнения одного Task и сохранённый до явного закрытия Task. Первый Worker runtime - Codex по ACP; другие ACP runtimes, включая OpenCode, добавляются adapter-ами.
_Avoid_: subagent, child agent

**Compaction**:
Сжатие active model context, которым владеет runtime harness для своей Agent session. Оно не удаляет durable Conversation entries, Task или Worker binding в Secretary server.
_Avoid_: history deletion, server cleanup

**Workspace**:
Отдельный пустой working directory Worker при запуске Task. Worker может сам создать или выбрать другой local path через shell.
_Avoid_: Project checkout, shared directory

**Worker template**:
Единый профиль Worker первого slice: Codex ACP на Linux с full access, provider-default model и отдельным Workspace.
_Avoid_: category, custom profile

**Dispatch**:
Создание Worker, передача ему Task envelope и возврат Worker binding через Bridge. Он считается accepted только после сохранения binding и передачи Task в runtime.
_Avoid_: assignment, launch

**Task envelope**:
Системные данные Dispatch для Worker: Task text, Worker reference и одноразовый Callback capability с правилом terminal Result callback.
_Avoid_: user prompt, free-form context

**Origin Conversation**:
Conversation, в которую Secretary обязан вернуть Result. Она передаётся в Bridge системным metadata, а не выбирается моделью.
_Avoid_: reply text, destination prompt

**Worker reference**:
Opaque публичный идентификатор Worker, который видят Secretary и пользователь. Он не является AgentHub `agent_id` или session ID.
_Avoid_: agent ID, session ID

**Worker observer**:
Opt-in client view Worker, открываемый по Worker reference. Он получает initial Worker status, затем показывает live Worker activity и позволяет направить Worker Steering message, Queued message или Cancel.
_Avoid_: Secretary conversation, global log

**Worker activity**:
Ephemeral text, tool и status events active Worker, передаваемые только открывшим Worker observer. Terminal Result не является Worker activity.
_Avoid_: Result, durable conversation history

**Worker binding**:
Сохранённая связь Task с Worker reference, Execution node и внутренними runtime identifiers, нужная для Follow-up и получения Result.
_Avoid_: session mapping, task mapping

**Attempt**:
Один execution cycle Worker для Task или Follow-up, завершающийся одним Result. Restart Secretary server или Execution node не повторяет active Attempt автоматически: он становится `interrupted`, а Task и Worker binding сохраняются.
_Avoid_: run, task

**Follow-up**:
Queued message, направленное в существующую idle Worker session по Worker binding. Оно создаёт новую Attempt. После `interrupted` Node обязан сначала загрузить сохранённую runtime session; при её отсутствии он возвращает `runtime_session_unavailable`, а не создаёт silent новую session.
_Avoid_: steering, retry, new task

**Cancel**:
Явное действие owner из Worker observer, которое best-effort останавливает active Attempt. Оно не закрывает Task или Worker binding, поэтому Worker может принять Follow-up позже.
_Avoid_: Task closure, deletion

**Result**:
Terminal report Worker, verbatim добавляемый server в Personal Conversation без automatic Secretary turn. Его status: `succeeded`, `failed` или `canceled`; summary обязателен, artifact references опциональны. Server принимает Result idempotently по Worker reference и Attempt.
_Avoid_: output, completion message

**Result callback**:
Private сообщение Worker в Bridge о terminal status, summary и artifact references, из которого формируется Result.
_Avoid_: event polling, user message

**Callback capability**:
Одноразовый token, который Bridge выдаёт Worker для Result callback и связывает с одним Worker binding.
_Avoid_: operator credential, shared API key

**Dispatch failure**:
Terminal failure до accepted Dispatch. Task сохраняется со status `dispatch_failed` в Personal Conversation, но Worker binding не создаётся и server не делает automatic retry.
_Avoid_: retry, partial dispatch

**Task closure**:
Явное действие Secretary, которое закрывает Task и архивирует Worker binding, сохраняя историю. Для active Attempt оно сначала отправляет Cancel и ждёт terminal Result.
_Avoid_: automatic expiry, deletion

**Coordinator profile**:
POC Secretary policy в `AGENTS.md` derived Coordinator Workspace. Per-Team `spec.members[].prompt` хранит профиль, но не меняет direct ACP input сам по себе; после изменения `AGENTS.md` нужен новый provider ACP session.
_Avoid_: Secretary runtime, capability boundary

**Role-managed skills**:
Навыки, которые AgentHub прикрепляет ACP session по Team role из собственной runtime logic. Team spec не может их изменить или отключить.
_Avoid_: Team skills configuration, Secretary tools

**AgentHub**:
Временный POC control plane и runtime для Workers. После proof его implementation не является частью целевой архитектуры, но измеренный contract может сохраниться.
_Avoid_: Secretary, product UI, permanent dependency
