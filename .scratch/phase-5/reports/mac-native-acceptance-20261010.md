# Живая проверка Mac — 2026-10-10

Phase 5 не завершена. Это реальный Codex на Mac, совместная топология server/Node; удалённый Node и Telegram здесь не проверены. Native Codex 0.162.0, ACP 1.12.0. Сборки из immutable git archive, пользовательские незакоммиченные spec-файлы не включены. Полный Go test/build на integration `e8de503` — PASS.

## Подтверждённые сценарии

| Сценарий | Результат | Сборка и свидетельство |
| --- | --- | --- |
| Secretary MCP создаёт Worker; один canonical reply | PASS | Реальные list_nodes/list_projects/spawn_worker; Web composer/AXI |
| Owner Nodes и Markdown/code/link | PASS | После `b694f1e`; owner HTTP200, strong/code.language-javascript/HTTPS в настоящем DOM |
| Profile и Node-local MCP с omitted env | PASS | Реальный tools/call прочитал `NODE_LOCAL_MCP_FILE_P5_63`; промежуточный `b694f1e`, затем immutable `372bee3` |
| Steering во время длинного инструмента | PASS | `372bee3`: собственный sleep50 PID51680 наблюдался до input; исходная Attempt `att_3aedcb87a16c9ca7fd2c304a727e935d` завершилась одним Result `res_768d1c88f5e69510367f355acb53ecb0` с `STEER_FINAL_P5_APPLIED_93`, context и MCP marker |
| Два active /q, FIFO и idempotent replay | PASS | `372bee3`: pending до terminal, затем Results `res_23963bda186f21a230fc10d07f107cf5` и `res_239a6c592e041060b9cb30f352560ac9`; три Attempts/Results, прежняя native session |
| Idle server+Node restart и recall | PASS | `372bee3`: до нового input нет новой Attempt; явный Follow-up Result `res_25d982b8b6527f133b1acc79ac3ed15a` сохраняет context/MCP marker и native session `01a1230c-9c4a-7a71-bef8-789a0a6da472` |
| Missing native session | PASS | Отсутствие собственного rollout вызывает visible failed `runtime_session_unavailable`; mapping/new session/hidden retry не создаются; файл восстановлен |
| Persisted late outbox после restart | PASS | `e8de503`: старые семь frames доставлены естественно, ACK1363→1370, outbox7→0, Node online; прежние две Attempts/Results и blocked/pending queue неизменны |
| MCP activity и Web observer | PASS | `e8de503`: actual tools/call; semantic tool_call/tool_result `mcp.acceptance.read_acceptance_marker`, seq526/527; читаемые ▶/✓ в AXI DOM, без rawInput |
| Idle pending /q после аварийного restart | FAIL | `e8de503`: оба inputs pending/unclaimed с пустым turn_id до остановки; immediate pump до Node reconnect блокирует первый как offline, второй pending. Ticket03 вновь открыт; исходный failed input не повторялся |
| Active Attempt restart без автоматического исполнения | В работе | Нужна отдельная реальная проверка |

## Обнаруженные ошибки и границы

Старые FAIL сохранены в исходных свидетельствах: owner HTTP401, MCP nil env, потерянные failed receipts, поздняя activity блокирует ACK и persisted pretty JSON ломает HMAC. Они исправлены и не подменены фиктивной приёмкой. Независимые reviews стандартов и спецификации согласовали изменения кода; новый queue readiness FAIL исправляется отдельно.

Временные Node/данные используются только для acceptance. После восстановления старого outbox временная Node `p5-mac-acceptance` отозвана штатным API и её проверенные процессы остановлены. Исторические данные, Telegram bot, native auth и global config сохранены. Credential values, raw native history и reasoning не переносятся в репозиторий.

Исходный полный отчёт: `/private/tmp/p5-mac-live-final-evidence.md`; private артефакты в `/private/tmp/p5-mac-live` и `/private/tmp/p5-mac-independent`. Для новых PASS сохраняются build/runtime versions, identities и точные события; fixture checks учитываются отдельно.

## Открытые внешние требования

- omarchy Codex: прежний token refresh отказал; auth metadata не изменились. Реальный remote model turn и remote/Telegram приёмка ещё не прошли.
- Claude Code: настроенный provider возвращает quota403, штатная авторизация без proxy отсутствует. Native same-Attempt steering не доказан; capability остаётся unsupported. Interrupt или очередь не засчитываются как steering.
- Сервисы на omarchy не активированы до успешной runtime readiness. Кандидат и резервные данные не означают развёрнутый MVP.
