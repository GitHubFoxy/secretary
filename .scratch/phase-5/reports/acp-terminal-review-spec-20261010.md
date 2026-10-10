# Проверка исправления ACP terminal reader

Диапазон: `fba6771...bc6f88c5fe33941372464d97fea1252b28b68ce5`. Scoped замечаний: 0. Предыдущий UI P2 закрыт в прошлом round; UI в этом изменении не менялся. Пользовательская Codex MVP приёмка остаётся открытой до нового настоящего Secretary turn после развёртывания; Claude Code отложен.

`internal/acp/jsonl.go:298` заменяет ограниченный Scanner на `bufio.Reader.ReadBytes('\n')`. Валидный JSONL кадр целиком поступает в прежний parser независимо от размера title. JSON-RPC request/notification/response разграничиваются прежними method/ID правилами; native terminal не выводится из текста, session title, assistant delta или task_complete. Ответ всё ещё сопоставляется с ожидающим request ID; FIFO event barrier остаётся перед его доставкой.

Свидетельства в `.scratch/phase-5/reports/acp-terminal-reader-20261010.md` соответствуют этой причине: большой session_info_update остановил reader, а настоящий response `stopReason=end_turn` уже находился в pipe. Диагностика не потребляла исходные байты и не повторяла неизвестный turn. Исправление не меняет auth/profile/native identity и не добавляет retry либо ранний успех.

Invalid JSON, пустые строки и CRLF по-прежнему не становятся terminal; CR остаётся допустимым JSON whitespace. Последний непустой JSON кадр перед EOF обрабатывается, как прежде Scanner. Нулевой результат чтения завершает loop и запускает прежний процесс Wait; Stop по-прежнему освобождает заблокированную доставку событий, а Close отменяет собственный process context. Reader errors раньше также не публиковались через Scanner.Err; новых потерянных ошибок либо изменения их трактовки в этом scoped diff не обнаружено.

Новая regression передаёт реальный большой notification frame, затем настоящий RPC response, оставляя persistent peer живым. Она проверяет полное содержимое события и завершение только после обработки preceding notification. Это соответствует публичному требованию 01 «пустой/error terminal даёт видимую ошибку» и сохранению completion guard: устранена потеря протокольного ответа, guard не ослаблен. По отчёту исходный reader RED, новый GREEN; ACP/Node/Secretary race checks PASS. Широкие tests повторно не запускались.

Неограниченная длина JSONL кадра нужна для реально наблюдавшегося native profile/context; новый искусственный cutoff не добавлен. Общий live MVP PASS по fixtures или этому review не утверждается. Репозиторий, runtime, browser и auth не изменялись.
