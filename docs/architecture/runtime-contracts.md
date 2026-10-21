# Runtime contracts

Здесь описано текущее поведение протоколов, runtime и lifecycle Secretary. Это не ADR и не долговременная архитектурная мотивация: устойчивые решения с известными альтернативами и причинами фиксируются отдельно в ADR.

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

## Короткое подтверждение перед работой

Legacy Secretary MCP предоставляет `acknowledge_user`: Secretary вызывает его с точной server-issued парой `secretary_turn_id`/`input_id` перед рабочими инструментами. Короткое подтверждение сразу становится canonical Conversation entry; повтор с тем же текстом не создаёт запись, другой текст конфликтует. Это отдельный progress slot, который не удовлетворяет addressed final reply или native completion guard. Addressed-reply-v1 остаётся отдельным opt-in контрактом и не получает этот инструмент.

После подтверждения native assistant stream скрывается, чтобы terminal echo не появлялся повторно. Непустой final, точно совпадающий с подтверждением, не публикуется второй раз. Другой final, например ошибка Dispatch, сохраняется по прежним правилам; suppression связанных Worker Results и addressed replies остаётся в силе. Failed/canceled/interrupted terminal остаётся видимым. Короткое подтверждение не означает успешное выполнение Task.

Web получает canonical entry сразу. Telegram отправляет acknowledgement через обычную durable отправку, не ожидая Secretary terminal; event cursor и send identity защищают replay/restart. Каналы асинхронны: commit подтверждения предшествует следующему рабочему вызову модели, но tool response не ждёт сетевого Telegram receipt. Общий порядок event delivery сохраняется. Task prompt в Telegram topic показывается без добавляемого заголовка; splitting и safe formatting не меняются.

Общие правила краткого результата, сообщения о блокировке и проверки актуальных фактов хранятся во внешнем Worker profile через `profiles.worker`, а не в workspace `AGENTS.md`. Task prompt содержит суть запроса и важные пользовательские ограничения. При deploy нужно обновлять фактически настроенные внешние profile paths; embedded defaults не переписывают установленные profiles.

## Conversations и доставка сообщений

- Personal Conversation имеет общий порядок entries для подключённых Channel adapters. Server сохраняет каждую Conversation entry один раз и синхронизирует её во все adapters Person.
- Внешний Channel adapter регистрируется с server-issued credential. Web client аутентифицирует Person через owner session.
- Origin Conversation передаётся в Bridge системными metadata; модель не выбирает место доставки Result.
- Steering message сохраняется сразу и передаётся активному Secretary или Worker на ближайшей safe boundary текущего turn или Attempt. Неподдерживаемое steering, offline и неизвестное принятие возвращают явную ошибку; Node не превращает ввод в очередь, новый turn или скрытый retry. Принятие transport input само по себе не доказывает применение steering в той же Attempt.
- Queued message с prefix `/q` сохраняется сразу, но доставляется целевому runtime только в idle. Worker-first HTTP, MCP и Telegram используют общий parser `WorkerService.MessageWorker`; пустой `/q` отклоняется, prefix не входит в prompt и не отвечает pending Approval даже при request_id.
- Worker queue принадлежит Secretary server и хранит FIFO message identity независимо от Turn/Attempt. Promotion атомарно связывает message с новым Follow-up, durable command intent и прежним continuation checkpoint. Ответ mutation содержит `action_mode: queued` и `action_message_id`; observer snapshot — `queued_messages`. Direct Worker text не добавляется в model context Secretary.
- Queue states: `pending`, `delivering`, `delivered`, `blocked`, `canceled`; durable events `worker.message.queued`, `.delivered`, `.blocked`, `.canceled` адресуют opaque Worker reference. Повтор input identity сохраняет ту же запись; одинаковый текст с разными identities сохраняется отдельно.
- Принятый Node receipt для dispatch/resume обычного follow-up атомарно активирует Attempt и записывает `worker.followup.delivered` с точным `Turn.Input`; событие идемпотентно для Turn. Turn из очереди остаётся только в событиях `worker.message.*`.
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
- Worker без Project — допустимое состояние. Dispatch resolver для projectless request собирает snapshot из Node, HarnessInstance и execution policy, без Project identity, и фиктивный Project не создаётся. Канонический Secretary context принимает такой Worker: пустой `project_id` не инвалидирует snapshot, а Approval такого Worker наследует пустой Project binding от своего Worker. Project-bound Worker сохраняет строгие проверки: Project должен существовать, а сохранённый Project snapshot должен совпадать с Worker, Node и HarnessInstance. Проверки Worker, Result, Turn, Attempt и Approval не ослабляются.

