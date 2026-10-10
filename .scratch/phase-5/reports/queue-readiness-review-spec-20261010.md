## Спецификация

Проверен diff `1b06ddc...d1a1ad1b279b5117b9cb4118d911859419dd4c8c`. Нарушений требований, ошибочной реализации и расширения объёма задачи не обнаружено.

Требование ticket 03: «Server/Node restart сохраняют очередь и identities, не повторяют неизвестное выполнение или активную Attempt» (`.scratch/phase-5/issues/03-durable-direct-worker-messages.md:17`). Production assembly действительно создаёт `NodeRuntime{Manager: remote, Local: local}` и запускает pump (`cmd/secretaryd/main.go:504–507`). Readiness проверяет сохранённую Online запись вместе с authenticated connection и draining/revoked. Pending guard находится до promotion; prepared guard — после проверок terminal/failed/uncertain/lease и до claim. Поэтому persisted Online без connection не создаёт новую Attempt/lease и не вызывает native prompt. Известные ошибки readiness проходят прежний handoff/error path; revoked regression подтверждает видимый blocked без повторения. Local runtime и fixtures без дополнительного метода сохраняют прежнюю семантику.

Требование: «После terminal Result очередь FIFO создаёт новый Follow-up с новой Attempt и прежней native session» (`03:16`), а разные inputs с одинаковым текстом остаются разными (`03:18`). Public external ACP fixture проверяет pending/prepared/draining, два отдельных `/q recall`, пустые turn/lease до reconnect, отсутствие native prompt, прежнюю session и remembered marker после reconnect, FIFO и три Attempts/Results без повторной доставки.

Требование recovery: «Claimed/active/unknown execution получает interrupted Result и остаётся без автоматического retry» (`docs/architecture/runtime-contracts.md:79`). Новые guards не обходят старые terminal/lease проверки; startup recovery сохраняет лишь доказанно unclaimed prepared intent. Shared lifecycle gate остаётся вокруг promotion/claim/handoff, Close cancellation и Approval routing не изменены (`03:19–20`, runtime contracts:77–78).

Ограничение доказательств: свежего live native PASS этого исправления ещё нет. Fixture не перезапускает сам ServerManager/Store, но воспроизводит решающее состояние persisted Online без connection и использует production recovery. Отчёт честно сохраняет исходный native FAIL; ticket 03 остаётся claimed, общий gate 05 открыт. Это незавершённая live приёмка, а не найденная ошибка diff. CC steering, remote auth и Telegram этой правкой не закрыты.

Повторные тесты не запускались: новых нерешённых сомнений после трассировки production callers нет; targeted/race PASS уже представлены исполнителем.

После этого review проведена новая native приёмка на `89e3135`: prepared FIFO после crash/reconnect и сохранение контекста подтверждены, scoped 03 resolved. Подробности и исходный FAIL сохранены в [живой матрице](mac-native-acceptance-20261010.md). Общий gate 05 остаётся открыт.
