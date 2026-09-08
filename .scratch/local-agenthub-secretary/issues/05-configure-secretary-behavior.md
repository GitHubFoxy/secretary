# Configure zero-fork Secretary behavior

Type: research
Status: resolved

## Question

Установить, где stock AgentHub хранит Coordinator instructions и managed skills. Подготовить zero-fork Secretary behavior: создание worker, короткое сообщение `Делегировано worker <имя>.`, отсутствие автоматического synthesis и сохранение worker identity для follow-up.

## Answer

Stock AgentHub разделяет prompt и managed skills.

- Default Coordinator prompt встроен в binary из `crates/agenthub-team-prompts/prompts/default_team_coordinator_prompt.txt`. При создании Team без `spec.members[].prompt` API копирует его в `teams.spec_json`.
- Для конкретного Team свой `spec.members[].prompt` сохраняется в том же `teams.spec_json` и заменяет default prompt в Team profile без fork. Живая проверка показала, что этого недостаточно для direct ACP input: даже после очистки provider session и restart Coordinator не выполнил Secretary policy только из этого поля.
- Managed Coordinator skills не являются полем Team spec. При запуске ACP AgentHub всегда materializes их из binary в `$HOME/.agents/skills/agenthub-runtime/...` и прикрепляет по role `coordinator`: `team-agents-index`, `team-coordinator-agents-index`, `team-coordinator-orchestrator`, `team-actor-mailbox`, плюс `agenthub-actor-runtime`.
- Поле `skills` из Team spec сервер удаляет при normalizing spec. Оно не отключает и не изменяет role-managed skills. UI создаёт впечатление настраиваемого списка, но для role skills source of truth находится в Rust runtime.
- Дополнительные не-reserved skills можно загрузить из `<workdir>/.agents/skills/**/SKILL.md` или общего `$HOME/.agenthub/skills.json`. Общий config применяется ко всем ACP sessions, поэтому для Secretary он хуже, чем workdir skill.

Следствие: `spec.members[].prompt` и Team skills не дают надёжного Secretary behavior для direct ACP input. Рабочая zero-fork точка для POC: `AGENTS.md` в derived Coordinator Workspace `<agent workdir>/.agenthub-team-coordinator/<actor-token>-<team-token>/AGENTS.md`. Default managed prompt требует читать этот файл при cold start.

После изменения `AGENTS.md` нужно очистить provider ACP session через `/api/agents/{id}/acp/session/clear` и вызвать `/api/teams/{team_id}/members/{member_id}/force_new_session`. После этого живой Coordinator вызвал Bridge, выдал короткий dispatch acknowledgement, направил Follow-up и вернул `not_ready` во время active Attempt.

Это prompt-level policy, не техническое ограничение. Реальная ограниченная capability появится только при будущем separated-user/container deployment.
