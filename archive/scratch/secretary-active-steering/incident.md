# Инцидент: уточнение в General стало отложенным Follow-up

Дата инцидента: 2026-10-10. Дата подготовки: 2026-10-11.

Это сокращённая запись read-only диагностики предыдущего шага, а не новая production проверка. Источники: SQLite `conversation_entries`, `secretary_turns`, `events`, `turns`; нативные Codex JSONL на omarchy и Mac. Credentials, полная история и reasoning не сохранены. Время UTC; для времени на скриншоте прибавить семь часов.

## Ввод

1. `How do i do screencast on mac? Is there a shortcut?`
2. Через 8,9 секунды: `i meant record a video`.

Пользователь ожидал Steering message для текущей работы. Получил первый Result, затем подтверждение уточнения и второй Result.

## Подтверждённая трасса

| UTC | Источник | Событие |
| --- | --- | --- |
| 15:39:26.339272124 | Conversation entry | Первый вопрос сохранён. |
| 15:39:26.365917557 | Secretary turn | Первый turn активен. |
| 15:39:28.614 | Codex Secretary user message | Первый вопрос передан в canonical prompt. |
| 15:39:35.227249270 | Conversation entry | Уточнение сохранено. |
| 15:39:35.248448912 | Event 10372, `secretary.turn.queued` | Создан отдельный queued turn для уточнения. |
| 15:40:00.951908003 | Event 10382, `secretary.tool_call` | `spawn_worker` содержит только первый вопрос. |
| 15:40:11.907636899 | Event 10384, `worker.spawned` | Worker создан после получения уточнения сервером. |
| 15:40:14.934 | Codex Worker user message | Точный prompt: первый вопрос. |
| 15:40:17.842 | Codex Secretary `task_complete` | Первый native turn завершён. |
| 15:40:17.865633705 | Event 10391, `secretary.turn.started` | Начат отдельный Secretary turn уточнения. |
| 15:40:17.917 | Codex Secretary user message | Canonical prompt теперь содержит уточнение. |
| 15:40:21.167 | Codex Worker `task_complete` | Первое выполнение завершено. |
| 15:40:22.453686244 | Event 10506, `result.accepted` | Сервер принял первый Result. |
| 15:40:24.067696148 | Event 10508, `secretary.tool_call` | Подтверждение «Понял, речь о записи видео экрана». |
| 15:40:30.657579408 | Event 10513, `secretary.tool_call` | `message_worker` содержит точное уточнение. |
| 15:40:33.572926934 | Event 10515, `turn.created` | Новый Worker Follow-up, поскольку Worker уже idle. |
| 15:40:38.275 | Codex Worker user message | Точный prompt: `i meant record a video`. |
| 15:40:42.946 | Codex Worker `task_complete` | Второе выполнение завершено. |
| 15:40:44.196932726 | Event 10601, `result.accepted` | Принят второй Result. |

Secretary turn IDs: `stn_c48f7fdce234642baf167c7c051ee2a1`, `stn_2ed2f583173f253782efdb066c2e91db`.
Worker reference: `wrk_cbb02a53c0050723fa5151b929e293d5`.
Worker Turn IDs: `trn_a5c382b62a9e3f41728f128103cd8a07`, `trn_f870f4f450c0a4ab7272b5c212b4cd1f`.
Attempt IDs: `att_f806c714b1d271e0fc87756457d6f046`, `att_05520d4c505cf6740f9b04fb3adb02f0`.

Сессии для адресной проверки первичного журнала:

- omarchy: `rollout-2026-10-10T22-31-49-01a12671-1dcb-7373-a110-20819e533e37.jsonl`.
- Mac Worker: `rollout-2026-10-10T22-40-11-01a12678-c6f9-7fa0-a144-8db4a58d55fc.jsonl`.

Задержка от сохранения уточнения до его появления в native Secretary prompt: 42,690 секунды. Два Results относятся к разным Attempts; это не Telegram replay одного ответа.

## Минимальное воспроизведение без production

Из корня repo:

```sh
python3 .scratch/secretary-active-steering/diagnostic/run.py
```

Скрипт использует Go overlay и временный каталог, не меняя исходники. Fixture создаёт настоящий Store, активный Secretary turn и Session с наблюдаемым `Steer`; затем вызывает реальный `Runtime.HandleMessage`. Зависимостей сверх Go/Python проекта нет. Это диагностический тест вызова steering, а не доказательство native same-turn применения.

Результат на HEAD `1f7f58f`, 2026-10-11:

```text
--- FAIL: TestDiagnosisDurableActiveMessageSteers (0.03s)
    steering_diagnosis_test.go:18: active durable Secretary queued clarification instead of calling Session.Steer
FAIL
```

При реализации перенести этот сценарий в штатные тесты и расширить проверкой identity, ошибок и terminal race. Native acceptance обязана отдельно показать применение уточнения до terminal той же работы.
