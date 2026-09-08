# Implement the disposable Secretary Bridge POC

Type: task
Status: resolved

## Question

Реализовать минимальный disposable local Bridge CLI без fork AgentHub и подтвердить его на одной test Team. Это измерительный adapter, не начало production service.

## Acceptance criteria

- Secretary вызывает только `delegate(task)` и `send_follow_up(worker_ref, text)` как prompt-level policy.
- Bridge через AgentHub HTTP API создаёт Worker с отдельным Workspace, добавляет его в Team, запускает и сохраняет минимальный Worker binding в локальном state store.
- Bridge не пишет напрямую в AgentHub SQLite.
- Worker получает Task envelope и возвращает terminal Result через Callback capability.
- Follow-up попадает в ту же Worker session; при active Attempt возвращается `not_ready`.
- Для POC допустим вручную supplied short-lived AgentHub token. Не реализовывать `bridge-operator`, automatic renewal, public API, security hardening, retention policy или Task closure.

## Comments

- Реализован `poc/secretary_bridge.py` и развёрнут на Linux как `/home/coder/.local/bin/secretary-bridge`.
- CLI прошёл локальные проверки `py_compile`, одноразового Result callback и `not_ready` для active Attempt.
- Для live test пользователь явно разрешил перенести short-lived AgentHub session из отдельного Helium в `/home/coder/.config/secretary-bridge-poc/env`. Codex credential не затрагивался.

## Answer

Bridge POC реализован в [`poc/secretary_bridge.py`](../../../poc/secretary_bridge.py) и развёрнут на Linux как `/home/coder/.local/bin/secretary-bridge`.

На Team `Secretary v2` `delegate` создал AgentHub Worker `166e435a-7fe9-46ed-87c4-6677ac6880af` с отдельным Workspace и вернул `wkr_391d302ab471` со status `accepted`. Worker получил Task envelope, выполнил `sleep 60` и записал Result callback `succeeded: sleep 60 completed successfully`.

`send-follow-up` направил второй Task тому же Worker. Во время active Attempt Bridge вернул `not_ready`; затем Worker выполнил `sleep 15` и записал второй Result `succeeded: sleep 15 completed successfully`. AgentHub events обоих Attempt имеют один launch session ID `c7e7639b-3577-4189-96e4-5ccf6f01215c`.

Bridge использовал только AgentHub HTTP API. Direct SQLite access не использовался. Result пока сохраняется в local state; возврат в Secretary conversation вынесен в `Route Worker Result to the Secretary conversation`.
