# Runtime contracts

Здесь описано текущее поведение протоколов, runtime и lifecycle Secretary V2. Это не ADR и не долговременная архитектурная мотивация: устойчивые решения с известными альтернативами и причинами фиксируются отдельно в ADR.

## Authority, owner и identity

- Secretary server — единственный source of truth для Personal Conversation, Task, Worker binding, Result и lifecycle. Он не зависит от Channel adapter или Execution node.
- Channel adapters показывают переписку, но не владеют Task, Worker binding или Dispatch rules.
- Только Secretary решает, ответить самому, создать Task, направить Follow-up или закрыть Task.
- Secretary capability ограничена Task lifecycle operations одной persistent Secretary identity и меняется при создании нового Secretary runtime.
- Каждый пользовательский ввод внутри persistent Secretary turn адресуется server-issued `input_id`. ACP message metadata, native tool names и сходство текста не определяют input identity.
- Addressed reply сохраняется idempotently для точной пары Secretary turn и input identity. Повтор с той же identity и тем же текстом возвращает ту же запись; другой текст для той же identity конфликтует.
- Worker origin link связывает явный принятый server-owned Worker action с точной Worker Turn и её canonical Result. Link строится по server-issued turn/input identity и lifecycle command, а не по содержимому сообщений.
- Addressed reply v1 — additive opt-in contract с отдельной операцией `reply_to_user` и обязательными origin identities для Worker actions. По умолчанию он выключен, сохраняя legacy behavior; внешние Profiles автоматически не переписываются.
- В первом thin slice есть один configured owner. Позже identities разных Channel adapters связываются с Person через явный account linking.

## Conversations и доставка сообщений

- Personal Conversation имеет общий порядок entries для подключённых Channel adapters. Server сохраняет каждую Conversation entry один раз и синхронизирует её во все adapters Person.
- Внешний Channel adapter регистрируется с server-issued credential. Web client аутентифицирует Person через owner session.
- Origin Conversation передаётся в Bridge системными metadata; модель не выбирает место доставки Result.
- Steering message сохраняется сразу и передаётся активному Secretary или Worker на ближайшей safe boundary текущего turn или Attempt. Неподдерживаемое steering, offline и неизвестное принятие возвращают явную ошибку; Node не превращает ввод в очередь, новый turn или скрытый retry. Принятие transport input само по себе не доказывает применение steering в той же Attempt.
- Queued message с prefix `/q` сохраняется сразу, но доставляется целевому runtime только в idle. Worker-first HTTP, MCP и Telegram используют общий parser `WorkerService.MessageWorker`; пустой `/q` отклоняется, prefix не входит в prompt и не отвечает pending Approval даже при request_id.
- Worker queue принадлежит Secretary server и хранит FIFO message identity независимо от Turn/Attempt. Promotion атомарно связывает message с новым Follow-up, durable command intent и прежним continuation checkpoint. Ответ mutation содержит `action_mode: queued` и `action_message_id`; observer snapshot — `queued_messages`. Direct Worker text не добавляется в model context Secretary.
- Queue states: `pending`, `delivering`, `delivered`, `blocked`, `canceled`; durable events `worker.message.queued`, `.delivered`, `.blocked`, `.canceled` адресуют opaque Worker reference. Повтор input identity сохраняет ту же запись; одинаковый текст с разными identities сохраняется отдельно.
- После restart подготовленная перед handoff delivery восстанавливается по тому же command intent. Active/terminal Attempt и неизвестное принятие не повторяются; queue остаётся `blocked` с видимой ошибкой. Interrupted binding использует прежний resume contract. Close отменяет накопленные сообщения до остановки активной Attempt; Cancel сохраняет binding и разрешает следующий queued Follow-up после terminal Result.

## Execution node и окружение

