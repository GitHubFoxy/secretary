# Implement web session and Conversation sync

Type: task
Status: resolved
Triage: ready-for-agent
Blocked by: 10

Spec: ../spec.md

## Work

Implement owner bootstrap-token exchange to HttpOnly web session, inbound message acceptance and ordered WebSocket Conversation subscription. Subscription must atomically replay from `entry_seq` and continue live delivery. Deduplicate inbound adapter events at the server boundary.

Cover login, reconnect catch-up and duplicate inbound delivery through the external HTTP/WebSocket seam.

## Answer

Реализован web HTTP/WebSocket transport. Owner обменивает bootstrap token на durable HttpOnly session cookie. Server хранит одного configured owner между рестартами.

`POST /v1/messages` сохраняет inbound message с deduplication. `GET /v1/conversation` читает history по `after_seq`. `GET /v1/ws` atomically ставит subscriber на Conversation, replay-ит entries после `after_seq` и затем отдаёт live entries в том же порядке.

External seam tests покрывают login, inbound duplicate и WebSocket replay с последующим live entry. `mise exec -- go test ./...` и `mise exec -- go vet ./...` проходят.
