# Secretary

Secretary ведёт одну Personal Conversation и при необходимости делегирует Task отдельному Worker. Подключённые каналы дают доступ к этой общей переписке, а не создают отдельные копии Secretary.

## Language

**Secretary**:
Постоянная идентичность Secretary, связанная с Personal Conversation.
_Avoid_: coordinator, orchestrator, manager

**Secretary server**:
Авторитетная система, которая владеет Personal Conversation, Task, Worker binding, Result и их lifecycle.
_Avoid_: backend, coordinator process

**Secretary capability**:
Ограниченное право выполнять lifecycle operations одной persistent Secretary identity.
_Avoid_: server token, adapter credential

**Secretary input identity**:
Идентификатор конкретного пользовательского ввода внутри Secretary turn.
_Avoid_: ACP message ID, inferred request ID

**Addressed reply**:
Ответ Secretary, связанный с конкретными Secretary turn и input identity.
_Avoid_: untyped ACP Result, guessed echo

**Worker origin link**:
Durable связь принятого Worker action с соответствующими Worker Turn и canonical Result.
_Avoid_: inferred task relation, text match

**Addressed reply v1**:
Версия контракта Addressed reply с явной адресацией пользовательских ответов и Worker actions.
_Avoid_: implicit response classifier, global echo filter

**Person**:
Владелец Personal Conversation.
_Avoid_: channel account, adapter user

**Personal Conversation**:
Одна непрерывная переписка пользователя, общая для подключённых Channel adapters; связанная с ней история, Task и Worker bindings не зависят от канала.
_Avoid_: channel thread, client session, per-app chat

**Channel adapter**:
Client integration, которая принимает сообщения из канала и показывает ответы Secretary и Results.
_Avoid_: UI, frontend, extension

**Execution node**:
Host, назначенный запускать Workers для Secretary.
_Avoid_: client, server replica, remote Worker

**Execution environment**:
Рабочая среда, в которой Worker выполняет Task: доступные файлы и capabilities.
_Avoid_: Execution node, Project checkout

**Node enrollment**:
Owner-approved регистрация Execution node в Secretary server.
_Avoid_: shared secret, automatic discovery

**Conversation entry**:
Упорядоченная запись Personal Conversation: пользовательское сообщение, ответ Secretary или Worker Result.
_Avoid_: channel message, client event, chat bubble

**Steering message**:
Сообщение для активного Secretary или Worker, которое направляет текущую работу.
_Avoid_: follow-up, interrupt

**Queued message**:
Сообщение, ожидающее возможности быть обработанным целевым runtime.
_Avoid_: steering, delayed send

**Task**:
Единица пользовательской работы, делегированная одному Worker до явного закрытия.
_Avoid_: request, run

**Worker**:
Persistent agent, выполняющий Task и остающийся связанным с ним до явного закрытия.
_Avoid_: subagent, child agent

**Compaction**:
Сжатие активного model context; оно не является удалением durable истории Conversation или Task.
_Avoid_: history deletion, server cleanup

**Workspace**:
Рабочая директория, используемая Worker при выполнении Task.
_Avoid_: Project checkout, shared directory

**Dispatch**:
Передача Task Worker для исполнения.
_Avoid_: assignment, launch

**Origin Conversation**:
Personal Conversation, в которую возвращается Worker Result.
_Avoid_: reply text, destination prompt

**Worker reference**:
Opaque публичный идентификатор Worker.
_Avoid_: agent ID, session ID

**Worker observer**:
Отдельный client view одного Worker для просмотра состояния и активности и управления работой.
_Avoid_: Secretary conversation, global log

**Worker activity**:
Временные text, tool и status events активного Worker.
_Avoid_: Result, durable conversation history

**Worker binding**:
Сохранённая связь Task с Worker, нужная для последующей адресации работы.
_Avoid_: session mapping, task mapping

**Approval**:
Durable server-owned запись запроса Worker или harness на разрешение либо пользовательский ввод. Связана с конкретным Worker, execution turn и Attempt, проходит через Execution node и Secretary server к владельцу и хранит request и lifecycle/decision state.
_Avoid_: `needs_input` event, неявное разрешение, permission grant

**Attempt**:
Один execution cycle Worker для Task или Follow-up, завершающийся Result.
_Avoid_: run, task

**Follow-up**:
Новое указание существующему Worker для продолжения того же Task.
_Avoid_: steering, retry, new task

**Cancel**:
Явный запрос остановить active Attempt.
_Avoid_: Task closure, deletion

**Result**:
Terminal report Worker по одной Attempt, возвращаемый в Origin Conversation.
_Avoid_: output, completion message

**Dispatch failure**:
Сбой передачи Task Worker до принятого Dispatch.
_Avoid_: retry, partial dispatch

**Task closure**:
Явное завершение Task с архивированием его Worker binding.
_Avoid_: automatic expiry, deletion
