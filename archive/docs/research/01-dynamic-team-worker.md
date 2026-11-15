# Stock AgentHub: динамический Team worker

## Объём и версия источника

Проверен checkout `/private/tmp/agenthub` на revision `ee35c3d8e2516105cc80b8abaddea4076f064cd5`.
Вывод ниже относится к stock Coordinator/ACP flow, а не к ручному вызову HTTP API с операторским bearer-токеном и не к fork AgentHub.

## Итог

Stock AgentHub **не может выполнить весь flow из Coordinator ACP turn**: в штатном actor CLI нет операции создания агента и нет операции изменения `Team spec`/добавления member. Поэтому Coordinator не может сам создать нового local worker, добавить его в Team, а затем назначить ему task. Последние два шага существуют по отдельности после ручного добавления worker: Coordinator умеет создать task с явным assignee, а runtime умеет запустить уже существующего Team member.

При этом stock UI поддерживает ручной вариант: он создаёт `team_forge` agent через `/api/agents`, затем добавляет его `id` в `Team spec` через `/api/teams/{id}/spec`.

## Четыре проверки

| Вопрос | Ответ для Coordinator flow | Доказательство |
|---|---|---|
| 1. Создать новый persistent local worker | **Нет из Coordinator ACP flow. Да вручную через stock UI/API.** `POST /api/agents` создаёт durable запись со случайным `id` и статусом `created`; разрешение проверяется как `AgentsManage`. | `src/api/agents.rs`, `create_agent` и `require_create_agent_capability`, L270-L358, L361-L373; `AgentManager::create_agent_with_source`, L1268-L1317 @ `ee35c3d8` |
| 2. Добавить его в Team | **Нет из Coordinator ACP flow. Да вручную через stock UI/API.** Штатный путь обновляет `spec.members[]` через `PUT /api/teams/{id}/spec`; этот route требует операторский `TeamsManage` и роль owner. | `src/api/teams.rs`, `router`, `update_team_spec`, L594-L605, L768-L798; `web/src/pages/team/use_team_management_actions.ts`, `onCreateForgeAgent`, L462-L501 @ `ee35c3d8` |
| 3. Назначить task | **Да, если member уже существует в Team spec.** `team-task-create` требует `--assigned-member-id`; backend отвергает id, которого нет в `spec.members[].member_id`, и Coordinator должен быть текущим Team coordinator. | `src/actor_cli/help.rs`, `actor_usage`/`actor_topic_usage`, L62-L70, L88-L90; `src/actor_cli/parse.rs`, ветка `team-task-create`, L654-L761; `src/internal/service/rpc.rs`, `create_team_task`, L472-L524; `src/team/manager/task_catalog.rs`, L40-L58; `src/internal/service/helpers.rs`, `ensure_coordinator_team_access`, L289-L303 @ `ee35c3d8` |
| 4. Сохранить идентификатор для follow-up в ту же ACP session | **Да, но это два разных идентификатора.** Для Team/mailbox сохраняется стабильный `member_id`, который одновременно используется как `agent_id`. Для прямого `/input` нужен текущий AgentHub launch `session_id`; для восстановления ACP после restart backend отдельно хранит provider ACP id в `agent_persistent_sessions (agent_id, provider, session_id)`. | `src/team/runtime/control.rs`, запуск по `member.member_id`, L15-L60; `src/team/manager/manager_types.rs`, `TeamStepRecord::runtime_handle_id`, L461-L483; `src/agent/manager/session.rs`, `get_persistent_session`, L113-L131, новый launch id L449-L460 и сохранение ACP id L725-L734, L859-L864; `src/api/agents.rs`, `SendInputRequest`/`send_input`, L459-L500; `src/agent/manager.rs`, проверка текущего launch id, L2203-L2253 @ `ee35c3d8` |

## Почему Coordinator не может сделать первые два шага

