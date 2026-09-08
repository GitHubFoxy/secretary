# Design the privileged Secretary bridge

Type: grilling
Status: resolved

## Question

Определить минимальный contract отдельного Secretary bridge, который без fork AgentHub создаёт worker из category template, добавляет его в Team, назначает task и сохраняет идентификаторы для follow-up.

Нужно зафиксировать trust boundary для AgentHub operator API и meaning первых templates: `coding`, `general`, `summarizing`, `books`.

## Answer

Первый vertical slice реализует Bridge как trusted-local CLI на текущем Unix user. Это contract boundary, не security boundary: Secretary и Worker технически не изолированы от Bridge.

### Public contract

Secretary получает только:

```text
delegate(task)
send_follow_up(worker_ref, text)
```

`delegate` создаёт отдельный Worker с пустым Workspace, добавляет его в Team, сохраняет Worker binding и передаёт Task envelope. Успех означает `accepted` только после сохранения binding и передачи Task runtime. Ошибка до этого возвращается как Dispatch failure без automatic retry.

`send_follow_up` использует тот же Worker binding и session. Он создаёт новую Attempt, только если Worker idle. Во время активной Attempt Bridge возвращает `not_ready`, без неявной очереди.

Categories отсутствуют в первом contract. Позднее добавится новая версия `delegate(category, task)` с фиксированными templates.

### Worker и Result

Worker получает Task envelope: Task text, `worker_ref`, одноразовый Callback capability и правило terminal Result callback. Worker отправляет private `report-result` через Bridge CLI. Result имеет status `succeeded`, `failed` или `canceled`, обязательный summary и optional artifact references. Bridge публикует Result от имени Secretary в Origin Conversation.

Worker хранится до явного Task closure. Operator выполняет closure отдельной Bridge CLI командой: она останавливает Worker и архивирует binding, но сохраняет историю и Workspace.

### AgentHub integration

Bridge использует существующий AgentHub HTTP API, не пишет в AgentHub SQLite. Он работает от отдельного `bridge-operator` user с минимальными capabilities. AgentHub не предоставляет long-lived service token, поэтому Bridge хранит credential локально с правами `0600` и обновляет примерно 12-hour Bearer session сам. Root account не используется.

Первый Worker template: Codex ACP на local Linux, full access, provider-default model и thinking level, отдельный Workspace. Bridge state хранится в собственной SQLite: Task, `worker_ref`, Origin Conversation, AgentHub internal identifiers, Workspace, Attempt state и Callback capability hash.

## Scope revision

Bridge остаётся semantic contract POC, но не будущим service. Для proof допустимы manually supplied short-lived AgentHub token и минимальное локальное state storage. Создание отдельного `bridge-operator`, automatic session renewal, Task closure, retention policy и security hardening не входят в первый implementation pass.

## Constraints

- Полный AgentHub operator API и более широкий privileged access не предоставлять Secretary в этом slice.
- Для будущего deployment с недоверенными Workers нужна отдельная security design: Unix users или containers. Она не входит в trusted-local vertical slice.