- Execution node запускает Workers по Dispatch от Secretary server и поддерживает outbound connection к серверу. Node не владеет Conversation или Result routing; в первом slice default node — local Secretary host.
- Node enrollment происходит с явного owner-approved pairing через одноразовый code; после pairing Node получает собственную identity. Envelope HMAC подписывает payload после `json.Marshal(RawMessage)`, то есть в compact/HTML-escaped форме wire serializer. Форматирование durable JSON при reopen не меняет аутентификацию; пробелы внутри строк, порядок ключей, значения и identity остаются частью подписи. Verify сохраняет совместимость со старой raw-byte подписью только для валидного Envelope/JSON и действительного per-Node HMAC.
- Первый Execution environment даёт Worker full access. Sandbox и isolated worktree могут стать другими вариантами; full access сам по себе не является sandbox или гарантией изоляции.
- Worker получает отдельный пустой Workspace при запуске Task. Через shell Worker может создать или выбрать другой local path.

## Локальный POC и AgentHub

- Bridge — одноразовый local POC adapter между Secretary и AgentHub. В первом vertical slice Secretary, Bridge и Worker работают под текущим Unix user: это trusted-local prototype. Bridge принимает только ограниченные операции Secretary, но это ограничение не является технической security boundary.
- Bridge CLI обслуживает локальные действия Secretary, Worker и operator; это не user-facing API.
- Bridge operator — отдельный AgentHub user с минимальными capabilities. Его обновляемая session используется только Bridge.
- AgentHub — временный POC control plane и Worker runtime, а не часть целевой архитектуры или определения product interface. Измеренный на нём contract может сохраниться после замены implementation.
- Coordinator profile — POC Secretary policy в `AGENTS.md`, полученная из Coordinator Workspace. `spec.members[].prompt` хранит профиль Team, но сам по себе не меняет direct ACP input. После изменения `AGENTS.md` нужен новый provider ACP session.
- Role-managed skills прикрепляются AgentHub к ACP session по Team role. Team spec не может изменить или отключить эти skills.

## Worker profile и Dispatch

- В Minimal MVP default Secretary и новых Workers — Codex через native `codex-acp`; Claude Code выбирается через существующий harness policy. Workers на host Secretary и удалённой машине используют durable `secretary-node`. Исторические OpenCode/FX adapters и bindings сохраняются.
- Compaction активного model context принадлежит runtime harness; она не удаляет durable Conversation entries, Task или Worker binding на Secretary server.
- Secretary и новый Worker используют model/reasoning `default` своего native harness и отдельный Workspace; OpenCode pins не наследуются Codex или Claude Code. Явные preferences и Project pins имеют приоритет; существующие Worker bindings не мигрируют при смене defaults.
- Dispatch передаёт Worker Task envelope с Task text, Worker reference и одноразовым Callback capability для terminal Result callback.
- Dispatch считается accepted только после сохранения Worker binding и передачи Task в runtime.
- Worker reference — opaque публичный идентификатор; он не является AgentHub `agent_id` или session ID. Worker binding сохраняет связь Task с Worker reference, Execution node и внутренними runtime identifiers.
- Worker observer — opt-in client view по Worker reference; он сначала получает состояние Worker, затем live Worker activity. Из observer можно направить Steering message, Queued message или Cancel.
- ACP tool identity берётся из явного native `name` либо из MCP provenance `_meta.is_mcp_tool_call` и semantic `rawInput.server/tool`. MCP arguments берутся из вложенного `rawInput.arguments`; sparse update связывается по прежнему `toolCallId`. Display title не определяет tool identity и не попадает в normalized telemetry. Completed native tool_call даёт один tool_result без выдуманного start; duplicate и ambiguous idless updates не создают новые tool events.
- Worker activity состоит из ephemeral text, tool и status events. Эти события получает только открывший Worker observer; terminal Result не является Worker activity. После terminal Outcome Node прекращает публикацию activity этой Attempt. Server проверяет payload и immutable Node/HarnessInstance/Worker/Turn/Attempt binding в одной транзакции с terminal state; поздний replay корректной завершённой Attempt получает ACK без нового event, Approval или изменения Result. Неизвестная Attempt и неверная identity отклоняются.

## Claude Code native Session