## Codex native Session и terminal answer

- Codex ACP adapter публикует фазу assistant-сообщения (`commentary` или `final_answer`) в метаданных extension namespace `jetbrains.air` (`_meta.jetbrains.air.phase`) и только для клиентов, которые объявили этот namespace в `initialize` через `clientCapabilities._meta.jetbrains.air`. Внешние адаптеры предыдущих версий публиковали фазу в legacy namespace `_meta.codex.phase`.
- Runtime объявляет extension namespace только в Codex harness. Другие ACP harnesses сохраняют прежний initialize handshake и не получают Codex-специфичных метаданных.
- Terminal answer Codex принимается только у сообщения с фазой `final_answer` при `stopReason: end_turn` и без последующего tool call по тому же message id. Commentary, tool output, произвольный последний chunk и сообщение без фазы финальным ответом не считаются.
- Противоречивая metadata (фазы в разных namespace расходятся) и смена фазы внутри одного message id делают turn недействительным, а не подсказывают ответ.
- Отсутствие фазы (модель или провайдер её не публикует) даёт явную ошибку terminal answer. Эвристика по последнему сообщению не применяется.
- Assistant chunk без `messageId` не может быть финальным ответом и не попадает в authoritative Summary. Chunk без `messageId` без фазы не отменяет идентифицированный финальный ответ того же turn: это notice адаптера, а не ответ. Chunk без `messageId`, помеченный фазой `final_answer`, делает turn недействительным: два претендента на финальный ответ, один из которых неатрибутируем, не дают доказанного ответа. Два идентифицированных `final_answer` отвергаются по той же причине. Такой chunk остаётся видимым в activity и диагностических событиях. Для остальных ACP harnesses прежний terminal contract, где chunk без идентификатора запрещает ответ, не меняется.
- Тот же строгий guard применяется к Worker Result: подтверждённый terminal обязателен и там.

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
- Исключение auto-resume при подтверждённом лимите использования (Usage Limit):
  - При подтверждённом исчерпании лимита (RPC account/rateLimits/read возвращает usedPercent >= 100.0 или окно сброса resetsAt) исходная Attempt переходит в failed, Turn переходит в состояние suspended, Worker — в limited. Преждевременный Result в Personal Conversation не публикуется.
  - Следующая Attempt создаётся атомарно в состоянии scheduled с незанятым command intent resume (dedupe_key='attempt').
  - В таблице phase4_attempt_schedules создаётся запись расписания: в состоянии pending со временем resetsAt + 60s (если включена политика auto-resume и resetsAt в будущем) либо manual_hold (при отключённой политике, отсутствии resetsAt, либо блокировках spend-control / ordinaryUsageAllowed=false).
  - При достижении дедлайна или ручном вызове /resume (API POST /v1/workers/{ref}/resume или команда Telegram /resume в топике Worker) единый CAS-шлюз переводит расписание в claimed, и Node возобновляет выполнение с ContinuationPrompt ("Continue previous work from the point where it was interrupted by usage limit.").
  - Очередь FIFO (/q) сохраняется во время suspended Turn и не продвигается до завершения текущего Turn.
  - Отмена (Cancel), закрытие (Close) или новый прямой ввод отменяют невыполненное расписание и завершают suspended Turn единым Result со статусом canceled.

## Согласование queue handoff и закрытия

- Один Store сериализует promotion/claim/handoff Queued message и Close для каждого Worker. Gate общий для копий WorkerService; ожидание учитывает context, весь участок ограничен 30 секундами. Разные Workers независимы. Gate хранится до закрытия Store; при необходимости очищать историю сотен тысяч Workers можно заменить хранение на reference-counted gates.
- Закрытие prepared очереди атомарно отменяет сообщение и неclaimed command intent. Claim проверяет canceled queue, terminal Attempt и закрытый Worker; cancelled intent не может быть заново принят после crash. Already claimed delivery не повторяется автоматически после restart.
- Startup recovery сохраняет только queue-owned starting Attempt с pending command, пустыми lease/error и delivering message: этот intent ещё не проходил handoff. Claimed/active/unknown execution получает interrupted Result и остаётся без автоматического retry.
- Node атомарно сохраняет failed Dispatch/Resume receipt вместе с completed command в своей outbox. Reconnect повторяет только delivery receipt с прежними identities; ACK отправляется после server sink и освобождает outbox. Native Start/Resume повторно не вызывается.
- Authenticated dispatch/resume receipts сверяются с command/Node/Turn/Attempt bindings. Acceptance переводит starting в active; failure/interruption даёт один видимый Result и blocked queue. Поздняя transport запись не заменяет native receipt, а поздний acceptance не оживляет terminal Attempt.
- Codex/Claude/OpenCode Worker освобождает native writer до публикации terminal Outcome; новый Follow-up не начинает resume параллельно старому writer. Claude stdin write учитывает context: отмена заблокированной записи закрывает Session/process, а Close не ждёт writer mutex.

