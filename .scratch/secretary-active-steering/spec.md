# Steering активного Secretary

Основание: [инцидент](incident.md). Реализация: [тикет 01](https://github.com/GitHubFoxy/secretary/issues/24). Первичные источники и выбранный дизайн: [исследование](research.md).

## Результат для пользователя

Обычное сообщение в Personal Conversation во время активного Secretary направляет текущую работу на ближайшей безопасной границе native runtime. Оно не ждёт окончания Secretary turn и не создаёт скрытый новый turn. Например, уточнение «i meant record a video» поступает в работающий Codex Secretary до его terminal и влияет на делегирование исходного вопроса.

«Сразу» означает немедленно инициировать steering после durable сохранения и сериализации предыдущего ввода. Это не обещание прервать выполняемый инструмент или немедленно получить ответ модели. Cancel остаётся отдельной операцией.

## Правила маршрутизации

| Состояние Secretary | Ввод | Операция |
| --- | --- | --- |
| Native turn активен и принимает steering | Обычный текст | Сохранить отдельную input identity в текущем Secretary turn; передать через `Session.Steer`. |
| Idle, terminal уже надёжно зафиксирован | Обычный текст | Создать следующий обычный Secretary turn. |
| Не запущен, native prompt ещё не принят, offline или steering unsupported | Обычный текст | Сохранить ввод; явно показать отсутствие применения, не притворяться успешным steering и не превращать его молча в queued turn. |
| Любое состояние | `/q текст` | Явная durable очередь; исполнить после окончания предыдущей работы. |
| Любое состояние | Пустой текст или `/q` без текста | Ошибка валидации. |

Ввод из General адресуется Secretary. Если Secretary idle, но Worker активен, новый Secretary turn решает, кому направить сообщение; сервер не угадывает Worker по последнему Result. `message_worker` для активного Worker использует существующий same-Attempt steering, для idle Worker — Follow-up. Worker topic сохраняет прямую адресацию.

## Сохранность и identity

- Conversation entry создаётся ровно один раз. Её ID и seq проходят в durable Secretary ingress; порядок не определяется запуском HTTP goroutines.
- Каждый ввод получает стабильный server-issued `input_id`. Одинаковый текст с разными входными identities — разные сообщения; replay той же entry — тот же input без повторного native RPC.
- Первоначальные `secretary_turns.input`/`input_id` и context snapshot не переписываются. Дополнительный реестр связывает несколько вводов с одним активным turn и текущей runtime generation.
- Acknowledgement, addressed reply, Worker origin и MCP observer проверяют точное принадлежание input активному turn/generation. Ранние уже принятые операции не теряют identity после уточнения.
- Успешный native final сохраняется одной Conversation entry. Не публиковать один и тот же final повторно ради каждой input identity. Его durable адресация должна учитывать все подтверждённо принятые в этом turn inputs, не утверждая, что модель семантически выполнила каждое требование.
- Для addressed-v1 не ослаблять exact addressed reply guard: каждый принятый input требует своего точного addressed reply. Acknowledgement, Worker Result, неподписанный текст, чужая identity и неявное «последний ответ покрывает всё» не удовлетворяют этот контракт. Addressed shared-reply API не входит в этот тикет; не включать addressed-v1 по умолчанию.
- Не считать все старые сообщения автоматически заменёнными последним: уточнение может содержать дополнительный независимый вопрос.

## Гонки, ошибки, restart

Ввод, binding, steering handoff и terminal commit должны иметь один определённый порядок на Secretary identity. Не держать runtime mutex или SQL transaction во время native RPC: Secretary tools и event reader должны продолжать работать. Native может завершиться в процессе handoff; этот случай проверяется отдельно от server-side состояния.

Receipt `injected` фиксирует native принятие, но не доказывает применение уточнения. `startedNewTurn` не является steering. Ошибка, timeout, потерянный receipt и смена generation имеют явный durable исход и видимое сообщение пользователю; неизвестное принятие не повторяется автоматически и не превращается в очередь. Restart не перезапускает уже отправленные inputs или active turn. Явно queued `/q` сохраняется по существующим правилам восстановления.

HTTP `202 saved` подтверждает сохранение, а не применение steering. Последующие ошибки должны доходить через canonical Conversation entry/event во все адаптеры, даже если HTTP-запрос уже завершился.

## Границы

Использовать существующие Codex ACP runtime и Worker message path. Не добавлять новый harness, автопоиск нужного Worker, cancel+restart вместо steering, переписывание истории, новую модель или изменение production в рамках подготовки тикета. Claude native steering остаётся вне этой приёмки; unsupported обязан быть видимым.
