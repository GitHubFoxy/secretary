# Повторная проверка соответствия Codex MVP

Проверен диапазон `89d4414c27d3fc080c904288a65ee9903366bf7e...fba6771`, исправление `99d7621`, и прежнее замечание из `p5-codex-live-review-spec.md`. Новых либо оставшихся замечаний по этому изменению: 0. Это scoped review Web подключения; живая Codex/Telegram приёмка и отдельно исследуемый ACP terminal hang не объявляются пройденными.

Прежний P2 закрыт. `web/src/App.svelte` теперь увеличивает generation до закрытия прежнего сокета и проверяет её после replay await. Поэтому последовательность begin A → begin B → resolve B → resolve A не может заменить события/сокет B ответом A. Устаревшее исключение replay также не изменяет ошибку выбранного turn.

Callbacks привязаны к собственному socket. Старый onerror закрывает только старый socket; onmessage/onclose и уже запланированный reconnect проверяют generation. Это защищает также reconnect одной и той же turn identity: одного сравнения ID ранее было недостаточно.

При смене turn очищается прежний stream и ошибка. При reconnect того же turn прежние события остаются видимыми до успешного replay; текущая ошибка replay показывается явно. Новая identity не получает terminal error предыдущей identity. Существующее правило canonical entry/terminal suppression в `SecretaryStream.svelte` не менялось.

Новый `refreshRequestGeneration` отбрасывает запоздавший bootstrap более раннего refresh. Сохранённое до await поколение stream не позволяет bootstrap, начатому до явного переключения пользователя, вернуть UI на прежний turn. Следующий независимый refresh продолжает обнаруживать настоящие Telegram turns.

Четыре новых проверки используют исходные пользовательские функции и управляемые HTTP/Socket ответы; проверяют итоговую turn identity, отображаемые события, выбранный socket, ошибки и callbacks, включая перестановку bootstrap responses. В parent round сообщены 18 PASS Web tests; широкие тесты повторно не запускались.

Исторические snapshot reconstruction, fresh Dispatch fail-closed и scoped bootstrap/replay privacy в этом диапазоне не изменились. Прежний положительный вывод по этим частям остаётся в силе. Репозиторий, runtime, auth и браузер не изменялись.
