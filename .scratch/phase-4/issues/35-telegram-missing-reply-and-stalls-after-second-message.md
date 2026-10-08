# 35 Telegram: нет ответа Secretary и зависание после второго сообщения

Type: task
Status: needs-triage

## Work

После включения Telegram пользователь сообщил о сбое видимого ответа Secretary и обработки следующих сообщений. Зафиксировать причину и восстановить сквозной путь Telegram → Secretary → Addressed reply → Telegram, не подменяя ответ модели fallback/echo.

## Наблюдение пользователя

1. Пользователь отправляет боту сообщение в Telegram.
2. В верхней панели появляется typing indicator, к пользовательскому сообщению добавляется реакция 👀.
3. Ответ Secretary не появляется. Пользователь ожидает сообщение вроде «Да, одну минуту, сейчас делегирую»; наличие именно такого текста в model output или durable Conversation entry пока не подтверждено.
4. После отправки второго сообщения бот перестаёт отвечать совсем.

Это reported failure, а не независимо воспроизведённый диагностический результат. Исходные тексты сообщений, точные timestamps, chat/Topic и состояние model calls ещё не установлены. Typing и 👀 не доказывают успешную обработку Secretary или доставку Addressed reply.

## Expected

- Принятое сообщение получает видимый Addressed reply от Secretary; при делегировании Worker Result доставляется отдельно по принятому contract.
- Второе сообщение не блокирует Personal Conversation: Steering message или Queued message обрабатывается согласно состоянию runtime и ordered-input contract.
- При ошибке пользователь получает понятный статус, а typing indicator и реакция не остаются единственным свидетельством работы бота.

## Контекст

- Telegram включён по прямому запросу владельца; running process flag и Bot API `getMe` подтвердили включение и действующий бот. Это readiness, не live delivery acceptance.
- До включения Telegram production target `9e3bc1e`, Secretary и новые Workers — OpenCode `openai/gpt-6-luna/low`. Worker Result, Follow-up, Resume после restart и независимый General reply через предыдущий live canary прошли; Telegram-путь этим не доказан.
- Tickets31/33 закрыты по решению владельца. Этот сбой отслеживается отдельно и не считается уже исправленным их реализацией.

## Область диагностики

- Установить, какое из двух сообщений принято Telegram Channel adapter и сохранено Secretary server, и где остановилась обработка каждого input.
- Проверить состояние Secretary turn, input identity, Steering/Queued message и recovery после второго сообщения.
- Разделить отсутствие model reply, отказ addressed-reply contract и сбой доставки уже сохранённой Conversation entry через Telegram outbox/cursor.
- Проверить terminal/failure handling и завершение typing/reaction lifecycle. Не считать индикаторы доказательством успеха и не угадывать причину без evidence.
- Сохранить старые Worker bindings, native sessions и durable историю. Не лечить зависание сбросом DB, replacement session или silent harness/model fallback.
- Использовать безопасные counts/booleans/phase codes и корреляцию identities без вывода токенов, private message bodies, raw ACP, reasoning или private identifiers. Не публиковать raw Bot API URL с токеном.

## Acceptance

- Есть воспроизводимый сценарий первого и второго Telegram inputs и подтверждённая причина для missing reply/зависания; если причины разные, они описаны отдельно.
- На реальном Telegram первое сообщение получает Addressed reply, следующее сообщение тоже обрабатывается без зависания. Проверены сообщения во время active Secretary turn и после idle.
- Каждый input имеет корректную durable адресацию; нет потерянного input, повторного ответа, лишнего Worker или automatic Secretary echo после Result.
- Ошибка runtime/доставки не оставляет канал навсегда занятым: пользователь видит допустимый статус, последующее сообщение можно обработать.
- Regression покрывает выявленный сбой и второй input, включая restart/retry/replay там, где они затрагивают причину. Fixtures и `getMe` не выдаются за actual Telegram PASS.

## Related

Tickets03a/03b — Secretary input queue/context; ticket12 — Telegram Channel adapter; ticket15 — общий acceptance; tickets21/22/29 — оформление и доставка Result; tickets31/33 — завершённый OpenCode runtime/store scope; ticket32 — отдельный UX model/reasoning commands, не предполагаемая причина этого сбоя.

## Comments

Создан по сообщению пользователя. На этапе записи тикета production, код и настройки не менялись; дополнительные Telegram/provider calls и paid model turns не запускались. Диагностика и исправление ещё не начаты.
