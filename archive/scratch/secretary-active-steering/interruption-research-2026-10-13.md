# Исследование: немедленное прерывание активного Secretary

Дата: 2026-10-13. Issue: [#24](https://github.com/GitHubFoxy/secretary/issues/24). Это уточнение владельца заменяет выбранный ранее steering-дизайн. Файл `spec.md` и production-код не изменялись.

## Вывод

Обычный вход во время активного ответа должен иметь семантику прерывания: сохранить вход, отменить текущий ACP prompt, дождаться terminal outcome исходного `session/prompt`, затем немедленно отправить сохранённый текст отдельным `session/prompt` в ту же ACP session. Это новый пользовательский запрос, а не steering, очередь `/q` или Worker follow-up. Не вызывать `_session/steering` и не создавать Worker follow-up автоматически.

Фраза «само сразу обрабатываться» слегка двусмысленна: её можно понять как немедленный параллельный prompt, но ACP session не задаёт безопасный параллелизм prompt-вызовов. Разумная трактовка — вход durable-сохраняется первым действием, затем cancel отправляется немедленно, а новый prompt начинается сразу после подтверждённой отмены/другого terminal исхода старого prompt. Никакой дополнительной смысловой задержки или Worker follow-up нет.

ACP отмена не откатывает уже исполненные инструменты или внешние эффекты. Контракт может прервать дальнейшую генерацию, но не обещает undo уже завершённых действий. «Текущий ответ модели» в этом документе означает весь текущий ACP prompt turn, включая инструментальные операции.

## Первичные источники и текущий код

- ACP v1 [Prompt Turn](https://agentclientprotocol.com/protocol/v1/prompt-turn): клиент отправляет `session/cancel` как notification; агент должен прекратить запросы/инструменты как можно скорее и ответить на исходный prompt запрос с `stopReason: cancelled` после abort незавершённых операций и отправки pending updates. Сам cancel notification не подтверждает завершение отмены.
- Проект устанавливает `@agentclientprotocol/codex-acp@1.12.0` в [secretary](../../secretary#L353). Проверен опубликованный пакет ровно этой версии (`npm pack`), а не поведение нового upstream. В его `dist/index.js` prompt-handler преобразует прерванный Codex turn в ACP `stopReason: "cancelled"` и cancelled outcome; Codex turn interrupt завершается до возврата результата. Исходный код закреплённого тега: [codex-acp v1.12.0](https://github.com/agentclientprotocol/codex-acp/tree/v1.12.0).
- Repo ACP adapter уже посылает notification `session/cancel` через `Session.Cancel` и разбирает `stopReason: cancelled` в `Result.Status == "canceled"`: [ACP adapter](../../internal/node/acp_runtime.go#L850), [prompt result mapping](../../internal/node/acp_runtime.go#L1712). `Session.Prompt` отвергает повторный вызов, пока `busy`; значит следующий prompt обязан ждать окончания предыдущего запроса и `finishTurn`.
- Durable `Runtime.HandleMessage` сейчас безусловно вызывает `QueueMessage` и `startNextDurable`; активный turn тем самым откладывает обычный ввод. `consumeRuntimeResult` фиксирует `canceled` terminal, сбрасывает активное состояние и запускает очередь. Эти ветви требуют новой классификации «cancelled because superseded by durable user input» против явной отмены, если существующая модель turn state этого различия не хранит: [HandleMessage](../../internal/secretary/runtime.go#L205), [terminal consumer](../../internal/secretary/runtime.go#L300), [durable dispatcher](../../internal/secretary/runtime.go#L577).
- HTTP уже durable-сохраняет Conversation entry через `AppendInbound`, затем вызывает `HandleMessage` в независимой goroutine и отвечает `202 saved`: [message ingress](../../internal/webapi/server.go#L330). Это сохраняет текст, но не атомарно связывает entry identity/seq с delivery command, не гарантирует порядок goroutine и не даёт долговечного состояния cancel/re-prompt. Требуется закрепить вход в durable dispatcher до ack или обеспечить восстановление pending work по сохранённой entry. Текст нельзя терять при restart; неизвестный результат отправленного prompt нельзя слепо повторять.
- Предыдущий [spec](spec.md) описывает немедленный `Session.Steer`, запрещает cancel+новый prompt и называет второй Secretary turn скрытой очередью. Эти положения конфликтуют с новой волей владельца, а значит Issue #24 и spec требуют отдельного главного решения/пересмотра; здесь spec не переписан.

## Минимальный delivery contract

1. Каждая canonical Conversation entry имеет неизменяемые `entry_id`/`seq` и одну durable delivery identity. Replay той же entry не запускает второй cancel или второй новый prompt. Вход сохраняется до ответа `202`.
2. При idle-входе сохранить обычный durable Secretary turn и отправить `session/prompt` как сейчас.
3. При активном prompt для обычного текста зафиксировать `interrupt_requested` для конкретных активных turn/session generation, затем отправить ровно один `session/cancel`. `/q текст` остаётся явной FIFO-командой и не отменяет активную работу.
4. Не отправлять prompt нового входа, пока вызов исходного `session/prompt` не закончен и подтверждён его terminal outcome. `stopReason=cancelled` закрывает старый durable turn как прерванный/замещённый; затем создать новый turn для сохранённого ввода и отправить его в ту же ACP session. Если старый запрос успел нормально завершиться, зафиксировать его terminal и после этого обработать новый ввод обычным следующим turn.
5. Новая работа ограничена исходным Secretary ingress. Не вызывать `Steer`, не создавать Worker command/follow-up; Secretary получает обычный prompt и сам решает, нужны ли дальнейшие действия. Повторные обычные inputs во время cancel сериализуются по canonical seq: первое сообщение запускается следующим, последующие могут каждое стать отдельным запросом после предыдущего terminal. Не объединять сообщения молча.
6. Cancel не является транзакцией отмены tool side effects. Логировать/показывать отменённый turn отдельно от нового ответа, не публиковать его partial output как успешный final и не обещать откат уже завершённых действий. Новому prompt дать собственную input/turn identity; сохранить историю и snapshot согласно существующим durable правилам.
7. Хранить состояния как минимум `saved/pending`, `cancel_requested`, `old_prompt_terminal(cancelled|completed|failed|unknown)`, `new_prompt_started`, `delivered|failed`. После crash `cancel_requested`/terminal неизвестен — reconcile с живым session, если возможно; иначе показать interrupted/unknown и сохранить вход для явного восстановления. Не пересылать prompt автоматически, если неизвестно, был ли он уже принят.

## Гонки и ожидаемый исход

| Событие/гонка | Обязательное поведение |
|---|---|
| Вход зафиксирован, активный prompt есть | Один cancel для зафиксированной generation; вход остаётся durable до завершения цепочки. |
| Старый prompt завершился до обработки cancel | Terminal старого prompt побеждает; не посылать cancel повторно после фиксации terminal; запустить сохранённый ввод следующим prompt той же session. |
| Старый prompt завершается одновременно с cancel | Сериализовать commit terminal и планирование нового prompt. Допустим outcome `completed` или `cancelled`, но ровно один старый terminal и ровно один следующий prompt. |
| ACP прислал `cancelled` до того, как cancel вызов/receipt обработан | Привязать terminal к исходному request ID/generation; не путать с новым turn и не отменять следующий prompt. |
| Cancel отправлен, ACP prompt request ещё pending | Ждать ответ исходного prompt, не запускать второй prompt параллельно. |
| Cancel notification потеряна / ACP завершился с ошибкой | Старый результат становится `unknown` либо явной ошибкой; durable вход сохранён, не помечать как доставленный и не повторять автоматически при неопределённом принятии. Видимый retry/recovery должен использовать исходную input identity и предотвращать дубликат после reconciliation. |
| Turn завершился между проверкой active и RPC cancel | RPC может быть no-op; terminal response решает гонку. Следующий ввод всё равно обрабатывается ровно один раз. |
| Повторный webhook/HTTP replay | Та же entry identity возвращает прежний durable outcome, без второго cancel/prompt. Одинаковый текст с новым entry ID — отдельный ввод. |
| Несколько inputs до cancelled outcome | FIFO по canonical entry seq; сохранить каждый текст отдельно. Не объединять и не превращать в `/q` без явной продуктовой политики. |
| Restart после durable save до cancel | Восстановить pending interruption и начать cancel, если точное active session generation ещё подтверждено; иначе сохранить вход и показать interrupted/needs recovery. |
| Restart после cancel до terminal old prompt | Не считать cancel подтверждённым. Reconcile с живой session; не отправлять следующий prompt, пока нельзя исключить конкурирующий активный prompt. |
| Новый prompt принят, процесс упал до receipt persistence | Исход доставки unknown; не слепо отправлять заново. Сверить canonical session history/native request state либо показать восстановление оператору. |
| Инструмент завершился до обработки cancel | Побочный эффект сохраняется; отмена блокирует только дальнейшую работу насколько это поддерживает runtime. Не повторять старый action через новую подачу автоматически. |
| Устаревший terminal старой generation пришёл после начала новой | Terminal коррелируется по request/generation; не сбрасывает `busy`/active turn нового prompt. |

## Обязательные доказательства реализации

- Тест настоящего ACP adapter: `session/cancel` notification послана, prompt API остаётся занят до original `session/prompt` response `cancelled`, затем второй prompt разрешён в той же session.
- Тесты barrier-controlled race: completion-before-cancel, cancel-before-completion, duplicate terminal/entry, concurrent ingress seq, cancel error/timeout, stale generation, crash/reopen до и после каждого durable перехода.
- Runtime/Store тест: старый Secretary turn закрыт ровно один раз, новый input имеет свой durable turn, входы не теряются и не дублируются, Worker follow-up отсутствует.
- Native acceptance с закреплёнными `codex-acp@1.12.0` и установленным Codex: длинная безопасная генерация прерывается marker-вводом, marker появляется в ответе нового prompt в той же ACP session; prompt старого turn завершён `cancelled`, prompt нового turn — отдельный request. Тест не должен утверждать отсутствие уже исполненных до cancel инструментов.

## Влияние на Issue #24

Это смена поведения по продуктовой воле владельца, не корректировка старой реализации в рамках старой спецификации. Перед кодовой работой основной агент должен заменить критерии о same-turn steering/отсутствии второго Secretary turn на interruption + следующий prompt, определить отображение отменённого ответа пользователю и решить политику серии быстрых вводов. Этот исследовательский файл фиксирует минимальное безопасное предположение: отменённый ответ не показывается как успешный, входы выполняются FIFO отдельными prompt, уже совершённые side effects не откатываются.
