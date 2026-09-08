# Route Worker Result to the Secretary conversation

Type: task
Status: resolved

## Question

Подключить terminal Result из Bridge state к исходной Secretary conversation в test Team.

## Acceptance criteria

- Dispatch сохраняет системный Origin Conversation reference.
- `report-result` доставляет status и summary в тот же Coordinator session через AgentHub HTTP API.
- Coordinator profile публикует Worker Result без самостоятельного synthesis или нового dispatch.
- Повторный callback не создаёт второй user-visible Result.
- Механизм ограничен test Team и disposable POC adapter.

## Answer

Dispatch теперь сохраняет Origin Conversation как Coordinator `agent_id` и текущий AgentHub `session_id` в Worker binding. `report-result` после сохранения terminal Result отправляет в эту session `Secretary Bridge Result` через `POST /api/agents/{id}/input` с сохранённым `session_id`.

Coordinator `AGENTS.md` требует опубликовать такой Result verbatim. Живой Task `sleep 1` создал Worker `wkr_59360b9e7d04`, вернул `succeeded: sleep 1 completed successfully`, а Coordinator опубликовал: `Worker wkr_59360b9e7d04: succeeded. sleep 1 completed successfully`.

Binding содержит `result_delivered_at`. Повторный callback блокируется до HTTP delivery, потому что его Attempt уже terminal, поэтому второй user-visible Result не создаётся.
