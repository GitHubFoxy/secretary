# Perplexity Computer и Personal Computer

Дата проверки: 2026-10-02. Это desk research по официальным страницам, справке и анонсам Perplexity. Продукт не устанавливал и не тестировал. Там, где источник описывает обещание, а не проверяемую гарантию, это отмечено отдельно.

## Краткое резюме

Perplexity Computer сейчас существует в нескольких режимах, которые важно не смешивать:

- **Computer**: облачный исполнитель многошаговых задач, доступный через web и ряд клиентских каналов. Он использует подключённые сервисы, поиск, модели и внутренне делегируемые subagents.
- **Personal Computer**: desktop-приложение Perplexity для доступа Computer к локальным файлам и приложениям. Само по себе оно не означает, что весь агент и обработка данных локальны. Основной сценарий остаётся cloud.
- **Hybrid Compute / Portable Computer**: режимы локального выполнения. На Mac гибрид делит работу между cloud и локальной моделью. Portable Computer запускает большую часть agent runtime на поддерживаемом NVIDIA-железе и может эскалировать в cloud с разрешения пользователя.

Продукт подходит тем, кому нужен облачный исследователь и исполнитель с готовыми интеграциями, а также владельцам Mac, которым важны действия над локальными файлами и приложениями. Для пользователя, которому нужна одна непрерывная беседа с адресуемым persistent Worker из Telegram и терминала, документированного соответствия нет.

По состоянию на дату проверки Computer доступен подписчикам Pro и Max; корпоративные варианты описаны отдельно. Personal Computer представлен как доступный desktop app, но публичная страница установки по-прежнему ориентирована на Mac, тогда как свежая статья справки также называет Windows 10/11. Поддержка Windows не равна паритету функций: управление установленными приложениями пока заявлено только для Mac. Уверенность высокая в различиях режимов и официальных функциях, средняя в актуальном onboarding и правилах непрерывности: документация обновляется неравномерно, hands-on проверки не было.

## Масштаб и перспективы