## Чтение исторических terminal events

Общий Store read boundary для `result.accepted` и `attempt.outcome_recorded` сохраняет payload со строковым status `succeeded` побайтно: обычный успешный output не очищается. Остальные payload собираются заново по разрешённой схеме. Сохраняются строковые `id`, `worker_id`, `turn_id`, `attempt_id`, `correlation_id`, валидный `created_at`, status `failed`/`canceled`/`interrupted` и classification `final`/`retryable`. Неизвестный или неверно типизированный status получает `failed`; неподдерживаемый JSON получает только этот status.

Разрешённые failure strings — `summary`, `error_code`, `error_message`, `diagnostics`, `failure_code`; они проходят общую очистку и ограничение длины, provider code — allowlist. `failure_category` и типизированный `failure` проходят общую проверку категории, provider code и ограниченных message/technical. Поля неправильного типа отбрасываются. Произвольные вложения, `artifact_refs` и display aliases `text`/`result`/`error`/`response` не входят в failure schema и отбрасываются до чтения Channel adapters. Event identity, sequence и replay cursors не меняются.

## Web still-image input (T10)

- Web/PWA accepts PNG, JPEG, and WebP originals up to 8 MiB through the owner-scoped T08 Artifact upload route. Before persistence, each image is fully decoded with a matching filename/format, at most 8192 px per side and 24 million pixels total. Invalid uploads return HTTP 422, oversized files HTTP 413; the original Artifact and its stable `artifact://` identity remain available for download.
- The owner may attach an image and a visual question in Personal Conversation or in the direct Worker observer. Web sends the Artifact reference with the caption as the canonical input; retries reuse the existing upload and message/command identities. Duplicate references in one Worker input produce exactly one native image block.
- Node fetches the original through existing authenticated T08 transport, verifies SHA-256, stages the original, validates and decodes the image again, and supplies the real bytes as native image content alongside the text question. There are at most four image blocks and sixteen distinct Artifact references in one Worker input.
- The selected HarnessInstance must advertise `image_input`. Claude Code receives stream-json user content blocks containing base64 images. ACP harnesses also require the actual native `initialize.agentCapabilities.promptCapabilities.image` (or equivalent advertised capabilities) before an image `session/prompt` or steering request; missing negotiation fails closed. No filename-only or text-only visual analysis fallback is permitted.
- Telegram photo ingestion is exclusively T11 (#83), not part of this Web slice. The native harness live-auth acceptance remains a separate deployment check if its credentials are unavailable in CI.

### T10 Web-to-Worker reliability

- A Secretary Worker action carries the image IDs from its server-owned canonical inbound Web entry, resolved through the durable Secretary turn and input identity. The model's chosen `spawn_worker.intent` or `message_worker.text` is not the source of truth for attachments; the Worker service binds those IDs to the stored action input before dispatch, so queued, continued and replayed attempts retain them. Missing or unauthorized references fail explicitly.
- Local Worker Node dispatch and steering use the same verified image preparation and image-capability checks as remote execution. The local Artifact fetcher reads original owner-scoped bytes from the Server store, without a new transport stack.
- The Web attachment control stores an unfinished upload's idempotency key against the filename, size and SHA-256 in session storage. Re-selecting the same file after a lost response reuses that key; a confirmed upload clears it. Other files and new intentional uploads receive new keys.


## Server video runtime dependency

An installed binary-release Server resolves FFmpeg and FFprobe from its own release directory, verifies the recorded pair version and SHA-256 in VIDEO-TOOLS.json, and fails the queued conversion if either executable is missing, incompatible or tampered with. A source Server can use a compatible system pair or explicit SECRETARY_FFMPEG and SECRETARY_FFPROBE paths together. The Node runtime never resolves the Server pair. The VideoJob remains a server-owned bounded asynchronous job; failed conversion retains the source Artifact and does not create false sample Artifacts. The existing 0.25–4 FPS, 1–16 frames, 1–60 seconds and 1–120-second timeout limits remain unchanged.
