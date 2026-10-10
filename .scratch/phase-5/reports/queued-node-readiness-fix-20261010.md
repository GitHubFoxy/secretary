# Готовность Node перед доставкой queued Follow-up — 2026-10-10

Исправление подготовлено в изолированном worktree `/private/tmp/secretary-p5-queue-readiness`, ветка `p5/queue-readiness`, исходная integration revision `e8de503297ce7e85ef00a173412e27757d024a09`.

## Причина и изменение

Queue pump выполнял promotion и claim до установления live authenticated connection Execution node. После server restart сохранённая inventory/Online запись сама по себе не подтверждает наличие connection в новом ServerManager. SendCommand возвращал `Node is offline`, и ещё не переданный prompt становился blocked вместо ожидания reconnect.

ServerManager.CommandReady проверяет durable Node record и текущую connection под существующим mutex. NodeRuntime передаёт проверку queue pump. При известной временной недоступности (offline, отсутствующая connection, draining) pending остаётся без нового Turn/Attempt, prepared delivering intent остаётся без lease. Проверка находится под существующим per-Worker lifecycle gate: до promotion pending и после проверок terminal/failed/uncertain/lease перед claim prepared.

Ошибки readiness (revoked, отсутствующий Node, отсутствующий runtime) проходят в прежний handoff/error path и остаются видимыми. Race disconnect после preflight сохраняет прежнее fail-closed поведение. Claimed/unknown/failed intent не получает нового retry. Таймеры и обычные Dispatch/Resume/Steering не менялись; local Node и fixtures без readiness метода продолжают прежний путь.

## Red → green и наблюдаемое поведение

`TestPublicQueuedFollowUpWaitsForNodeReconnect` использует существующий authenticated Node protocol и внешний Codex ACP process fixture с native history. Два отдельных inputs с одинаковым текстом `/q recall` сохраняют разные identities и FIFO. Pending и prepared subtests до изменения падали с `node server: Node is offline`.

После исправления persisted Online=true при отсутствующей connection не вызывает promotion pending, claim prepared и native prompt. Prepared вариант проходит production Phase4 recovery; lease остаётся пустой. После authenticated reconnect каждый input доставляется ровно один раз по прежней session, вспоминает nonce, оставляет три Attempts/Results с initial. Connected/draining вариант удерживает очередь до снятия draining. Отдельный public WorkerService regression подтверждает, что revoked Node остаётся blocked с ошибкой и не повторяется.

Это deterministic fixtures, не новая live native приёмка. Исходный live prepared-restart FAIL сохраняется; ticket 03 и общий gate 05 не закрываются этим отчётом.

## Проверки

- `go test ./internal/ctl -run 'TestPublicQueuedFollowUpWaitsForNodeReconnect|TestRevokedNodeBlocksQueue' -count=1 -timeout=80s` — PASS.
- `go test ./internal/ctl ./internal/node ./cmd/secretaryd -run 'Queue|Queued|Public.*FollowUp|ProductionAssembly' -count=1 -timeout=180s` — PASS.
- `go test -race ./internal/ctl ./internal/node ./cmd/secretaryd -run 'Queue|Queued|Public.*FollowUp|ProductionAssembly' -count=1 -timeout=180s` — PASS. Включены queue Close/recovery, claimed и failed handoff regressions.
- Полный `go test ./internal/ctl ./internal/node ./cmd/secretaryd -count=1 -timeout=180s`: ctl и secretaryd PASS; Node process integration сначала остановился на недоверенном `.mise.toml` нового worktree. После `mise trust` повторён весь пакет Node — PASS.
- `go build ./...` — PASS.
- `git diff --check` — PASS.

Пользовательские `spec/THE spec.md` и `spec/memory.md`, actual runtime/DB/outbox и queue не изменялись.
