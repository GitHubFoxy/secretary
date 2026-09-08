# Implement web client and Worker observer

Type: task
Status: resolved
Triage: ready-for-agent
Blocked by: 11, 12, 13

Spec: ../spec.md

## Work

Build the first web client for Personal Conversation. Show Dispatch acknowledgement with Worker link. Worker observer must show initial Worker status, live-only activity, Steering input, `/q` queued input and Stop with `stopping` state.

Keep raw activity outside durable Conversation and send terminal Result verbatim through normal Conversation sync.

## Answer

Добавлен embedded vanilla web client в `web/`. Он делает owner login, ordered Conversation catch-up и reconnect через WebSocket, отправляет inbound messages и показывает Worker acknowledgement как ссылку на observer.

Observer поддерживает initial status, live-only activity WebSocket, Steering, queued input и Stop. Stop сразу показывает `stopping`; raw activity не записывается в Conversation. Queued input проходит через optional Node `Queueer` и запускается после завершения текущего ACP turn.

API tests покрывают authenticated status, Steering, queue, Stop и activity stream. Embedded asset test подтверждает выдачу web client. `mise exec -- go test ./...` и `mise exec -- go vet ./...` проходят.
