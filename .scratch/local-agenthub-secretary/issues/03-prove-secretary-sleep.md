# Prove the Secretary sleep flow

Type: prototype
Status: resolved

## Question

На живой установке проверить flow: MacBook отправляет Secretary `sleep 60`; Secretary создаёт нового local worker на Linux laptop, немедленно отвечает, что задача делегирована; worker завершает работу; затем MacBook отправляет follow-up тому же worker.

## Findings so far

Прототип выполнил инфраструктурную часть, но не целевой dynamic-worker flow.

- MacBook открыл AgentHub на Linux laptop по LAN. Codex ACP запустился после официального login.
- Созданные вручную Coordinator и `sleeper` работали в persistent ACP sessions.
- Coordinator получил прямой запрос и сразу подтвердил dispatch. `sleeper` выполнил `sleep 15` за 14.9 секунды с exit status `0`.
- Follow-up был отправлен в ту же worker session и вернул `sleep 15` exited successfully with status `0`.
- Во время проверки включён внутренний gRPC mailbox на `127.0.0.1:50051`. Без него `agenthub actor inbox/receive` не работают.

Целевой acceptance test не пройден: worker был создан вручную, а не Secretary; использовался `sleep 15`, не `sleep 60`; stock flow не вернул completion в Coordinator/user conversation надёжно. Это подтверждает решение из `01`: для нужного flow обязателен отдельный privileged Secretary bridge.

## Answer

Проверка выполнена из авторизованной Helium session на MacBook через LAN URL `http://192.168.0.16:8080`.

1. MacBook отправил Secretary `Run sleep 60, then report completion.`
2. Secretary немедленно ответил `Делегировано worker wkr_c356a1185a6b.`
3. Новый Worker выполнил `sleep 60` и Bridge вернул в ту же Coordinator session: `Worker wkr_c356a1185a6b: succeeded. sleep 60 completed`.
4. MacBook отправил Follow-up этому `worker_ref`: `run sleep 15, then report completion.`
5. Secretary ответил `Follow-up отправлен worker wkr_c356a1185a6b.`
6. Тот же Worker вернул второй Result: `Worker wkr_c356a1185a6b: succeeded. sleep 15 completed`.

Это первая проверка, в которой Worker создан Secretary dynamically через Bridge, а не вручную. Worker binding, Result routing и Follow-up используют один persistent Worker и одну Coordinator session.
