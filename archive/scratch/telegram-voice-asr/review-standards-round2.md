# Независимое ревью: стандарты, раунд 2

2026-10-11. Baseline: `8ceaee3a56d45baf532238d8054d881faf4d53e6`. Проверены согласованные изменения Telegram ASR/config и исправления после первого раунда. Несвязанные изменения checkout исключены. Код не изменялся; SSH, API, commit и deployment не выполнялись.

Подтверждённых замечаний по корректности, безопасности и документированным стандартам нет. Правила `AGENTS.md`, `docs/agents/domain.md`, `GLOSSARY.md` и runtime contracts учтены; каталог ADR отсутствует. Оснований добавлять замечания только по эвристикам запахов кода не найдено.

Оба прежних P2 исправлены:

- Форматтер сравнивает симметрично мультимножества обнаруженных чисел и технических токенов. Добавление новых токенов и повторение существующих отклоняются, концевая пунктуация ссылки/пути допускается. Ограничение проверки смысловой эквивалентности явно описано.
- Worker request ID обычного текста сохраняется в durable pending input перед отправкой. Replay использует исходный ID после clearing mapping и restart; новый pending request не стирается повторной доставкой. Следующий текст фиксирует request ID при dispatch, поэтому ответ на уже закрытый вопрос не наследуется из очереди.

Повторно проверены owner/topic до download, лимиты, cleanup, отмена ASR и ожидания слота, один ASR одновременно, FIFO внутри lane и независимые topics, сохранение transcript и idempotency identity, блокировки state/outbox и сохранение прежнего event bridge. Дополнительных воспроизводимых ошибок не найдено.

Проверки: `go test ./internal/telegram ./internal/config -timeout 45s` прошло. Без кэша прошли целевые regression tests для добавленных токенов, replay Worker text после clearing mapping, request binding при dispatch, voice/text FIFO с независимым topic и replay transcript. Полный race/build suite повторно не запускался: его прохождение после исправлений отражено в implementation report. Живой Telegram → local ASR → cloud formatter ещё требует приёмки родителем и пользователем.
