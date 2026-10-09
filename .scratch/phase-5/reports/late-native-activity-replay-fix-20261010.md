# Поздняя native activity больше не блокирует replay

Исходный P1 finding: `/private/tmp/p5-late-activity-review.md`; public proof — `/private/tmp/p5-late-activity-review/repro_test.go`. Source fixture показала terminal sequence 1 и buffered assistant text sequence 2. Production StoreEventSink возвращал ErrInvalidTransition для второго кадра; reconnect повторял тот же event без ACK и снова закрывался.

Regression разделён на публичные проверки источника и сохранённой outbox. До исправления обе FAIL: `TestExecutionNodeStopsActivityAfterTerminal` находил два события после terminal, `TestReviewLateNativeActivityDoesNotPoisonReplay` закрывал socket на позднем тексте и его replay.

Исправление источника — watcher возвращается после QueueOutcome, поэтому больше не публикует activity этой Attempt после terminal. Authoritative Result сохраняется; buffered поздняя activity не нужна для нового execution и не превращается в новый turn.

Исправление server — `RecordNodeActivityReplay` проверяет payload до входа в транзакцию, затем читает Attempt и Worker через существующие transactional helpers, проверяет все immutable identities и только после этого пропускает поздний frame завершённой Attempt. Return value — пустой Event; production sink не вызывает trusted-local approval для такого drop. Прямая `RecordNodeActivity` сохраняет strict terminal error contract. Idempotency старого активного события не обходит terminal check на replay. Для безопасного позднего кадра protocol продолжает обычный authenticated ACK; запись истории, Result и lifecycle не меняются. Ручного удаления старой outbox нет.

Публичная проверка persisted outbox сохраняет terminal sequence 1 и late text sequence 2 через LocalStore, отправляет их authenticated ProtocolServer с production event sink, намеренно повторяет late text после reconnect и подтверждает ACK2/пустую outbox. Late permission sequence 3 получает ACK без Approval и без trusted-local handler. Wrong Node, HarnessInstance, Worker, Turn и неизвестная Attempt по-прежнему не получают ACK. Terminal state сохраняется. Публичная source fixture проверяет отсутствие второго события после buffered native Close.

Проверки:

- `go test -race ./internal/node -run 'TestReviewLateNativeActivityDoesNotPoisonReplay|TestExecutionNodeStopsActivityAfterTerminal' -count=1` — PASS.
- `go test -race ./internal/core -run 'NodeActivity|NodeTerminal|Approval|Queue|Worker' -count=1` — PASS.
- `go test -race ./internal/node -run 'LateNative|StopsActivity|Daemon|LocalStore|Authenticated|Protocol|Receipt|ExecutionNode|WorkerMCP|Claude|ACPRuntimeMCP' -count=1` — PASS.
- `go test ./internal/node -run 'Native|Continuation|Queue|Close|Cancel|Runtime|Deferred|Readiness|Rebind|Recovery' -count=1` — PASS.
- `go build ./...` — PASS.
- `git diff --check` — PASS.

Прежний live queue FAIL остаётся FAIL до отдельного свежего native прогона. Этот fixture PASS не объявляет full gate или native live acceptance; Claude quota403 остаётся внешним блокером.
