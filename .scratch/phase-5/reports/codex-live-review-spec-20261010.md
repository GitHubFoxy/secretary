# Проверка соответствия Codex MVP

Диапазон: `872a9c2...89d4414c27d3fc080c904288a65ee9903366bf7e`. Найдено 1 замечание P2. Проверка относится к двум исправлениям; полная живая приёмка MVP ещё не подтверждена. Claude Code явно отложен пользователем.

## P2: запоздавший replay заменяет выбранный Telegram/Web turn

Путь: `web/src/App.svelte:145`, зависимый `connectSecretaryStream` на строках 208–225.

Новый периодический refresh может подключить turn B, пока replay turn A ещё выполняется. Функция присваивает `secretaryTurnID` до `await`, а после ожидания без проверки актуальности перезаписывает события и создаёт сокет. Поздний ответ A оставляет выбранную identity B, но показывает события A и слушает A. Следующие refresh видят latest B равным `secretaryTurnID` и не исправляют состояние. Уже созданный сокет B теряет управляемую ссылку; ошибки/события обоих turns могут смешиваться.

Воспроизведено исходной функцией в Node VM с управляемыми двумя replay responses: начать A, начать B, завершить B, завершить A. Получено `selectedTurn: B`, `renderedEventTurn: A`, `socket: .../turns/A/stream/ws`. Репозиторий и runtime не изменялись.

Нарушается критерий 04: «Если canonical reply не появился либо turn failed, пользователь видит ошибку без скрытого retry», а также единый наблюдаемый Web/Telegram диалог. Нужно поколение подключения/проверка после await, привязанные к своему сокету callbacks и защита от устаревшего bootstrap response; добавить проверку наблюдаемого состояния при перестановке ответов.

## Остальной рассмотренный scope

Исторический отсутствующий harness добавляется только как unavailable из валидного immutable Project snapshot; identity Project/Node/HarnessInstance сверяется. Новый Dispatch по-прежнему разрешается через действующий `NodeRecord.Inventory`, поэтому историческая запись не становится доступным runtime. Worker bindings, inventory и глобальные настройки не меняются.

Bootstrap использует `authorizedConversationScope(...ScopeConversationRead)` и scoped conversation query; наружу добавляется лишь turn ID, не private context/profile. Turn replay дополнительно проверяет принадлежность Conversation. Telegram production delivery не переписывался; новый regression проверяет существующий exactly-once pre-Prompt failure при restart. Новые runtime/model/live положительные сценарии не объявлены пройденными по fixtures.
