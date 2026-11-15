# Немедленное прерывание активного Secretary

Issue: [#24](https://github.com/GitHubFoxy/secretary/issues/24). Исследование: [interruption-research-2026-10-13.md](interruption-research-2026-10-13.md).

## Поведение

Обычное новое сообщение в Personal Conversation, полученное пока Secretary отвечает, немедленно прерывает текущую ACP prompt-операцию. После terminal outcome старой операции Secretary отправляет новое сообщение отдельным `session/prompt` в ту же ACP session. Не использовать `Session.Steer`, не создавать Worker follow-up и не запускать два prompt одновременно. Отмена не отменяет уже совершённые внешние действия или завершённые вызовы инструментов.

Порядок: сохранить Conversation entry и её server-issued ID/seq; зафиксировать durable запрос на прерывание активной generation; отправить `session/cancel`; дождаться результата исходного prompt; закрыть прежний Secretary turn как cancelled или сохранить его естественный terminal, если он выиграл гонку; затем создать и отправить turn для нового input. Новый prompt начинается сразу после безопасного terminal старого.

## Правила входа

| Состояние | Ввод | Результат |
| --- | --- | --- |
| ACP prompt выполняется | Обычный текст | Durable-сохранение, немедленный cancel, затем новый prompt в той же session. |
| Предыдущий prompt уже завершается | Обычный текст | Зафиксировать вход; сериализовать его после фактически победившего terminal outcome. |
| Secretary idle | Обычный текст | Обычный новый Secretary turn. |
| Любое состояние | `/q текст` | Существующая явная durable очередь; не прерывает активный prompt. |
| Любое состояние | Пустой текст или `/q` без текста | Ошибка валидации. |

Если быстро пришло несколько обычных сообщений, сохранить их все с canonical seq и обработать по порядку. Новое сообщение, пришедшее во время prompt предыдущего нового ввода, снова запрашивает отмену текущего prompt. Replay той же Conversation entry не должен отправлять повторный cancel или prompt. Не склеивать сообщения и не терять более раннее сообщение.

Ввод из General адресуется Secretary. В worker topic и явном `message_worker` сохраняется существующая адресация. Новый Secretary turn может сам решить, кому делегировать сообщение; сервер не создаёт Worker follow-up автоматически.

## Сохранность и переходы

- Conversation entry создаётся один раз; её ID и seq передаются durable ingress. Порядок задаётся seq, а не расписанием HTTP goroutines.
- Для каждого input сохраняются неизменяемые input identity, текст, target session и исход доставки. Replay сохраняет прежний outcome.
- Новый input привязывается к собственной Secretary turn identity. Старая turn identity и context snapshot не перезаписываются. Старый turn завершается ровно один раз с terminal исходом.
- Cancel и отправка нового prompt выполняются без удержания runtime mutex или SQL transaction во время native RPC. Одновременно в одной ACP session может выполняться не более одного `session/prompt`.
- `session/cancel` notification сама по себе не подтверждает отмену. Следующий prompt нельзя отправлять, пока исходный prompt не вернул terminal outcome и ACP session не готова к новому prompt.
- Если естественное завершение старого prompt выиграло гонку с cancel, сохранить его terminal результат ровно один раз, затем обработать вход как новый prompt.
- Ошибка/timeout cancel оставляет новый input сохранённым, не запускает параллельный prompt и показывает пользователю видимый статус. Не повторять неизвестно принятый prompt автоматически.
- Restart после durable save должен восстанавливать pending interruption/prompt delivery только при доказуемой идентичности session generation и с защитой от повторной отправки. При неопределённом исходе требуется reconciliation, не слепой replay.
- Если после рестарта невозможно доказать остановку предыдущей ACP generation, пометить старый turn как interrupted/unknown, оставить входы queued и показать владельцу предупреждение. После подтверждения, что прежний процесс остановлен, команда `/recover` закрывает текущую session и продолжает сохранённые входы по FIFO. Повтор `/recover` без незавершённого recovery не должен останавливать работу.
- `HTTP 202 saved` подтверждает сохранение входа, а не отмену или запуск нового prompt. Поздний исход доставки должен быть видимым во всех адаптерах.
- Не ослаблять точную addressed reply проверку. Отменённый turn не является успешным ответом на новое сообщение и не может удовлетворить его reply guard.

## Проверки

- До изменения запустить `.scratch/secretary-active-steering/diagnostic/run.py`; сохранить ожидаемый красный результат.
- Штатные Store/runtime/API-тесты: durable identity/seq, duplicate replay, несколько быстрых inputs, явный `/q`, адресация и visible outcomes.
- ACP adapter тест: notification `session/cancel` отправляется; первый `session/prompt` остаётся активным до `stopReason: cancelled`; второй prompt допускается только после terminal и в той же session.
- Barrier-controlled race tests: cancel против естественного terminal, cancel failure/timeout, duplicate terminal, concurrent ingress ordering, stale generation, restart до/после cancel и prompt acceptance.
- Restart runtime test: при unresolved recovery новый prompt не начинается до остановки старой generation и явного `/recover`; повторная команда не прерывает новую работу.
- Scoped suite и `-race` для concurrent ingress/terminal согласно issue. Native acceptance на закреплённых `codex-acp@1.12.0` и Codex: длинный безопасный prompt прерывается marker-вводом, marker появляется в ответе нового prompt, ACP session ID остаётся прежним. Fake adapter и cancel notification сами по себе не являются native доказательством.

## Изменения по сравнению с предыдущей спецификацией

Новая продуктовая воля заменяет same-turn steering на cancel старого prompt и отдельный новый prompt. Удалены требования доставлять уточнение до terminal того же turn, вызывать `Session.Steer` и не создавать новый Secretary turn. Старый текст сохранён в [superseded spec](spec.md) только как историческая запись.
