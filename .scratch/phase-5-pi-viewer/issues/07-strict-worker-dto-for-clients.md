# 07 Strict worker DTO for client credentials

Type: task
Status: resolved
Blocked by: none
Contract: `docs/pi-viewer.md`
Blocks: 04 (real acceptance cannot resolve until Pi credentials stop receiving Node topology)

## Goal

`node_id` и `harness_instance_id` не уходят Pi credential вообще. Прямое требование privacy boundary.

## Background

Real acceptance тикета 04 показал: `GET /v1/workers` отдаёт viewer credential поля `node_id` и `harness_instance_id` из `publicWorkerDTO`. Pi эти поля не читает, но owner Web UI (`App.svelte`, `ui-model.js`) на них зависит, поэтому удалять их из общей формы нельзя.

## Work

```text
Client credential → strict public worker DTO (без node_id, harness_instance_id)
web session        → текущая owner UI форма без изменений
```

- Отдельный allowlisted DTO для client-credential вызовов worker surface: список, детали, turns.
- Все вложенные slices в details мапятся через allowlisted DTO: `turns` без `context_snapshot`/`normalized_intent`, `attempts` без топологии, `outcomes` без `diagnostics`, `results` без `correlation_id`/`attempt_id`/`worker_id`. Никаких raw `core.*` structs в credential-ответе.
- `/v1/workers/{ref}/diagnostics`: credential → 403, web session → существующее поведение.
- Pi client и TUI не меняются (поля не используются).
- Regression tests: credential получает strict форму, web session — полную. Проверка всего HTTP response рекурсивно по запрещённым ключам, не только worker object.

## Acceptance

- Viewer credential не получает `node_id`/`harness_instance_id` ни в списке, ни в деталях worker, ни в turns.
- Никакие raw domain structs (и `diagnostics`, `context_snapshot`, `correlation_id` и т.п.) не уходят credential.
- `/v1/workers/{ref}/diagnostics` → 403 для credential, без изменений для web session.
- Owner UI без изменений.
- Повторная проверка на Air, затем закрытие 04.

## Answer

Код закрыт 2026-09-22: strict DTO для credential на list/details/turns, `/diagnostics` 403 для любого Client credential (включая `internalCredential`), web session без изменений. `TestWorkerSurfaceStrictForClientCredential` засидет непустые attempts/outcomes/results: в объектах стора лежат `diagnostics`, `error_message` с секретом, `correlation_id`, `failure_code`, топология `node_id`/`harness_instance_id` (это проверяется отдельно на данных стора и в owner-ответе), а весь HTTP response credential проверяется рекурсивно по запрещённым ключам. `go test ./...`, `go vet`, `gofmt`, `diff --check` чистые. Повторная проверка на Air выполняется в рамках 04.
