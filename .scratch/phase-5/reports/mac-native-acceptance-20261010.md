# Живая проверка Mac — 2026-10-10

Phase 5 не завершена. Это реальный Codex на Mac, совместная топология server/Node; удалённый Node и Telegram здесь не проверены. Native Codex 0.162.0, ACP 1.12.0. Итоговая исполняемая сборка `89e3135cc22c0cac5727617d2c9310ff2c246848`. Сборки из immutable git archive, пользовательские незакоммиченные spec-файлы не включены. Полный Go test/build и relevant race — PASS; оба независимых reviews каждого исправления без новых замечаний.

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
| Active Attempt restart без автоматического исполнения | PASS | `e8de503`: observed owned sleep50, crash Node/server, один interrupted Result `res_64042d9a6af362440999ae727051b0b0`, Attempt count1 без auto Prompt; явное resume сохраняет context и session `01a12416-7df6-7331-8441-db41411a30a3` |
| Новый prepared /q restart после readiness fix | PASS | `89e3135`: оба input pending/unclaimed до crash; после reconnect Results `res_f8c4c70c14ce31afc21c84b30c7a4242` и `res_2b09d53218fbcf4122031fee278085c4`, ровно две новые Attempts/Results, FIFO, прежняя native session |
| Контекст после нового prepared FIFO | PASS с оговоркой | Два queue prompts по ошибке спрашивали маркер другого Worker, модель правдиво сообщила его отсутствие. Новый явный Follow-up `res_a1e0964fcd5131a1f5ac053f1a0c9b34` вернул правильный `ACTIVE_RESTART_CONTEXT_P5_104` и resume marker этого Worker. Исходные prompts/results не переписаны |

## Обнаруженные ошибки и границы

Старые FAIL сохранены в исходных свидетельствах: owner HTTP401, MCP nil env, потерянные failed receipts, поздняя activity блокирует ACK, persisted pretty JSON ломает HMAC и queue pump до Node reconnect. Они исправлены и не подменены фиктивной приёмкой. Независимые reviews стандартов и спецификации согласовали изменения кода; scoped 03 закрыт после нового реального prepared restart/FIFO proof.

Временные Node/данные используются только для acceptance. После восстановления старого outbox временная Node `p5-mac-acceptance` отозвана штатным API и её проверенные процессы остановлены. Четыре собственных Workers на 8092 закрыты штатным owner API; старые failed test queues отменены только при cleanup после сохранения исходных снимков, без повторной доставки. 8092 оставлен для просмотра: server PID53363, Node PID53225, health200, active Attempts0, outbox0. Исторические данные, Telegram bot, native auth и global config сохранены. Credential values, raw native history и reasoning не переносятся в репозиторий.

Исходный полный отчёт: `/private/tmp/p5-mac-live-final-evidence.md`; private артефакты в `/private/tmp/p5-mac-live` и `/private/tmp/p5-mac-independent`. Для новых PASS сохраняются build/runtime versions, identities и точные события; fixture checks учитываются отдельно.

## Открытые внешние требования

- omarchy Codex: прежний token refresh отказал; auth metadata не изменились. Реальный remote model turn и remote/Telegram приёмка ещё не прошли.
- Claude Code: свежий probe `89e3135` на 2026-10-10T04:41:27Z, Claude2.1.295, ровно одна Attempt `att_5b4e7c2c1bfd8f6dad0e219175d6d616` и failed Result `res_4c6b1ff4e6bb2cd1dfa6df6cdc829fc1`: provider API403 insufficient quota. Auth/provider/model/global config не менялись. Native same-Attempt steering не доказан; capability остаётся unsupported. Interrupt или очередь не засчитываются как steering.
- Сервисы на omarchy не активированы до успешной runtime readiness. Кандидат и резервные данные не означают развёрнутый MVP.

## Подготовленный кандидат

Immutable source archive, пять Linux binaries и manifest размещены в `/home/coder/.local/share/secretary/candidates/phase5-89e3135-20261010T0438Z`; SHA256 проверены на omarchy. Source SHA256 `034013c5c98f164c528380f68076b19004336babaf01f68fc6a636f2d44cb4e2`. Прежний backup сохранён. Активного cutover или переноса native auth не было; `secretary.service`/`secretary-node.service` остаются inactive.