Perplexity не публикует в проверенных материалах MAU, загрузки Personal Computer, число активных Computer-задач или независимые benchmark-результаты. Аудиторию Perplexity в целом нельзя выдавать за аудиторию Computer. Продуктовые сигналы есть: Computer вышел на всех Pro-подписчиков в марте 2026 года, появился в desktop app и iOS, а Perplexity выпустила интеграции Slack и Teams, локальные варианты и Automations. Это подтверждает расширение доступности и активные инвестиции, но не популярность среди пользователей ([мартовский changelog](https://www.perplexity.ai/changelog/what-we-shipped---march-13-2026), [апрельский changelog](https://www.perplexity.ai/changelog/personal-computer-on-mac-launch-and-computer-updates---april-17-2026), [анонс Automations](https://www.perplexity.ai/hub/blog/computer-adds-automations-for-ongoing-work)).

За весну и осень 2026 Perplexity опубликовала крупные продуктовые обновления: desktop Personal Computer, hybrid compute для Mac, Portable Computer и Automations. У закрытого продукта нет применимых GitHub stars. В проверенных источниках не обнаружен официальный репозиторий исходного кода Computer; сторонние форки и их звёзды не являются показателем аудитории Perplexity.

## Установка и первый полезный результат

**Cloud Computer.** Не требует установки отдельного агента: открыть Computer из Perplexity в web либо доступного приложения, войти в аккаунт, создать задачу и при необходимости подключить сервисы в разделе Connectors. Точная доступность интеграций зависит от тарифа и аккаунта. В актуальном changelog Computer указан для Pro; Max получает ежемесячные кредиты и более высокие лимиты ([описание Computer](https://www.perplexity.ai/help-center/en/articles/13837784-what-is-computer), [тарифное обновление](https://www.perplexity.ai/changelog/what-we-shipped---march-13-2026)).

**Personal Computer на Mac.** Официальный setup предлагает скачать Perplexity for macOS, установить Comet для веб-задач, разрешить Accessibility, Screen & System Audio Recording и Full Disk Access, выбрать доступные папки, подключить нужные сервисы и спарить телефон. Разрешения широкие: до выдачи Full Disk Access разумно проверить, какие папки и действия действительно нужны. [Setup guide](https://www.perplexity.ai/personal-computer-setup) не даёт оснований считать эти шаги проверенными мной. Требование на странице установки: macOS 15 или новее; свежая [справка Personal Computer](https://www.perplexity.ai/help-center/en/articles/14659663-what-is-personal-computer.html) указывает также Windows 10/11, но не полное равенство функций. Отдельная страница установки пока говорит только о Mac.

**Локальное выполнение.** Hybrid Compute на Mac включается в обновлённом Mac app и использует локальную модель для приватных файлов и действий, а cloud-модели для планирования и веб-поиска. Portable Computer предназначен для Pro/Max и поддерживаемого оборудования, включая DGX Spark и ПК с NVIDIA RTX с достаточной VRAM; это не одно и то же, что предоставить облачному Computer доступ к папке. Свежая официальная статья описывает Windows, Linux и локальный runtime, но подробные шаги setup не удалось надёжно извлечь в этой проверке. Поэтому команды установки здесь не приводятся. [Hybrid Compute](https://www.perplexity.ai/hub/blog/introducing-hybrid-compute-on-mac), [Portable Computer](https://www.perplexity.ai/help-center/en/articles/20260915-what-is-portable-computer).

Практичный первый prompt для cloud: «Сравни три публичные вакансии по требованиям, формату работы и зарплате. Дай ссылки на первоисточники и таблицу различий». Ожидаемый по документации результат: поиск, синтез с цитатами и возможность уточнять задачу в Computer. Для локального варианта первый prompt мог бы попросить разобрать файлы из выбранной папки и подготовить сводку. Это реконструкция ожидаемого пути, не наблюдённый результат. Perplexity не публикует обоснованную оценку времени до первого результата.

Трение: разрешения macOS, установка Comet для полноценного браузерного сценария, связка нужных аккаунтов и непредсказуемое потребление кредитов. Время, стоимость и качество зависят от сложности запроса и интеграций.

## Ежедневный UX и возможности

Доступны web, desktop, мобильное приложение и рабочие каналы Slack; changelog также описывает iOS, а документация отдельной интеграции есть для Microsoft Teams. В Personal Computer есть плавающая панель/горячая клавиша, голосовой ввод, управление локальными приложениями на Mac, Comet, локальные файлы и запуск задачи с iPhone. Продуктовая страница обещает продолжение работы на Mac mini, пока пользователь вне дома. [Страница Personal Computer](https://www.perplexity.ai/personal-computer), [справка Slack](https://www.perplexity.ai/help-center/en/articles/14016915-using-perplexity-in-slack.html), [справка Teams](https://www.perplexity.ai/help-center/en/articles/14855210-using-perplexity-in-microsoft-teams).

Документация говорит о сохранении контекста между сессиями и каналами, а Slack-бот позволяет продолжить разговор в DM или group chat. Но источники не подтверждают одну общую Conversation с единым глобальным порядком сообщений между каналами. Нет подтверждённого терминального клиента для Pi или Telegram-интеграции. Для cloud-задач заявлена фоновая работа; для локальной задачи нужен доступный хост. Что именно происходит с активной сессией при перезапуске приложения, потере сети или выключении локального компьютера, документация не уточняет.

| Возможность | Что описано для пользователя | Статус и источник |
|---|---|---|
| Память и персонализация | Память между сессиями, предпочтения, персональный cloud sandbox. | Подтверждено как функция документации, механизм просмотра, редактирования, экспорта и удаления памяти не описан. [Computer Help](https://www.perplexity.ai/help-center/en/articles/13837784-what-is-computer) |
| Skills и обучение | Предлагаются готовые и пользовательские Skills; можно давать дальнейшие инструкции в ходе задачи. | Наличие заявлено; формат переносимых skill-файлов и долговременное обучение пользователя не подтверждены. [Мартовский changelog](https://www.perplexity.ai/changelog/what-we-shipped---march-13-2026) |
| Browser/computer use | Поиск и браузерная автоматизация; Personal Computer взаимодействует с Comet и приложениями Mac. | Подтверждено описанием продукта; надёжность действий не тестировалась. [Setup](https://www.perplexity.ai/personal-computer-setup) |
| Connectors и MCP | Сотни облачных интеграций; Portable Computer может использовать local MCP. API credentials можно сохранить отдельно от trajectory. | Connectors заявлены; точные функции каждого зависят от интеграции. API keys, не SSH keys. [Custom credentials](https://www.perplexity.ai/help-center/en/articles/20260716-using-custom-api-credentials-in-computer), [Portable](https://www.perplexity.ai/help-center/en/articles/20260915-what-is-portable-computer) |
| Код и файлы | Создание приложений и документов; cloud sandbox; у Personal Computer есть доступ к выбранным локальным файлам для чтения/записи. | Документировано, но переносимость результата и формат runtime не унифицированы в источниках. [Computer](https://www.perplexity.ai/products/computer) |
| Поиск и исследование | Параллельное исследование веба, сравнение источников и цитирование. | Подтверждено официальными описаниями, без опубликованного здесь независимого benchmark. [Computer Help](https://www.perplexity.ai/help-center/en/articles/13837784-what-is-computer) |
| Расписания и триггеры | Automations запускаются по расписанию или событиям в Gmail, Slack, Outlook, Linear, GitHub; помнят предыдущие запуски. | Анонсировано 29 сентября 2026 года. Более старая статья Scheduled Tasks говорит о новом изолированном агенте без предыдущего контекста на каждом запуске. Документация расходится, свежий анонс описывает Automations как замену. [Automations](https://www.perplexity.ai/hub/blog/computer-adds-automations-for-ongoing-work), [Scheduled Tasks](https://www.perplexity.ai/help-center/en/articles/11521526-perplexity-tasks) |
| Длительные задачи и совместная работа | Фоновые задачи, Slack и Teams, итерации; product page обещает выполнение задач «hours or months». | Фоновая работа документирована; длительность в часах/месяцах это обещание, не подтверждённый SLA. |

## Делегирование и контроль задачи

Computer описывает внутренние subagents для исследований, финансовых задач, мониторинга и координации; промпт разбивается на этапы, а специализированные агенты выполняют части работы. Это внутреннее делегирование Computer, а не пользовательский каталог постоянных Workers. Документация не показывает, как создать Worker с отдельной постоянной identity, адресовать именно его, посмотреть его binding или отправить ему Follow-up так, чтобы новая работа продолжила прежнюю runtime session. Automations являются повторяющимися назначениями с памятью между запусками по новому анонсу, но это не подтверждает persistent worker на произвольный Task.

Пользователь может направлять и уточнять Computer, пока он работает; каналы Slack и Teams поддерживают диалог и итерации. Подробный пользовательский интерфейс прогресса, отмены, steering, runtime activity и восстановления после прерывания в изученных источниках не определён. Нельзя приравнивать «Computer uses subagents» к возможности управлять несколькими постоянными агентами.

Контроль безопасности включает локальные системные разрешения, подтверждение чувствительных действий и изолированную среду выполнения по описанию продукта. Hybrid Compute заявляет локальный privacy gate: он может оставить данные на устройстве, скрыть детали, отказать в отправке в cloud или запросить согласие. Для API credentials Perplexity указывает шифрование и secure proxy, чтобы ключ не попал в trajectory или sandbox; поддерживаются API credentials, а не произвольные секреты вроде SSH key ([справка по credentials](https://www.perplexity.ai/help-center/en/articles/20260716-using-custom-api-credentials-in-computer)). Это описания поставщика, не независимый аудит границ sandbox. Процесс повторного подключения, отмены и автоматического восстановления cloud task после сбоя не подтверждён.

## Стоимость, приватность и переносимость

Computer использует кредиты. Help Center указывает ориентир 100 credits = $1, стоимость зависит от сложности, а баланс и auto-refill доступны на странице usage. Извлечённая версия статьи содержит разные ориентиры для «light» task во вводном абзаце и таблице, поэтому не использую оценку цены отдельной задачи как надёжную. Мартовский changelog говорит о ежемесячных кредитах и повышенном лимите для Max; точную актуальную цену подписки, объём кредитов для конкретного региона и текущие лимиты нужно сверять в аккаунте. Локальная работа, выполненная моделью на устройстве, по документации не тратит Computer credits. [Правила кредитов](https://www.perplexity.ai/help-center/en/articles/13838041-how-credits-work-on-perplexity), [гибридный режим](https://www.perplexity.ai/hub/blog/introducing-hybrid-compute-on-mac).

Computer закрыт и работает как сервис Perplexity. Self-hosting для cloud Computer не описан. Для cloud-режима данные обрабатываются в сервисе и заявлен изолированный sandbox; Personal Computer даёт локальный доступ, но без включения локальной модели это не означает локальную обработку всего содержимого. Hybrid и Portable заявляют локальное исполнение с возможной отправкой отдельных шагов в cloud после согласия. Общая [Privacy Policy](https://www.perplexity.ai/hub/legal/privacy-policy) датирована 2 февраля 2025 года и не даёт продуктовых деталей о сроках хранения Computer memory, регионе хранения, экспорте Personal Computer, переносе состояния к другому провайдеру или способе удалить отдельный Worker. По этим пунктам ответ: неизвестно.

## Проверка сценария пользователя

| Шаг | Что подтверждено | Что не подтверждено |
|---|---|---|
| Telegram: исследовать три вакансии | Computer умеет web research; можно попросить таблицу и цитаты. | Telegram-канала нет в изученной документации. Ближайшие альтернативы: web, iOS, Slack или Teams. |
| Терминал: продолжить кодовый Task | Computer способен создавать код и работать с файлами; Personal Computer может дать доступ к папке. | Подключение Pi terminal, терминальная Conversation и передача cloud Task в Pi не описаны. Нет основания считать, что продолжится та же session. |
| Telegram: статус и уточнение | Computer принимает уточнения в поддерживаемых диалоговых каналах. | Не подтверждены Telegram, единая Conversation с терминалом и доступ к прогрессу конкретного persistent Worker. |
| Закрыть ноутбук | Cloud Computer и Automations заявлены как фоновые; Mac mini можно оставить работающим постоянно. | На выключенном/спящем локальном хосте Personal/Portable Computer продолжит локальную задачу или нет, не уточнено. Для Mac mini это отдельная постоянно включённая машина. |
| Вернуться завтра | Заявлена память между сессиями; Automations сохраняет историю предыдущих запусков. | Не доказаны общая Personal Conversation, сохранённый Worker binding, восстановление той же runtime session и адресуемый Follow-up. |

## Преимущества и слабые стороны

Сильная сторона Computer в том, что облачный поиск, подключённые сервисы, генерация артефактов и расписания собраны в одном интерфейсе. Personal Computer добавляет конкретный локальный доступ, а Hybrid/Portable различают «агент видит мои локальные файлы» и «модель/agent runtime реально работает на устройстве». Slack и Teams снижают порог входа для команд.

Слабая сторона для сценария Secretary не в отсутствии multi-agent pipeline, а в непрозрачном объекте долгой работы. Документы говорят о контексте, сессиях и Automations, но не о публичной сущности Task с явным Worker binding и гарантированным follow-up. Нет Telegram или терминального канала. Стоимость облачного исполнения кредитная и изменяемая; локальный режим требует совместимого компьютера, разрешений и, вероятно, оставленного включённым хоста. Отзывы пользователей не использованы как доказательство качества: исследование опирается на первичные источники и не было hands-on.

## Выводы для Secretary

Это не готовая спецификация реализации, а продуктовые гипотезы:

1. **Показывать непрерывную Conversation отдельно от каналов.** Сценарий пользователя требует именно общей истории и маршрутизации, а у Computer межканальная непрерывность заявлена общими словами, не как явный контракт.
2. **Давать видимую адресацию persistent Worker и Follow-up.** Внутренних subagents недостаточно, если пользователь хочет вернуться к тому же кодовому Task из другого места.
3. **Разделить состояние Task и живой runtime.** Показывать Task, Worker binding, Attempt, последний Result и то, что будет после перезапуска, а не обещать «работает в фоне» без объяснения последствий.
4. **Сделать состояние выполнения доступным в Telegram и терминале.** Computer показывает ценность web/mobile/Slack/Teams, но не покрывает эти два канала пользователя и не доказывает общий журнал между ними.
5. **Обозначать границы исполнения до подтверждения.** Cloud, локальный доступ к файлам, гибрид и полностью локальный runtime должны быть разными понятными режимами с явным показом стоимости и того, какие данные покидают устройство.

## Открытые вопросы и будущий hands-on

Неясно, как именно устроены общие сессии между web, iOS, Slack и Teams; сколько времени живёт cloud task и что происходит после сбоя; можно ли отменять и возобновлять конкретную задачу; как экспортировать или удалить memory; как Automations мигрировали со старой модели Scheduled Tasks; какова фактическая цена задач и на каких регионах/планах доступны local modes.

Короткий тест после получения легального доступа: подключить Slack и iOS, создать исследовательскую задачу и уточнить её с обоих каналов; проверить, совпадают ли история и task identity. Затем запустить кодовую задачу в Personal Computer на выбранной папке, перезапустить app/хост и проверить сохранение runtime и возможность продолжить ту же задачу. Отдельно прогнать cloud, hybrid и Portable с контрольным файлом, посмотреть согласия перед отправкой данных, отмену, уведомления, логи активности и фактический расход credits. Ни один из этих тестов здесь не выполнялся.

## Основные источники

- [Computer, продуктовая страница](https://www.perplexity.ai/products/computer), без даты на странице, проверена 2026-10-02.
- [Computer Help Center](https://www.perplexity.ai/help-center/en/articles/13837784-what-is-computer), проверена 2026-10-02.
- [Personal Computer, страница продукта](https://www.perplexity.ai/personal-computer), проверена 2026-10-02.
- [Personal Computer Setup](https://www.perplexity.ai/personal-computer-setup), проверена 2026-10-02.
- [Personal Computer Help Center](https://www.perplexity.ai/help-center/en/articles/14659663-what-is-personal-computer.html), проверена 2026-10-02.
- [Portable Computer Help Center](https://www.perplexity.ai/help-center/en/articles/20260915-what-is-portable-computer), проверена 2026-10-02.
- [Computer credits](https://www.perplexity.ai/help-center/en/articles/13838041-how-credits-work-on-perplexity), проверена 2026-10-02.
- [Scheduled Tasks](https://www.perplexity.ai/help-center/en/articles/11521526-perplexity-tasks), проверена 2026-10-02.
- [Automations for ongoing work](https://www.perplexity.ai/hub/blog/computer-adds-automations-for-ongoing-work), 2026-09-29.
- [Hybrid Compute on Mac](https://www.perplexity.ai/hub/blog/introducing-hybrid-compute-on-mac), 2026-09-01.
- [Personal Computer on Mac launch and Computer updates](https://www.perplexity.ai/changelog/personal-computer-on-mac-launch-and-computer-updates---april-17-2026), 2026-04-16.
- [Computer for Pro subscribers, Slack and Personal Computer](https://www.perplexity.ai/changelog/what-we-shipped---march-13-2026), 2026-03-12.
- [Using Perplexity in Slack](https://www.perplexity.ai/help-center/en/articles/14016915-using-perplexity-in-slack.html), updated 2026-05-01.
- [Using Perplexity in Microsoft Teams](https://www.perplexity.ai/help-center/en/articles/14855210-using-perplexity-in-microsoft-teams), updated 2026-08-14.
- [Custom API credentials in Computer](https://www.perplexity.ai/help-center/en/articles/20260716-using-custom-api-credentials-in-computer), проверена 2026-10-02.
- [Privacy Policy](https://www.perplexity.ai/hub/legal/privacy-policy), last updated 2025-02-02.
