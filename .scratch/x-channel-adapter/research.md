# Исследование: X Channel adapter

**Проверено: 2026-10-12.** Использованы официальные X Developer docs и существующие контракты secretary-v2. API-доступ и реальный аккаунт не тестировались. Цены и policy могут измениться; перед запуском их нужно перепроверить в Developer Console.

## Вывод

Технически MVP можно построить на X API v2: polling ленты упоминаний настроенного аккаунта, приём постов владельца и публикация ответа как reply к исходному посту. X также описывает X Activity API, но polling проще для первого slice и совпадает с polling-first Telegram adapter. Это будет официальная X API интеграция с аккаунтом Secretary, а не продолжение продукта Grok Bot: локальное исследование Grok Bot описывает отдельный продукт xAI/Cursor без заявленного X mention adapter ([разбор Grok Bot](../../docs/research/agent-landscape/grok-bot.md)).

**До публичного включения есть policy gate:** X Developer Guidelines помечают AI-generated replies как требующие предварительного одобрения X. В тех же guidelines сказано, что автоматизированный аккаунт должен быть обозначен и использовать только официальный API. Код можно реализовать и проверять через fake transport, но до одобрения нельзя выполнять никакие реальные AI reply posts, включая тестовый ответ. Перед запуском нужен включённый `Automated` label, указание оператора в bio и связь автоматизированного аккаунта с человеческим аккаунтом. [Developer Guidelines](https://docs.x.com/developer-guidelines)

## X API

- Читать упоминания можно через `GET /2/users/{id}/mentions`. Endpoint принимает `since_id`, cursor pagination и `max_results` от 5 до 100. Mentions timeline ограничена 800 последними Posts, поэтому cursor не восстановит упоминания, которые вышли за это окно во время простоя/всплеска. [Get Users Mentions](https://docs.x.com/x-api/users/get-mentions) · [Timelines](https://docs.x.com/x-api/posts/timelines/introduction)
- Отправлять пост можно через `POST /2/tweets`; в схеме есть `reply` configuration для ответа на пост. Отправку следует ограничить адресованной веткой и не использовать для произвольных публикаций. [Create Posts](https://docs.x.com/x-api/posts/create-post)
- Для доступа от имени аккаунта подходит OAuth 2.0 Authorization Code с PKCE и user context. Документация перечисляет scopes `tweet.read`, `tweet.write`, `users.read`; `offline.access` выдаёт refresh token для длительной работы. Access token без него живёт два часа. [OAuth 2.0 Authorization Code Flow with PKCE](https://docs.x.com/fundamentals/authentication/oauth-2-0/authorization-code)
- Текущая pricing-страница указывает `$0.005` за чтение Post; `$0.001` per resource для Owned Reads, куда входит `GET /2/users/{id}/mentions`, когда `{id}` совпадает с владельцем developer app; `$0.010` за Post Create (summoned), `$0.015` за обычный Post Create и `$0.200` за Post Create (with URL). Чужие упоминания всё равно читаются и могут тарифицироваться. До включения подтвердить категорию тарификации reply и условие Owned Reads в Developer Console: страница предупреждает, что цены меняются. [X API pricing](https://docs.x.com/x-api/getting-started/pricing)
- Актуальный X Activity API поддерживает webhook subscriptions; он может сигнализировать о создании/удалении posts, но сложнее polling. Если не подключать webhook, нужен проверяемый периодический reconciliation сохранённых IDs. [X Activity API](https://docs.x.com/x-api/activity/introduction) · [webhook pricing](https://docs.x.com/x-api/getting-started/pricing)
- Automation Rules требуют opt-in, простой opt-out, соблюдение отказа и максимум один автоматический reply/mention на interaction. Запрещено публиковать личные или конфиденциальные сведения о человеке без предварительного явного разрешения. Значит X turn выдаёт только один финальный публичный reply, без отдельного acknowledgement и цепочки частей. [Automation Rules](https://help.x.com/en/rules-and-policies/x-automation)
- Developer Policy требует удалить или обновить сохранённый X Content при изменении/удалении, а также при переводе в protected/private, suspension или withholding. Прямой запрос X или владельца на удаление требует немедленно запустить очистку; завершить её нужно как можно скорее и не позднее 24 часов с момента получения запроса. Это касается durable Conversation/transcript storage. [X Developer Policy](https://docs.x.com/developer-terms/policy)
- Упоминание в X публично. Поэтому даже ответы только владельцу могут быть видны читателям ветки. Владелец должен понимать, что текст ответа публикуется; чужие авторы до account linking должны быть заблокированы.

## Соответствие репозиторию

- По [GLOSSARY.md](../../GLOSSARY.md) Channel adapter даёт доступ к одной Personal Conversation; сервер остаётся владельцем Conversation, Task, Worker binding и Result. Не создавать отдельную X Conversation или второй Secretary.
- Telegram уже реализует polling, внешний message ID, отдельный credential, состояние adapter и event bridge в `cmd/secretaryd` и `internal/telegram`. `/v1/messages` сохраняет inbound через внешний ID и idempotency key. Эти части можно использовать как поведенческий образец, не копируя Telegram API-specific состояние.
- У Telegram есть отдельная `client:telegram-adapter` identity. X должен получить отдельную identity/credential и не переиспользовать Telegram secret.
- Разрешение входа по одному owner X user ID следует из текущего single-Person ограничения runtime. Нельзя принимать любые публичные mentions в общую Personal Conversation.
- Для ответа X нужен устойчивый origin link от принятого X поста до точного Secretary turn/input и его публичного ответа. Текущего события с «последним сообщением» недостаточно: параллельные inputs и Worker Result могут отправить текст в неверную ветку. Нельзя определять origin по текстовому совпадению.
- Runtime уже описывает `Addressed reply v1`: X adapter должен использовать этот opt-in контракт. Запрещён fallback на legacy event routing без точного origin `input_id`.
- Публичный ответ не должен использовать приватные entries Web/Telegram, локальные файлы, credentials или private Worker Workspace. Нужен проверяемый public context для X turn; приватный запрос должен безопасно отказать и направить владельца в приватный канал. Поскольку runtime/Worker имеет доступ к локальному Workspace, MVP должен отключать Worker для X turn, пока доступ нельзя ограничить и проверить.
- У создания публичного X поста возможен неопределённый исход при сетевом обрыве после принятия запроса. Нельзя слепо повторять такой POST. Нужно хранить состояние попытки и явно показывать неопределённую доставку без секрета.
- `docs/adr/` в checkout отсутствует; отдельного ADR о X adapters или публичных каналах не найдено.

## Рекомендуемый MVP

1. Один настроенный аккаунт X Secretary и один owner X user ID.
2. Явно выключенная по умолчанию интеграция с OAuth user context и минимальными scopes.
3. Polling `GET /2/users/{id}/mentions`; на первом старте установить верхнюю границу и не публиковать ответы на старую историю. Для новых постов сохранять cursor и обработанные IDs. Гарантия ограничена 800 последними mentions; выход cursor за окно сообщать владельцу.
4. Принимать только текстовое явное упоминание целевого аккаунта от owner ID. Обрабатывать entity ranges X, чтобы удалить только сам address mention из prompt.
5. Сохранять вход в общую Personal Conversation, но формировать изолированный публичный context без приватных entries других каналов и private Workspace; не зеркалить сообщения Telegram/Web.
6. Использовать `Addressed reply v1` и публиковать один итоговый reply на interaction. Для длинного ответа вернуть один короткий reply с просьбой запросить сокращение; не создавать цепочку posts и не терять исходник молча.
7. Поддержать простой opt-out, не упоминать третьих лиц без opt-in и обрабатывать удаление/редактирование сохранённого X Content как можно скорее, но не позднее 24 часов после получения запроса.
8. До любой реальной AI reply activity, включая live test reply, получить письменное явное одобрение X; до него разрешены только проверки с fake transport. Перед запуском включить `Automated` label, указать оператора в bio, связать аккаунт с человеческим аккаунтом и подтвердить billing category в Developer Console.

## Открытые решения владельца

- Подтвердить X developer app, OAuth refresh token, handle и X user ID владельца; эти значения нужны для локальной/production настройки, не для написания адаптера.
- Получить/подтвердить предварительное одобрение X на AI-generated replies и выбрать текст маркировки автоматизированного аккаунта.
- Принять публичную видимость ответов и стоимость polling/posting; проверить точный billing category в консоли.
- Перед production определить процедуру для ответа с неопределённым исходом после сетевого сбоя и live acceptance с реальным mention.
