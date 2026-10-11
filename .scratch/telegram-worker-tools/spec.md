# Вызовы инструментов в Telegram topic Worker

Status: needs-triage

## Проблема

Пользователь сообщает, что в Telegram topic Worker не видны вызовы инструментов. Ожидается компактное отображение Worker activity во время работы, до появления Result.

## Известный контекст

В `internal/telegram/adapter.go` уже предусмотрены события `worker.tool_started`, `worker.tool_finished`, `worker.tool_failed`. `importantWorkerEvent` допускает их отправку, а `renderWorkerEvent` показывает название инструмента и короткий preview с маркерами ▶, ✓ и ✗. Если `Tool` пуст, renderer возвращает пустой текст. Это возможная точка проверки, а не установленная причина.

## Что проверить и исправить

Воспроизвести отсутствие сообщений на фактически используемом Worker и проследить путь native runtime → Execution node → Secretary server → event stream → Telegram Channel adapter → Bot API. Проверить типы событий, заполнение `Tool`, Worker reference, topic mapping, фильтры и ошибки доставки. Установить причину на основании наблюдаемых событий и исправить соответствующее место.

Worker activity должна оставаться в topic соответствующего Worker и не превращаться в Conversation entry или Result. Сохранить порядок доставки, защиту от повторов, обработку restart и существующую доставку Result.

## Задача

[01 — Выяснить и исправить отсутствие вызовов инструментов](https://github.com/GitHubFoxy/secretary/issues/30).
