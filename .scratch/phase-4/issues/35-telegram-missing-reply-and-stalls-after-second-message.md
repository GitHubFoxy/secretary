# 35 Telegram: потеря Addressed reply и отказ запуска Worker

Type: task
Status: needs-triage

## Work

После включения Telegram пользователь сообщил о сбое видимого ответа Secretary и обработки следующих сообщений. Зафиксировать причину и восстановить сквозной путь Telegram → Secretary → Addressed reply → Telegram, не подменяя ответ модели fallback/echo.

## Наблюдение пользователя

1. Пользователь отправляет боту сообщение в Telegram.
2. В верхней панели появляется typing indicator, к пользовательскому сообщению добавляется реакция 👀.
3. Ответ Secretary не появляется. Пользователь ожидает сообщение вроде «Да, одну минуту, сейчас делегирую»; наличие именно такого текста в model output или durable Conversation entry пока не подтверждено.
4. После отправки второго сообщения бот перестаёт отвечать совсем.

Это исходное наблюдение пользователя. Диагностика ниже подтверждает отсутствие доставки ответов, но не зависание второго Secretary turn. Typing и 👀 не доказывают успешную обработку Secretary или доставку Addressed reply.

## Подтверждённые findings — 2026-10-08

Исследованы production journal, durable SQLite events/entries/turns, Telegram state и redacted ACP log на omarchy. Два input со скриншота сопоставлены с Telegram inbound records по entry identity. Все времена — UTC+7.

| Этап | Вопрос о погоде | Вопрос о фильме |
| --- | --- | --- |
| Input сохранён | 10:54:44.346, Conversation seq 171 | 10:55:00.453, Conversation seq 173 |
| `spawn_worker` отклонён | 10:54:49.206, event 7374 | 10:55:03.624, event 7383 |
| Addressed reply сохранён | 10:54:50.886, Conversation seq 172, event 7376 | 10:55:05.139, Conversation seq 174, event 7385 |
| Turn завершён | 10:54:52.069, event 7378 | 10:55:06.202, event 7387 |

### 1. Причина missing reply: bridge пропускает canonical entry

Оба `reply_to_user` успешны. Каждый turn имеет server-issued input identity и ровно одну связанную durable reply. Ответы непустые и сообщают о неудаче запуска Worker; private bodies не публикуются.

`RecordSecretaryReply` сохраняет ответ и публикует `conversation.entry` с текстом в `body`. `cmd/secretaryd/telegram.go:165` (`telegramEvent`) не обрабатывает `conversation.entry`: default обнуляет Kind, а `body` извлекается только для `message.saved`. Adapter не отправляет событие без Kind/WorkerRef, возвращает `nil`, после чего `HandleDurableEvent` продвигает cursor. Ответ не попадает в Telegram outbox.

Оба turns завершены по `addressed_reply_only`: `status=succeeded`, `reply_count=1`, `assistant_chunks=0`, `rpc_succeeded=true`, `drain_completed=true`, `entry_present=true`, `committed=true`, `code=completed`. Terminal events не содержат renderable top-level текста; старый путь через `secretary.text_delta`/terminal text не доставляет эти canonical replies.

Actual Telegram state при инспекции: `last_event_seq=7387`, `secretary_ready=true`, pending text/tools/event sequences и outbox отсутствуют. Cursor прошёл оба reply events. Обычный replay после текущего cursor не вернётся к пропущенным ответам.

### 2. Причина отсутствия Worker Result: несовместимый explicit FX pin

Оба запуска отклонены с ошибкой:

```text
core: invalid dispatch pin: core: requested harness pin is unavailable:
reasoning "low" is not observed by omarchy/fx
```

Первый вызов явно выбирает FX, `gpt-6-luna` и `low`. Второй снова выбирает FX и ту же модель, опуская reasoning; resolver наследует `low` из Worker policy. FX inventory ready, модель присутствует, но `reasoning_levels=[]`. Deployment defaults — OpenCode / `openai/gpt-6-luna` / `low`; ready OpenCode inventory объявляет `low`. Explicit FX selection не переключается на OpenCode автоматически. Новых Workers за исследуемый период создано **0**.

