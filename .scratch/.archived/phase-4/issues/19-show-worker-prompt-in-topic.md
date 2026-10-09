# 19 Show the dispatched Worker prompt in its Telegram Topic

Type: task
Status: resolved

## Work

A Worker Topic currently does not show the task prompt Secretary sent to that Worker. Publish the dispatched, user-facing Worker prompt in the Topic when it is created so the task context is visible without returning to General.

Show the actual task text sent to the Worker, not Secretary's hidden system prompt, internal reasoning or private routing metadata. Keep delivery durable and deduplicated across Telegram retries.

## Acceptance

- The Worker Topic displays the initial task prompt sent by Secretary.
- The prompt appears once, in a clear position before subsequent Worker activity.
- Telegram retry/replay does not duplicate the prompt.
- Internal instructions, reasoning and private identifiers are not exposed.
- Tests cover Topic creation, delivery retry and replay.

## Answer

- Telegram adapter публикует `worker.spawned.intent` в Worker Topic с заголовком "Задача от Secretary:" до последующих Worker notifications. Переносы строк сохраняются; применяется существующая фильтрация чувствительного текста. Остальной payload не выводится.
- Сообщение проходит через durable outbox с identity `task-prompt:<worker_ref>`. `TopicMapping.prompt_delivered` предотвращает повторную отправку после replay или restart, в том числе когда pending сообщение уже доставлено через Flush до replay события.
- Длинный prompt доставляется частями с сохранением прогресса; retry продолжает с недоставленной части и не создаёт новый Topic.
- Тесты проверяют порядок, форматирование, отсутствие private metadata, restart/replay и сбои первой или последующей части.
- Проверки: `go test ./...`, `go test -race ./internal/telegram ./cmd/secretaryd`, `git diff --check`.
- Как и для остальных Bot API сообщений, неопределённый исход сетевого запроса после фактического приёма Telegram не даёт строгой exactly-once гарантии: Bot API не принимает idempotency key.
