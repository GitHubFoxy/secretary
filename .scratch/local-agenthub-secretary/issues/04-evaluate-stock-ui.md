# Evaluate the stock AgentHub UI

Type: grilling
Status: resolved

## Question

После живого proof решить, достаточно ли stock UI для дальнейшей работы. Если нет, назвать конкретный UX gap и решить, нужен ли минимальный frontend fork.

## Answer

Stock AgentHub UI не подходит как user-facing Secretary client. Его оставляем временной операторской и диагностической консолью для тестов.

Конкретные проблемы, увиденные в живом flow:

- Team start/stop и состояние runtime плохо объяснены, статусы запаздывают и противоречат фактическому состоянию процессов.
- History разрезана между channel, Team run и member thread. Пользователь не видит единый task lifecycle.
- Permission fallback может отображаться пустой строкой после timeout.
- Для запуска, просмотра выполнения и follow-up приходится переключаться между несколькими скрытыми панелями.
- UI не выражает основной продуктовый объект: request -> новый worker -> короткий dispatch -> result -> follow-up.

Решение: не делать frontend fork AgentHub. После Secretary bridge строить отдельный минимальный client поверх своего API, а stock UI оставить для operator/debug. Переписывание AgentHub backend или выбор Go не решались в этом ticket и остаются отдельным будущим исследованием.