Почему модель выбрала FX, из безопасных логов однозначно не установлено. В context snapshots есть прежние FX bindings и acceptance-контекст; literal FX instance pin в Secretary profile отсутствует. Возможное влияние старого контекста — гипотеза, а не подтверждённая причина решения модели.

### 3. Второй input обработан; зависание не подтверждено

Первый turn завершён примерно за 7,7 секунды, второй — за 5,7 секунды. Второй input поступил после завершения первого. При инспекции active/queued Secretary turns нет. Steering во время active turn, `/q` и дальнейшие inputs этим эпизодом не проверены.

Ранние journal ошибки `createForumTopic` HTTP429 в 10:49:30–10:49:51 относятся к старому event 7333. Cursor позже прошёл исследуемые events до 7387; локальный repro работает без Telegram/rate limit. Этот 429 не объясняет пропуск данных replies. MCP discovery и terminal RPC/drain успешны для обоих turns.

### Воспроизведение и оставшаяся работа

Локальный Go overlay использует настоящие `telegramEvent`, `Adapter.HandleDurableEvent` и `Flush` с fake transport. Четыре запуска дали одинаковый результат:

- Два turns с canonical replies: cursor=6, отправок=0 вместо 2 — **FAIL**.
- Минимальный reply entry + terminal event: cursor=2, отправок=0 вместо 1 — **FAIL**.
- Контроль с прежним `secretary.text_delta`: отправок=1 — **PASS**.

Полный [отчёт, provenance и команда воспроизведения](../reports/35-diagnosis-20261008.md); [диагностический стенд](../reports/35-diagnostic/replay_test.go.txt). Это deterministic repro missing delivery, не live Telegram acceptance и не воспроизведение модельного выбора FX.

Дальнейшая реализация должна отдельно обеспечить доставку canonical Addressed reply и совместимые Worker preferences. Acceptance ниже остаётся невыполненным. По указанию владельца исправления не вносились: production/код/config/state/bindings/auth не менялись, новые Telegram/provider calls не выполнялись. Ticket остаётся открытым.

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

### Диагностика 2026-10-08 — без исправления

По прямому запросу владельца исследованы два Telegram inputs в 10:54–10:55 UTC+7. Оба приняты и завершены Secretary успешно, каждый имеет один canonical Addressed reply. Telegram bridge пропускает их `conversation.entry` и продвигает cursor; отсутствие доставки воспроизведено локально с двумя turns и минимальным replay. Отдельно оба `spawn_worker` отклонены: explicit FX несовместим с reasoning `low` (во втором запросе он наследуется из Worker policy). Workers не созданы. Зависание второго Secretary turn не подтверждено: он завершился за 5,7 секунды. Production/код/config/state не менялись, live calls не запускались, acceptance исправления не выполнен, статус тикета сохранён.

Полный [отчёт и воспроизводимый диагностический стенд](../reports/35-diagnosis-20261008.md).


### Исправление и подготовка развёртывания — 2026-10-08

По запросу владельца исправлена доставка `conversation.entry` с `kind=secretary`: bridge передаёт `body` в существующий путь текста Secretary, который отправляет ответ после завершения turn. Остальные kinds пропускаются, чтобы не возвращать user input и не дублировать Worker Result.

Добавлен `TestTelegramBridgeDeliversCanonicalAddressedRepliesForEveryTurn`: два последовательных turns дают ровно два Secretary replies; user и worker_result entries не отправляются. `go test ./cmd/secretaryd ./internal/telegram` и `git diff --check` прошли.

Владелец поручил закоммитить все изменения, выполнить push и обновить omarchy. Отказ explicit FX/low и причина выбора FX не исправлялись. Реальный Telegram acceptance (input во время active turn и после idle) не выполнен; статус `needs-triage` сохраняется. Если runtime выдаст одновременно text deltas и canonical reply, возможное дублирование требует отдельной проверки. Старые пропущенные события автоматически не переотправляются, поскольку durable cursor уже прошёл их.
