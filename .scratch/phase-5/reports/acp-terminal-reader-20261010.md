# Завершение Codex: большой ACP session title

## Симптом и причина

Secretary turn `stn_94a7bae4c42cb28263be5e90d7ac4abb` получил 17 текстовых delta, но не создал canonical reply/Telegram reply. Native Codex session `01a12432-5869-7623-ae9b-e9bc25e01b59`, turn `01a12432-f0b9-70c0-9d20-1cf6d1175f13` действительно завершился: assistant `final_answer` в 05:04:44.735 UTC, `task_complete` в 05:04:44.779 UTC.

Установленный codex-acp 1.12.0 генерирует fallback session title из всего первого prompt (`createPromptFallbackTitle` → `normalizeSessionTitle`), без ограничения длины. Secretary передаёт canonical context вместе с вводом, поэтому такая notification может превышать 64 KiB.

Общий ACP reader использовал стандартный `bufio.Scanner`, ограниченный 64 KiB. После большого кадра scanner прекращал чтение. Deferred `process.Wait` ожидал живой persistent ACP process, поэтому pending `session/prompt` не получал уже отправленный штатный ответ. Это ошибка чтения протокола, а не отсутствие native completion.

## Доказательство на работающем процессе

Read-only FIONREAD показал 14325 непрочитанных байтов в ACP stdout pipe и Go reader fd 20. Scoped GDB attach был запрещён Linux ptrace policy; sudo и изменения политики не применялись.

Linux `tee(2)` скопировал эти байты в отдельный диагностический pipe **без потребления оригинала**. В памяти диагностического процесса были проверены только структура и разрешённые metadata:

- первая строка: 13646 байтов, хвост JSON-объекта без начала;
- следующая строка: полноценный JSON-RPC response, ID 5, `stopReason=end_turn`, 485 байтов;
- следующая строка: `session/update`, `session_info_update`, 191 байт;
- original unread bytes до и после проверки: **14325 → 14325**.

Prompt, title, reasoning, capabilities и auth из буфера не печатались и не сохранялись. Ответ ACP уже находился в pipe; reader остановился перед ним.

Отдельные проверки исключили общую несовместимость версий: оба реальных Workers на omarchy/Mac завершились с теми же версиями; изолированный ACP без MCP завершился штатно; изолированный ACP с исходным Secretary profile/MCP и 40-секундной паузой до короткого нового ввода также завершился (`end_turn`, 480 байтов; session `01a12526-1183-7e31-92d7-9ff59fbd2bcd`). Короткий ввод не воспроизводит большой fallback title.

## Исправление и проверка

Общий ACP reader использует `bufio.Reader.ReadBytes('\n')`, сохраняя целый JSONL кадр независимо от прежнего Scanner limit. Полностью сохраняются notification, FIFO barrier и настоящий RPC terminal response. Новых terminal heuristics, таймаутов завершения, повторного ввода или изменений native auth/profile нет.

Публичная регрессия `TestLargeSessionTitleDoesNotStrandPromptResponse`: валидный `session_info_update.title` больше 64 KiB, затем настоящий `session/prompt` response, persistent peer остаётся живым. Проверяются полное сохранение notification и ответ после её FIFO обработки.

`go test ./internal/acp -run '^TestLargeSessionTitleDoesNotStrandPromptResponse$' -count=1`: исходный reader **RED** (response stranded), исправленный **GREEN**. Живая проверка нового Secretary turn после развёртывания остаётся обязательной; неизвестный исход старого ввода не повторялся.

`MISE_TRUSTED_CONFIG_PATHS=/Users/beruseruko/projects/secretary-p5-acp-terminal-hang go test -race ./internal/acp ./internal/node ./internal/secretary`: **PASS**, включая отдельные Node процессы. Переменная доверия применялась только к проверочной команде; глобальная конфигурация mise не менялась.
