# Issue #52: typing-индикатор для Worker

Источник: [GitHub issue #52](https://github.com/GitHubFoxy/secretary/issues/52). Исследование просмотрело текущую реализацию; код и issue не менялись.

## Рекомендация

Ограничить первую реализацию Telegram: в репозитории `sendTyping` — не общее событие runtime или WebSocket, а метод Telegram adapter, который вызывает Bot API `sendChatAction` с действием `typing`. Для Worker нужно вести активные попытки по durable lifecycle events, регулярно обновлять typing в forum topic конкретного Worker и прекращать обновление на terminal event. Не связывать периодический typing с `attempt.activity`: activity может долго не приходить, а worker может продолжать работать.

## Существующий путь Secretary

- `Adapter.handleSecretaryEvent` открывает `pending.TurnOpen` на `secretary.turn.queued` и `secretary.turn.started`, закрывает на `secretary.turn.finished`, сохраняет состояние и планирует `Flush` ([`internal/telegram/adapter.go`](../../internal/telegram/adapter.go), `handleSecretaryEvent`).
- `Adapter.Flush` вызывает `sendTyping(owner, 1)`, пока turn открыт или есть ожидаемый текст ([`internal/telegram/adapter.go`](../../internal/telegram/adapter.go), `Flush`). General conversation использует Telegram thread `1`.
- `sendTyping` ограничивает вызовы для пары `chatID:threadID` интервалом в 4 секунды и вызывает `Transport.SendChatAction(..., "typing")` ([`internal/telegram/adapter.go`](../../internal/telegram/adapter.go), `sendTyping`). Транспорт передаёт `chat_id`, действие и, если задан, `message_thread_id` в Bot API `sendChatAction` ([`internal/telegram/http.go`](../../internal/telegram/http.go), `BotAPITransport.SendChatAction`). У этого пути нет отдельной операции остановки: Telegram индикатор истекает сам, а адаптер прекращает новые вызовы.
- Входящие сообщения также сразу вызывают typing в соответствующем контексте: General thread `1` или Worker topic thread ([`internal/telegram/adapter.go`](../../internal/telegram/adapter.go), `deliverTextUpdate`).

## Worker event path и контракты

- Запуск попытки фиксирует durable `worker.started` ([`internal/core/phase4_store.go`](../../internal/core/phase4_store.go), переход в `AttemptActive`). Это пригодный сигнал начала активности, включая случаи, когда harness пока не прислал activity.
- Node activity фиксируется как `attempt.activity` с `WorkerRef` и `AttemptID`; запись отклоняется, если attempt уже terminal ([`internal/core/phase4_store.go`](../../internal/core/phase4_store.go), `RecordNodeActivityReplay`). Tool calls и результаты, разрешения и запросы ввода являются деталями activity, а не полным lifecycle.
- Telegram bridge читает durable event log через `EventsAfterSeq`, преобразует события `telegramEvent` и передаёт их в `HandleDurableEvent` ([`cmd/secretaryd/telegram.go`](../../cmd/secretaryd/telegram.go), `bridgeTelegramEventsOnce`, `telegramEvent`). `worker.started` уже сохраняется как такой же kind; `attempt.activity` отображается в `worker.tool_started`, `worker.tool_finished`, `worker.tool_failed` либо `worker.approval_requested`/`worker.needs_input`.
- `Adapter.handleEvent` обеспечивает topic mapping и отправляет typing перед важным Worker-сообщением, но только непосредственно перед отправкой сообщения. `worker.started` сейчас не считается важным событием; обычный `worker.activity` намеренно не отображается как сообщение в topic ([`internal/telegram/adapter.go`](../../internal/telegram/adapter.go), `handleEvent`, `importantWorkerEvent`; тесты [`internal/telegram/adapter_test.go`](../../internal/telegram/adapter_test.go), `TestWorkerActivityNeverReachesTopics`).
- `ServerClient` используется для входящих пользовательских сообщений и управления Worker; typing идёт через `Transport`, отдельно от серверного API ([`internal/telegram/adapter.go`](../../internal/telegram/adapter.go), `ServerClient`, `Transport`). Поиск по Go и Web-коду не обнаружил другой реализации `sendTyping`; Worker observer WebSocket передаёт activity, но это другой клиентский контракт.

## Предлагаемое поведение и крайние случаи

1. В adapter хранить активные попытки по Worker и Attempt ID. На `worker.started` зарегистрировать попытку, затем периодически вызывать уже throttled `sendTyping` с `TopicMapping.ChatID` и `TopicMapping.ThreadID`.
2. На успешном, ошибочном и отменённом завершении удалить попытку. Очистка должна происходить при обработке terminal event независимо от наличия текста, необходимости уведомления, дедупликации сообщения или успешности доставки финального сообщения.
3. Если у Worker несколько попыток, завершение одной не должно останавливать typing для другой; один Worker должен обновлять только свою topic, а не General thread.
4. Если topic mapping ещё отсутствует или `WorkerRef` пуст, не отправлять typing в случайный чат. Ошибка `sendChatAction` сейчас игнорируется в общем `sendTyping`; это полезно сохранять, чтобы вспомогательный индикатор не ломал доставку результата.
5. Новая версия сохраняет активные попытки и topic mapping в Telegram state. Для старого state без маркера этой функции bridge один раз восстанавливает набор попыток по durable event history от начала до сохранённого `LastEventSeq`, а затем продолжает обычный replay с этого cursor. `EventsAfterSeq` предоставляет сохранённые события без отдельного snapshot/compaction-контракта; восстановление останавливается с ошибкой, если история не достигает сохранённого cursor. При отсутствии topic mapping попытка пропускается, чтобы не отправить typing в неизвестный thread.
6. Для Web observer не вводить новое событие без отдельного требования: текущий issue употребляет Telegram-специфичное `sendTyping`, а web activity stream уже имеет свой контракт.

## Тесты для реализации

- `internal/telegram/adapter_test.go`: `worker.started` запускает typing в правильном `chatID/threadID`; регулярное обновление соблюдает 4-секундный throttle; terminal success/failure/cancel прекращает обновления; параллельные Workers/attempts не влияют друг на друга; Worker typing не уходит в General thread; поведение при отсутствии mapping и дубликатах durable sequence.
- `cmd/secretaryd/telegram_test.go`: преобразование `worker.started` и каждого terminal event сохраняет нужный kind, `WorkerRef` и `AttemptID`, чтобы adapter получал оба конца lifecycle.
- Транспортные тесты: запрос `sendChatAction` включает `message_thread_id` для Worker topic.

## Неопределённость в issue

Если автор имел в виду также Web UI или другие клиенты, нужно дополнить acceptance criteria: назвать client-facing event contract и определить, какой клиент должен рисовать typing indicator. В текущем коде общего события `sendTyping` нет.