- Claude Code использует persistent JSONL stream input/output. Deferred Start не вызывает модель; Prompt принимает только idle Session. Secretary сохраняет process, Worker закрывает его после terminal Attempt и возобновляет прежний native transcript через `--resume`.
- Profile/skills передаются в native system prompt, managed tools — в native allowlist. MCP config задан явно; секретные environment values не попадают в argv. Необслуживаемые native approvals отключены через dontAsk.
- Resume требует initialize до Prompt; missing session и другая native identity дают явную ошибку. Error/empty terminal не считается успехом. Cancel отправляет interrupt; receipt подтверждает только запрос отмены и не завершает Attempt. Native terminal определяет Result; Close завершает собственную process group, закрывает stdin/stdout и ждёт освобождения native writer не более пяти секунд. Копирование stderr ограничено `Cmd.WaitDelay` в 250 мс; ошибка завершения не превращается в успешный Outcome. Partial и complete events не дублируют ответ.
- CapabilitySteering для Claude не объявляется до live same-Attempt evidence. Обычный active input получает явный unsupported; silent queue и interrupt + новый turn не используются как замена. Текущая реальная приёмка заблокирована quota403 настроенного provider и отсутствием working first-party auth; [свидетельства](../../.scratch/phase-5/reports/claude-interactive-runtime-20261010.md).

## Attempt, Result и закрытие Task

- Attempt — один execution cycle Worker для Task или Follow-up, заканчивающийся одним Result.
- Restart Secretary server или Execution node не запускает active Attempt повторно автоматически. Attempt становится `interrupted`, а Task и Worker binding сохраняются.
- Follow-up адресуется существующей idle Worker session через Worker binding и создаёт новую Attempt. После `interrupted` Node сначала загружает сохранённую runtime session. Если её нет, Node возвращает `runtime_session_unavailable`, не создавая незаметно новую session.
- Cancel от owner из Worker observer best-effort останавливает active Attempt, но не закрывает Task или Worker binding. Worker может принять Follow-up только после native terminal Result, а не после cancellation receipt. Cancel до доказанного отсутствующего handoff prepared queue Attempt завершается сервером без runtime выполнения.
- Result — terminal report Worker, который server verbatim добавляет в Personal Conversation без automatic Secretary turn. Допустимые status: `succeeded`, `failed` и `canceled`; summary обязателен, artifact references опциональны. Server принимает Result idempotently по Worker reference и Attempt.
- Result callback — private сообщение Worker в Bridge с terminal status, summary и artifact references, на основе которого формируется Result.
- Callback capability — одноразовый token, выдаваемый Bridge Worker для Result callback и связанный с одним Worker binding.
- Dispatch failure до accepted Dispatch сохраняет Task со status `dispatch_failed` в Personal Conversation, но не создаёт Worker binding и не запускает automatic retry.
- Task closure явно закрывает Task и архивирует Worker binding, сохраняя историю. Для active Attempt Secretary сначала отправляет Cancel и ждёт terminal Result.

## Согласование queue handoff и закрытия

- Один Store сериализует promotion/claim/handoff Queued message и Close для каждого Worker. Gate общий для копий WorkerService; ожидание учитывает context, весь участок ограничен 30 секундами. Разные Workers независимы. Gate хранится до закрытия Store; при необходимости очищать историю сотен тысяч Workers можно заменить хранение на reference-counted gates.
- Закрытие prepared очереди атомарно отменяет сообщение и неclaimed command intent. Claim проверяет canceled queue, terminal Attempt и закрытый Worker; cancelled intent не может быть заново принят после crash. Already claimed delivery не повторяется автоматически после restart.
- Startup recovery сохраняет только queue-owned starting Attempt с pending command, пустыми lease/error и delivering message: этот intent ещё не проходил handoff. Claimed/active/unknown execution получает interrupted Result и остаётся без автоматического retry.
- Node атомарно сохраняет failed Dispatch/Resume receipt вместе с completed command в своей outbox. Reconnect повторяет только delivery receipt с прежними identities; ACK отправляется после server sink и освобождает outbox. Native Start/Resume повторно не вызывается.
- Authenticated dispatch/resume receipts сверяются с command/Node/Turn/Attempt bindings. Acceptance переводит starting в active; failure/interruption даёт один видимый Result и blocked queue. Поздняя transport запись не заменяет native receipt, а поздний acceptance не оживляет terminal Attempt.
- Codex/Claude/OpenCode Worker освобождает native writer до публикации terminal Outcome; новый Follow-up не начинает resume параллельно старому writer. Claude stdin write учитывает context: отмена заблокированной записи закрывает Session/process, а Close не ждёт writer mutex.
