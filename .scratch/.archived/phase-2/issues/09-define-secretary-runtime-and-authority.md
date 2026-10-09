# Define the Secretary runtime and authority

Type: grilling
Status: resolved

## Question

Как запускается persistent Secretary identity и каким минимальным authority она управляет Task lifecycle, не превращая Channel adapter или Worker в control plane?

## Answer

Secretary - одна persistent Codex ACP session на default local Node, связанная с Personal Conversation. Server не встраивает LLM harness и не заменяет Secretary deterministic router.

Secretary получает capability-scoped `secretaryctl` в своём runtime environment. CLI принимает typed JSON и имеет только `create-task`, `retry-dispatch`, `close-task`, `list-tasks` и `show-task`. У него нет generic Node execution, доступа к adapter credentials или возможности самому направлять Result в Conversation.

Channel adapters не управляют Task lifecycle. Worker observer остаётся узким явным путём для Steering, Queued message и Cancel существующего Worker.

Server создаёт random Secretary capability при запуске runtime, хранит только её hash и передаёт raw token только этому process. При создании нового Secretary runtime capability меняется.
