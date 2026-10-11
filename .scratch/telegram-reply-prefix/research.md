# Убрать заголовок темы перед Result в Telegram General

Дата исследования: 2026-10-11. Источники: локальный код, тесты и domain documentation. Runtime и сетевой Telegram в этом исследовании не запускались; по скриншоту нельзя установить фактический Worker или turn, но вид строки совпадает с явным преобразованием в коде.

## Вывод

Строку вида `как будет пачкать по английски ? чтобы было разнве…:` добавляет Channel adapter при зеркалировании terminal Worker Result в General. Она является названием Worker topic, а не частью Result. Минимальное изменение — отправлять в General уже подготовленный `body`, сохраняя существующие destination и identity. Источник добавления: [adapter.go:903](../../internal/telegram/adapter.go#L903). Отправка в Worker topic уже использует только `body`: [adapter.go:895](../../internal/telegram/adapter.go#L895).

## Как возникает строка

1. Bridge превращает `result.accepted` и содержательный `attempt.outcome_recorded` в terminal Worker event. Текст берёт из `text`, `summary`, `result`, `error`, `response`; terminal identity определяет по correlation/turn/result identity: [telegram.go:170](../../cmd/secretaryd/telegram.go#L170), [telegram.go:258](../../cmd/secretaryd/telegram.go#L258), [telegram.go:289](../../cmd/secretaryd/telegram.go#L289).
2. Adapter выбирает существующий Worker topic; `TopicMapping.Title` сохраняется при создании и используется повторно: [adapter.go:914](../../internal/telegram/adapter.go#L914), [adapter.go:941](../../internal/telegram/adapter.go#L941).
3. Для terminal event adapter подготавливает `body = safeText(event.Text)`. Пустой body заменяет подписью статуса. Результат отправляется в topic, затем в General с `topicName("", mapping.Title) + ":\n" + body`: [adapter.go:860](../../internal/telegram/adapter.go#L860), [adapter.go:889](../../internal/telegram/adapter.go#L889).
4. Название может совпадать с началом Task prompt: fallback берёт первую содержательную строку и сокращает до 60 символов с многоточием. При доступном генераторе возможен отдельный короткий title: [topic_title.go:13](../../internal/telegram/topic_title.go#L13), [topic_title.go:61](../../internal/telegram/topic_title.go#L61), [topic_title.go:80](../../internal/telegram/topic_title.go#L80). Дополнительный `topicName` нормализует title и подставляет `Worker` при пустом названии: [adapter.go:1691](../../internal/telegram/adapter.go#L1691).

Текущий production constructor не передаёт `TitleGenerator`, поэтому для Worker creation использует валидный event title либо fallback по Task prompt: [telegram.go:87](../../cmd/secretaryd/telegram.go#L87), [adapter.go:927](../../internal/telegram/adapter.go#L927). Выяснять состояние фактически запущенного deployment для исправления не требуется: General добавляет любой сохранённый title одним и тем же способом.

## Границы изменения

- Исправить только формирование General mirror terminal Result. Идентификатор `terminal:<TerminalIdentity>:general`, topic destination, порядок Topic → General и terminal dedup остаются прежними: [adapter.go:873](../../internal/telegram/adapter.go#L873), [adapter.go:895](../../internal/telegram/adapter.go#L895).
- Не изменять Task prompt, создание/название topics, Secretary profile, title generation prompt, Worker Result или sanitizer. Title generation prompt управляет только названием темы: [title-generation-prompt.md](../../internal/config/defaults/title-generation-prompt.md). Применённый код добавляет заголовок уже после получения model output.
- Короткое подтверждение Secretary и обычные Secretary ответы не проходят этот mirror. Подтверждение отправляется отдельным canonical событием; обычные ответы собираются в batch без Worker title: [adapter.go:790](../../internal/telegram/adapter.go#L790), [adapter.go:1008](../../internal/telegram/adapter.go#L1008), [adapter.go:1089](../../internal/telegram/adapter.go#L1089). Поэтому править batching для удаления этой строки не требуется.
- Не удалять первую строку Result эвристикой: если модель сама пишет заголовок или повторяет вопрос, он остаётся частью пользовательского ответа. Удаляется только автоматически добавляемое название topic и `:\n`.
- Для failed/canceled/offline terminal events тот же mirror использует тот же body. Требование распространяется на них, а пустой текст продолжает показывать существующую подпись статуса: [telegram.go:274](../../cmd/secretaryd/telegram.go#L274), [adapter.go:863](../../internal/telegram/adapter.go#L863).

## Durable delivery, несколько turns и batching

Каждый terminal Result имеет собственную identity, независимо от одинакового текста и постоянного названия topic. General и Topic имеют разные identities. Проверка доставки использует identity, а не text; сохранённый outbox с той же identity не переписывается: [adapter.go:1143](../../internal/telegram/adapter.go#L1143), [adapter.go:1159](../../internal/telegram/adapter.go#L1159), [adapter.go:1199](../../internal/telegram/adapter.go#L1199), [adapter.go:1218](../../internal/telegram/adapter.go#L1218). Последующие Follow-up Results одного Worker должны также поступать без title и ровно по одному в каждый destination; title темы при этом сохраняется.

Решение для обновления существующего deployment: не мигрировать outbox. Уже сохранённые сообщения могут доставиться со старым заголовком, включая продолжение частично отправленного сообщения после restart. Новое формирование General mirror использует только body. Это сохраняет старые byte checkpoints и не рискует повторно отправить ранее подтверждённые части. Существующий `sendMessage` сохраняет текущий pending текст вместо пересоздания сообщения по новой версии кода: [adapter.go:1218](../../internal/telegram/adapter.go#L1218). Такое ограничение нужно явно указать в ticket.

Secretary batching остаётся независимым: он ждёт завершения Secretary turn, затем отправляет накопленный текст через `secretaryBatchMessage`. Bridge читает durable events последовательно и делает Flush на `secretary.turn.finished`: [adapter.go:1043](../../internal/telegram/adapter.go#L1043), [telegram.go:134](../../cmd/secretaryd/telegram.go#L134). Нет необходимости менять интервалы, separators либо смешивать Results с этим batch.

## Тесты и критерии приёмки

Существующие тесты явно закрепляют прежний заголовок; их expectations необходимо обновить:

- `TestTerminalResultMirrorsToGeneralWithWorkerLabel`: Topic и General должны получить `pong`, без `Погода:\n`: [adapter_test.go:233](../../internal/telegram/adapter_test.go#L233). Переименовать тест под новый контракт.
- `TestOutboxSurvivesTransportFailureAndRestart`: оба destinations должны получить `Task`, без `Worker:\n`: [adapter_test.go:574](../../internal/telegram/adapter_test.go#L574).
- `TestLongWorkerResultResumesBothDestinationsAfterRestart`: собранные Topic и General должны совпасть с `safeText(text)`: [delivery_test.go:54](../../internal/telegram/delivery_test.go#L54).
- `TestLongResultTopicThenGeneralCheckpointOrder`: General реконструируется в тот же Result, сохраняя порядок и checkpoint behavior: [long_message_research_test.go:28](../../internal/telegram/long_message_research_test.go#L28).

Проверки после реализации:

1. Короткий terminal Result с title, совпадающим с вопросом со скриншота: General начинается с самого ответа. Topic также получает body; название темы не меняется.
2. Табличная проверка completed/failed/canceled/offline: header отсутствует, имеющиеся body и status fallback сохраняются.
3. Два terminal turns одного Worker с разными identities и одинаковым body: по одному сообщению на destination для каждого turn; replay не создаёт новых сообщений. Существующий title сохраняется и не появляется в General.
4. Два разных Workers с одинаковым body: delivery остаётся независимой по terminal identity; никакой text-based suppression не добавляется.
5. Длинный Result, ошибка на Topic или General chunk, restart и replay: существующие tests сохраняют отправку целого текста по частям без повторов подтверждённых chunks. Проверить limit, Markdown/Unicode и plain fallback существующим пакетом Telegram.
6. Уже сохранённая старая outbox восстанавливается без изменения text/checkpoint; новая terminal identity после restart создаёт General без title.
7. Отдельное подтверждение Secretary и последующий terminal Result: подтверждение остаётся отдельным сообщением, Result без title. Обычные Secretary ответы и batched delta behavior не меняются.

Достаточная автоматическая проверка: `go test ./internal/telegram ./cmd/secretaryd`. Для визуальной проверки можно воспроизвести короткий перевод в Telegram; это дополнение к локальным тестам, а не повод менять prompts или formatter.

Проверка исходного поведения выполнена основным агентом: `go test ./internal/telegram -run 'TestTerminalResultMirrorsToGeneralWithWorkerLabel|TestLongResultTopicThenGeneralCheckpointOrder|TestTerminalOutboxRestartDoesNotReplayAfterDrain' -count=1` → `ok` за 1.190s. Эти три теста подтверждают текущую отправку с префиксом, порядок destination и восстановление outbox. Это baseline до исправления, а не проверка исправленного поведения.

## Domain compatibility

Изменение касается отображения Channel adapter, не server-owned Result и не Personal Conversation. Сохраняет verbatim Result в server и две независимые durable отправки Telegram. Это соответствует [GLOSSARY.md](../../GLOSSARY.md) и [runtime-contracts.md](../../docs/architecture/runtime-contracts.md). `docs/adr/` в текущем checkout отсутствует; конфликтующего ADR не найдено.
