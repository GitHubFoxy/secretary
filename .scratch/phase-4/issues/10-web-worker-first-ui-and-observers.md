# Web Worker-first UI and observers

Type: task
Status: ready-for-human
Blocked by: 03, 06, 09

## Work

Обновить Web Client под Worker-first model и полноценный Secretary stream.

- В Personal Conversation показывать full Secretary live stream: text deltas, thinking summaries, tool calls и tool results.
- Показывать Worker в основном разговоре как compact status, acknowledgement и terminal Result.
- Сделать основной список Workers вместо Tasks, с Project, Node, HarnessInstance, Turn status и explicit offline/blocked states.
- Вынести полный Worker activity в отдельный observer.
- Поддержать Worker actions Message, Stop, Approve, Follow-up и Close через общий API.
- Отображать AttemptOutcomes и raw harness details только в diagnostic/observer view.
- Не показывать raw chain-of-thought и не выдавать бесконечный spinner без durable state/live event.
- Сохранить Enter-to-send и Shift+Enter newline behavior.

## Acceptance

- Main Conversation показывает Secretary stream и компактный Worker status согласно spec example.
- Worker observer показывает capability-dependent activity и не рисует отсутствующие events.
- Attempt retries видны в diagnostic view, но в Conversation отображается один Result на Turn.
- UI различает Saved, Accepted, Queued, Working, Waiting for approval, Needs input, Succeeded, Failed, Canceled и Interrupted.
- Reconnect восстанавливает выбранный Worker через replay без нового Dispatch.
- Offline Node оставляет Worker видимым и показывает immutable binding.
- Повторная отправка или reconnect не создают duplicate message, Worker или Result.
