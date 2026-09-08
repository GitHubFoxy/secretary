# Confirm stock dynamic Team-worker creation

Type: research
Status: resolved

## Question

Может ли stock AgentHub без fork создать нового persistent local agent, добавить его в Team и назначить ему новый task из Coordinator flow? Нужно также установить, какой identifier сохраняется для follow-up в ту же ACP session.

## Answer

Исследование завершено: [отчёт](../../../docs/research/01-dynamic-team-worker.md), источники проверены на AgentHub `ee35c3d8`.

- **Создать worker из Coordinator ACP flow:** нет. Stock UI/API умеет `POST /api/agents`, но actor CLI не содержит agent-create или Team-roster операции (`/private/tmp/agenthub/src/api/agents.rs:L270-L358`; `src/actor_cli/help.rs:L62-L70`).
- **Добавить в Team:** нет из Coordinator flow; ручной UI делает `PUT /api/teams/{id}/spec` после создания агента (`src/api/teams.rs:L768-L798`; `web/src/pages/team/use_team_management_actions.ts:L462-L501`).
- **Назначить task:** да после появления member, через `team-task-create --assigned-member-id`; backend требует этот `member_id` в `spec.members[]` (`src/team/manager/task_catalog.rs:L40-L58`).
- **Follow-up:** сохранять `member_id`/`agent_id` для адресации и текущий AgentHub launch `session_id`/`runtime_handle_id` для проверки того же runtime. ACP provider id AgentHub отдельно сохраняет в `agent_persistent_sessions` для resume (`src/agent/manager/session.rs:L113-L131`, `L449-L460`, `L725-L734`).

Требуемый единый динамический Coordinator flow stock AgentHub не поддерживает без внешнего privileged orchestration слоя или fork.
