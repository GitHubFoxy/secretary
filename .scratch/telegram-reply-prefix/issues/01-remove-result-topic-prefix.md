# 01: Убрать префикс с названием Worker topic из Result в Telegram General

Type: task
Status: ready-for-agent
Blocked by: None

## Проблема

General показывает сокращённый текст вопроса с двоеточием перед Result: «как будет пачкать по английски ? чтобы было разнве...:». Пользователь требует убрать эту строку. [Скриншот](../evidence/reported-prefix.png), [спецификация](../spec.md), [исследование](../research.md).

## Что сделать

1. В terminal General mirror (`internal/telegram/adapter.go`, сейчас строка 903) заменить `topicName("", mapping.Title) + ":\n" + body` на `body`. Сохранить destination и `Identity`.
2. Обновить и переименовать `TestTerminalResultMirrorsToGeneralWithWorkerLabel`: ожидаются `pong` в обоих destinations, ровно по одной отправке при replay. Также заменить прежние expectations с префиксом в `TestOutboxSurvivesTransportFailureAndRestart` (`adapter_test.go:591`), `TestLongWorkerResultResumesBothDestinationsAfterRestart` (`delivery_test.go:85`) и `TestLongResultTopicThenGeneralCheckpointOrder` (`long_message_research_test.go:84`). При изменении длины General body пересчитать ожидания chunks/fail positions, если fixture от них зависит.
3. Добавить или расширить проверки наблюдаемой доставки для разных terminal identities, restart/retry, пустого Result и длинного/форматированного body. Использовать существующие fixtures и helper; ожидания General должны опираться на Result без префикса.

## Критерии приёмки

- [ ] General Result начинается с самого `body`; перед ним нет добавленного названия topic, `Worker:` или пустой строки. Проверить сокращённый русский Task prompt со скриншота и обычное название.
- [ ] Topic получает прежний body. Terminal success/failed/canceled и Follow-up используют одинаковое правило для General; пустой текст сохраняет существующий fallback label.
- [ ] Если сам Result начинается с названия topic и двоеточия, эта авторская строка сохраняется полностью.
- [ ] Replay той же `TerminalIdentity`, в том числе после restart, не удваивает отправки. Два разных Results с одинаковым body не объединяются.
- [ ] Ошибка отправки General после успешной отправки topic и повторная доставка сохраняют по одному Result в каждом destination; restart с pending outbox не теряет Result.
- [ ] Длинный body корректно проходит splitting/HTML/plain fallback в обоих destinations без добавленного префикса, потери текста и нарушения UTF-16 лимита. Не требовать одинаковых bytes HTML и plain representations.
- [ ] Названия topics, Task prompt, маршрутизация и события без `TerminalIdentity` сохраняют прежнее поведение; profiles, formatter, canonical Result и Web не изменены.
- [ ] `go test ./internal/telegram ./cmd/secretaryd -count=1` проходит.
- [ ] Новый Result после обновления проверен в реальном Telegram: General начинается с ответа, topic сохраняет название и Result. Live evidence записано отдельно от fixtures.

## Comments

2026-10-11: задача подготовлена по скриншоту и исследованию исходного кода. Реализация не выполнялась. Не смешивать с отдельным тикетом форматирования таблиц. Уже отправленные сообщения и старая persisted outbox не переписываются.