1. Actor CLI намеренно предоставляет Team context, mailbox, task, channel/thread, step, trigger и permission-review операции. В его usage нет `agent create`, `team member add` или `team spec update`; разбор команд также начинается с этих Team/task/mailbox веток и отклоняет неизвестную подкоманду. (`src/actor_cli/help.rs:L62-L70`, `src/actor_cli/parse.rs:L485-L537`, `src/cli.rs:L22-L30 @ ee35c3d8`.)
2. Coordinator prompt разрешает ему `team-task-create` и `team-task-update`, но не добавляет capability для изменения roster. (`crates/agenthub-team-prompts/prompts/default_team_coordinator_prompt.txt:L29-L54 @ ee35c3d8`.)
3. Низкоуровневый `InternalAction::AgentManage` существует, но его нет в default permissions для Coordinator; соответствующие internal RPC `EnsureAgentRecord`/`StartManagedAgent` предназначены для managed-agent control и проверяют именно это разрешение. (`src/internal/auth.rs:L91-L118`, `src/internal/service/helpers.rs:L542-L574`, `src/internal/service/rpc.rs:L1354-L1420 @ ee35c3d8`.)
4. Реальный stock UI делает это двумя операторскими HTTP вызовами: `api.createAgent(... source: "team_forge")`, получает `created.id`, подставляет его как `member_id`, затем вызывает `api.updateTeamSpec`. (`web/src/pages/team/use_team_management_actions.ts:L462-L501 @ ee35c3d8`.)

Следовательно, ручной UI flow не является доказательством динамического Coordinator flow. Без добавления stock capability/команды или внешнего операторского orchestration слоя Secretary не может самовольно создать и зарегистрировать нового worker.

## Identifier contract для follow-up

- **Team identity:** `spec.members[].member_id`. Runtime ищет AgentHub agent именно по этому значению и запускает его через `start_agent_with_actor_context(member.member_id, ...)`. (`src/team/runtime/control.rs:L23-L60 @ ee35c3d8`.)
- **Mailbox follow-up:** отправка идёт на стабильный `to_actor_id = member_id`; actor mailbox contract называет `spec.members[].member_id` canonical route key. (`skills/team/team-actor-mailbox.SKILL.md:L34-L45 @ ee35c3d8`.)
- **Current direct-input guard:** `POST /api/agents/{id}/input` принимает optional `session_id`; менеджер сравнивает его с текущим `AgentHub` launch id и возвращает `SessionMismatch` при расхождении. (`src/api/agents.rs:L459-L500`, `src/agent/manager.rs:L2203-L2253 @ ee35c3d8`.)
- **ACP continuity:** ACP provider session id не равен launch id. При старте AgentHub сначала создаёт новый launch UUID, затем читает сохранённый provider id и передаёт его в ACP `LoadSession`; после успешного старта сохраняет полученный ACP id обратно по ключу `(agent_id, provider)`. (`src/agent/manager/session.rs:L113-L131`, `L449-L460`, `L725-L734 @ ee35c3d8`; `crates/agenthub-acp/src/lib.rs:L1561-L1625 @ ee35c3d8`.)
- **Team step follow-up:** `TeamStepRecord.runtime_handle_id` в ACP-backed реализации хранит member agent session id и именно его `maybe_nudge_reconcile_step_prompt` передаёт в `send_input` как ожидаемый session id. (`crates/agenthub-team-domain/src/lib.rs:L461-L483 @ ee35c3d8`; `src/team/reconcile_prompt.rs:L102-L148 @ ee35c3d8`.)

Практическая формула: для адресации worker сохранять `member_id`/`agent_id`; для защиты follow-up внутри текущего runtime дополнительно сохранять `session_id` из Team runtime snapshot или `runtime_handle_id`. Provider ACP id оставлять AgentHub, он уже сохраняется backend-ом и используется при restart/resume.

## Решение по тикету

Вопрос закрыт отрицательно для требуемого **одного Coordinator flow**: stock AgentHub поддерживает все нужные примитивы на операторском UI и поддерживает task assignment/follow-up после регистрации member, но не даёт Coordinator ACP штатной операции создать local agent и изменить Team roster. Fork или внешний privileged orchestration слой потребуется именно для динамического создания и добавления worker.
