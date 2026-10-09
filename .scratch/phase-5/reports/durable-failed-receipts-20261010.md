# Durable failed dispatch/resume receipts

Исходный snapshot: `d76c372`, после трёх исправлений `b694f1e`.

Публичный regression `/private/tmp/p5-lost-receipt-review/repro_test.go` подтвердил потерю receipt: Runtime.Start закрывает authenticated ProtocolConnection перед возвратом native auth failure; после reconnect/heartbeat Node имеет failed command, pendingEvents=0, nativeCalls=1, сервер receipt не получает. До исправления `TestReviewFailedDispatchReceiptSurvivesReconnect` — FAIL.

`CompleteCommand` теперь атомарно сохраняет отказ Dispatch/Resume и raw CommandOutcome в существующей sequenced outbox. PendingEvent получил optional message type; прежние NodeEvent payloads и raw CommandOutcome wire schema сохранены. Daemon отправляет эти отказы через обычный flush/replay, а ProtocolServer ACK подтверждает sequenced receipt только после успешного authenticated sink. Старые receipt с sequence=0 продолжают обрабатываться как прежде. Readiness и остальные command paths не менялись.

Дополнительная очередь returned failed receipt в Daemon покрывает ранний validation rejection без claim и повтор команды после ACK. Pending receipt dedup использует command ID; ClaimCommand по-прежнему запрещает повтор native execution. Публичный Daemon/ProtocolServer regression проверяет Dispatch и Resume с предварительным durable checkpoint: reconnect доставляет прежние command/Turn/Attempt identities, ACK очищает outbox, следующий reconnect не повторяет ACKed receipt, duplicate command возвращает ту же ошибку при nativeCalls=1.

Проверки:

- `go test -race ./internal/node -run 'Daemon|LocalStore|Authenticated|Protocol|Receipt|ExecutionNode|WorkerMCP' -count=1` — PASS.
- `go build ./...` — PASS.
- `git diff --check` — PASS.

Исправление обеспечивает доставку ошибки, не делает повтор Start/Resume и не доказывает native live acceptance. Независимый live queue failure и поздняя activity требуют отдельной проверки; данные работающего сервера не редактировались.
