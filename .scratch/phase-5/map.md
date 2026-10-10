# Phase 5: карта Minimal MVP

## Notes

Цель: [spec](spec.md). История: [архив Phase 4](../.archived/phase-4/map.md). Разбивка на пять сквозных tickets согласована пользователем. Область ограничена рабочим Codex/CC MVP; live readiness ещё не подтверждена.

| Ticket | Status | Blocked by | Проверяемый результат |
| --- | --- | --- | --- |
| [01](issues/01-codex-native-round-trip.md) | claimed | None | Codex Secretary → native local/remote Worker → Result и resume |
| [02](issues/02-claude-interactive-runtime.md) | claimed | None | CC интерактивный runtime, MCP/profile/resume и доказанный steering |
| [03](issues/03-durable-direct-worker-messages.md) | resolved | None | Web/Telegram direct message и durable `/q` |
| [04](issues/04-readable-unified-chat.md) | claimed | None | Читаемый единый чат, canonical reply и topics без дублей |
| [05](issues/05-deploy-and-live-acceptance.md) | blocked | 01, 02, 03, 04 | Развёрнутый MVP и реальная матрица приёмки |

`Status:` — состояние работы; `Blocked by:` — номера реальных blockers. После выполнения blockers перевести 05 в `ready-for-agent`; перед работой — `claimed`, при выполнении acceptance — `resolved`. Не считать fixtures доказательством live результата.

## Decisions so far

- Сохранить server/store/Node/MCP architecture; одна integration branch.
- 01–04 не блокируют друг друга: native Session interface, Worker HTTP/MCP и canonical events уже существуют. 02 может начать native spike без нового Codex; 03 проверяет server-owned очередь на существующем Session seam; 04 проверяет canonical delivery и renderer на существующих events. Native cross-harness integration — gate 05.
- Public seams заранее разрешены: server HTTP/MCP, native Session, Web rendering, Bot API output. Повторно спрашивать разрешение не требуется.
- Codex ACP сохраняется при успешной native проверке. CC streaming/SDK выбирается по evidence, а не по наличию flags.
- Steering меняет текущую работу; `/q` создаёт следующий Follow-up. CC fallback в очередь или interrupt + запрос не удовлетворяет steering.
- Без sync прямого Worker диалога с Secretary и без удаления старых harness/данных.

## Fog

- Реальная same-turn семантика Claude на доступной версии CLI/SDK не доказана; ответ — в 02. Если возможностей недостаточно, явно записать blocker и продолжить поиск совместимого native пути, не объявлять MVP готовым.
- Реальная работоспособность Codex ACP, точные исполняемые версии, remote profile/MCP и Secretary reply contract — 01.
- Реальная admin/forum конфигурация Telegram, deployment credentials/readiness и полная restart matrix — 05.

## Evidence

Существующие исследования являются baseline, не новой live приёмкой. Каждый ticket добавляет проверенные ответы в `## Answer` и историю в `## Comments`; 05 разделяет scoped fixtures и реальные сценарии. В карту добавлять короткие ссылки на полученные решения, не копировать частные логи.

- [CC streaming spike](reports/claude-streaming-spike-20261009.md): работа native процесса и MCP подтверждена; живой запуск модели и steering блокирует HTTP403 из-за квоты существующего провайдера. Приёмка CC не завершена.

- Интеграция 01/03/04: Codex merge `58e4fe9`, unified chat merge `fd90825`; queue production wiring и конфликт ACP fixtures исправлены. [03 resolved](issues/03-durable-direct-worker-messages.md#answer) по scoped server/channel criteria; 01/04 остаются claimed, 05 blocked из-за native/channel live требований и незавершённого CC steering.
- Scoped integration checks 2026-10-10: `go test ./...`, `go build ./...`, Web tests (14), `npm run build:all` — PASS после `npm ci` по merged lockfile. External ACP и Bot API fixtures не засчитываются как live evidence. Browser visual, real Telegram permissions/routing и deployment matrix ещё не проверены.

- [Review fixes](reports/review-fixes-20261010.md): 03 повторно открыт после Close/startup repro и resolved после исправлений и scoped/race checks. Дополнительно исправлены owner-cookie Node read, Codex writer lifecycle, native dispatch/resume receipts и CC backpressure/cancellation ordering. 01/02/04 остаются claimed, 05 blocked; новый live PASS не объявлен.

- Живая проверка выявила пропущенный production owner route, зависание Claude на дочерних процессах, потерю MCP с пустыми arrays, потерю failed receipt при reconnect, блокировку replay поздней activity, пропуск MCP tool activity и неверную HMAC подпись persisted JSON. Исправления интегрированы до `e8de503`; полный `go test ./...` и `go build ./...` — PASS. Независимые проверки [стандартов](reports/live-fixes-review-standards-20261010.md), [спецификации](reports/live-fixes-review-spec-20261010.md), [wire standards](reports/wire-review-standards-20261010.md) и [wire spec](reports/wire-review-spec-20261010.md) не нашли новых ошибок этих исправлений.
- Native Mac Codex: same-Attempt steering во время наблюдаемого собственного `sleep`, две `/q` FIFO и idempotent replay, реальный Node-local MCP, прежняя native session и context после idle restart, missing session fail-closed — PASS на явно записанных промежуточных сборках. На `e8de503` прежние семь persisted late frames доставлены штатным replay/ACK, без повторного execution и изменения старой failed queue. Полная restart/observer матрица ещё проверяется; это не закрывает 01/04/05 и CC steering.
- На `e8de503` реальный MCP tool виден в Activity API и Web observer. Однако idle pending `/q` после аварийного restart блокируется immediate pump до Node reconnect; 03 вновь claimed, критерий restart открыт. История прежних PASS сохранена, новый FAIL не заменён fixture результатом.
- На `89e3135` queue readiness исправлена, полный Go test/build и оба независимых reviews — PASS. Новая реальная prepared queue пережила server crash и выполнилась FIFO после reconnect; context/session сохранены. Active crash/missing session и old outbox healing также прошли реальные проверки. [03 повторно resolved](issues/03-durable-direct-worker-messages.md#comments); [матрица и оговорки](reports/mac-native-acceptance-20261010.md). Свежий CC probe по-прежнему quota403, CC steering/remote auth/Telegram не закрыты. Проверенный Linux candidate размещён на omarchy с manifest/SHA256, services не активированы; Phase 5 остаётся active с gate05 blocked.
