# Apply the zero-fork Secretary Coordinator profile

Type: task
Status: resolved

## Question

На отдельной test Team настроить per-Team Coordinator prompt для Bridge vertical slice без fork AgentHub.

## Acceptance criteria

- `spec.members[].prompt` Coordinator содержит согласованную Secretary policy.
- Новый Task вызывает только `secretary-bridge delegate` и после accepted возвращает `Делегировано worker <worker_ref>.`.
- Coordinator не создаёт Team worker, не исполняет Task сам и не синтезирует Worker Result.
- Явно адресованный Follow-up вызывает только `secretary-bridge send-follow-up`.
- Ошибки Bridge и `not_ready` возвращаются коротко, без automatic retry.
- Изменение ограничено test Team и не меняет upstream prompt или managed skills.

## Answer

Policy сохранён в [`poc/secretary_coordinator_AGENTS.md`](../../../poc/secretary_coordinator_AGENTS.md) и установлен в derived Coordinator Workspace test Team. Per-Team `spec.members[].prompt` тоже обновлён, но сам по себе не влияет на direct ACP input. Чтобы Coordinator прочитал новый `AGENTS.md`, Bridge очистил provider ACP session и выполнил `force_new_session`.

После restart запрос `Run sleep 1` создал новый Worker `wkr_4bc8ef42d690` через `secretary-bridge delegate`. Coordinator ответил ровно `Делегировано worker wkr_4bc8ef42d690.` и Worker вернул Result callback.

Follow-up этому `worker_ref` был направлен тем же Coordinator через `secretary-bridge send-follow-up`, завершил вторую Attempt и дал ответ `Follow-up отправлен worker wkr_4bc8ef42d690.`

Для Worker с active `sleep 30` Coordinator получил явно адресованный Follow-up и ответил `not_ready: worker wkr_68d230663ea6 не готов принять follow-up.` Он не создал очередь и не выполнил Task самостоятельно.
