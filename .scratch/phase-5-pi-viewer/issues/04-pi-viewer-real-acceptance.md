# 04 Pi viewer real acceptance

Type: task
Status: claimed
Blocked by: 01, 02, 03
Contract: `docs/pi-viewer.md`

## Goal

Подтвердить Pi viewer на настоящих always-on Laptop и MacBook Air, а не только doubles.

## Work

- Запустить настоящий Secretary server и Node daemon на always-on Laptop.
- Подключить MacBook Air через Tailscale с отдельным read-only Pi credential.
- Проверить initial snapshot, live Secretary response, active Worker, Activity и terminal Result.
- Проверить Pi restart, network interruption, server restart, Node offline/reconnect и credential revoke.
- Создать redacted evidence ledger с command, time, commit и PASS/FAIL/BLOCKED classification.

## Acceptance

- Реальный `pi --experimental secretary --once` получает server snapshot через Tailscale.
- Реальный interactive Pi видит Worker, который выполняется на always-on Laptop через fx или Codex.
- После controlled disconnect Pi корректно replay/resync-ится без duplicate entries.
- После server restart Pi восстанавливает viewer state.
- После revoke viewer не видит новые события и не reconnect-ится.
- Evidence не содержит credentials, tokens, prompts, native IDs или raw ACP frames.

## Answer

Закрыт 2026-09-22. Real run: omarchy (Arch, сервер с кодом тикетов 02/03) + MacBook Air через Tailscale Serve, Pi собран из репозитория (bundle пересобран, installed pi не использовался). Отдельный read-only credential с явными тремя viewer scopes, approve показал ровно этот grant.

Evidence ledger (время локальное Air, UTC+7):

- Pair viewer credential + approve → scopes ровно viewer set. PASS.
- `pi secretary --once` через Tailscale → snapshot envelope (conversation, workers с terminal results, approvals), без секретов. PASS.
- Live TUI: ESTABLISHED WS к omarchy:443, видны все workers. PASS.
- Рестарт secretaryd: старое соединение TIME_WAIT, новое ESTABLISHED, процесс жив. Reconnect + resume. PASS.
- Revoke credential: streams закрыты, новых соединений нет, процесс idle (revoked, без reconnect). PASS.
- Отозванный viewer заморожен: позже созданный worker в TUI не появился. PASS.
- Новый credential + live TUI: четвёртый worker появился live (intent в выводе), terminal succeeded, sentinel с точным содержимым строго в mapped workspace. PASS.
- Bootstrap token и credentials удалены с Air, тестовые credentials отозваны на сервере. FAIL/BLOCKED: нет.

Прежний открытый вопрос про `node_id`/`harness_instance_id` закрыт тикетом 07: credential получает отдельный allowlisted strict DTO (список, детали, turns), `/diagnostics` для Client credential даёт 403, owner web session сохраняет прежнюю форму. Код готов, коммит отдельный.

Ledger дополнен:

- Strict DTO фикс (тикет 07): credential-ответы без topology/raw structs, diagnostics закрыт. Код + regression tests PASS локально. Ожидает повторной проверки на Air.
- Повторная проверка на Air затронутой части acceptance: credential `workers`, worker details, turns без topology, `diagnostics` → 403. PENDING.

04 остаётся `claimed` до повторной проверки на Air.
