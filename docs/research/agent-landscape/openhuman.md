# OpenHuman: продуктовый отчёт

Дата проверки: 2 октября 2026 года. Источники проверены по официальному сайту, документации GitBook и GitHub; hands-on теста не было. OpenHuman прямо помечает себя как early beta и предупреждает о шероховатостях. Уверенность высокая по установке, каналам, памяти, стоимости и приватности; средняя или низкая по реальному UX долгих задач, единой истории каналов и пользовательскому управлению sub-agents. Документация меняется быстро, а некоторые страницы неполны.

## 1. Краткое резюме

OpenHuman сегодня документирован как локально работающий desktop-ассистент с Rust core, встроенным чатом, модельным роутером, памятью, интеграциями, каналами сообщений и workflow-автоматизацией. Подходит пользователям, которым нужна персональная память по почте и другим подключённым источникам, рабочее desktop-приложение и возможность автоматизировать типовые процессы. Менее очевидный выбор для человека, которому нужен один удалённый Secretary с устойчивой Personal Conversation и доступными из любого канала адресуемыми persistent Workers.

Это не только локальный продукт: память и Markdown vault хранятся на устройстве, но стандартная настройка использует сервисы OpenHuman для входа, модельных запросов, части OAuth-интеграций и web search. Продукт открыт по GPL-3.0, но конкретное поведение и доступность функций нужно оценивать с поправкой на beta.

## 2. Масштаб и перспективы

На странице GitHub репозитория `tinyhumansai/openhuman` при проверке отображалось около 40,4 тыс. stars, 4 тыс. forks и дата создания 18 февраля 2026 года. Страница releases показывала около 39,4 тыс. stars в старом навигационном бейдже, поэтому корректнее считать это округлённым снимком порядка 40 тыс., а не точным счётчиком. Это сигнал заметного интереса к репозиторию, не показатель активных пользователей. Репозиторий обозначен GPL-3.0. [GitHub](https://github.com/tinyhumansai/openhuman)

На странице releases последней опубликованной версией была `v0.63.12`, от 7 августа 2026 года. Предыдущий релиз `v0.63.11` вышел в тот же день и включал крупную пачку изменений. Releases подтверждают активную разработку, но частые версии не доказывают стабильность. [Releases](https://github.com/tinyhumansai/openhuman/releases)

Продуктовая аудитория отдельно от аудитории TinyHumans не раскрыта. Заявление README о том, что репозиторий девять дней подряд был самым трендовым на GitHub, это заявление автора проекта, не метрика использования. Тарифная страница TinyHumans описывает также Medulla и OpenCompany, поэтому её тарифы и аудиторию нельзя целиком приписывать OpenHuman.

## 3. Установка и доступ

Официальная документация поддерживает desktop на macOS, Windows и Linux. Рекомендовано от 4 ГБ RAM; от 16 ГБ, если планируется большая индексация или локальная модель. Установка для разработки требует Git, Node.js 24+, pnpm 10.10.0, Rust 1.93.0 через rustup и CMake, но обычному пользователю эти инструменты не нужны. [Getting Started](https://tinyhumans.gitbook.io/openhuman/overview/getting-started) · [Install](https://github.com/tinyhumansai/openhuman/blob/main/INSTALL.md)

Подтверждённые пользовательские варианты установки:

- macOS: `brew install --cask openhuman`.
- Debian/Ubuntu: скачать `.deb` из [последнего релиза](https://github.com/tinyhumansai/openhuman/releases/latest), затем `sudo apt-get install -y --no-install-recommends ./OpenHuman_*_amd64.deb`.
- Windows: скачать `.msi` из последнего релиза и запустить.
- Есть `.dmg`, `.deb`, `.AppImage` и `.msi`. Документация предупреждает, что AppImage может не стартовать на Wayland или при нехватке системных библиотек. Arch/AUR путь описан как будущий после публикации пакета.

После запуска пользователь входит через один из поддерживаемых способов авторизации, при необходимости выдаёт разрешения ОС, подключает Gmail и выбирает, как запускать AI. Точный набор экранов и время onboarding не обещаны. По умолчанию можно пользоваться управляемыми сервисами, без отдельного ключа провайдера; доступен BYOK и локальные модели. Gmail ingestion работает по scheduler, первый цикл может начаться в течение 20 минут. До него полезен запрос к уже доступной модели или web search, но документация демонстрирует первые персональные вопросы после импорта почты.

## 4. Первые 30 минут: реконструкция по документации

Это ожидаемый путь по Getting Started, а не мой тест. Установить app, войти, подключить Gmail через OAuth, выбрать managed/cloud либо допустимый local/BYOK вариант и дождаться первичной синхронизации. Первый запрос из документации: «Что мне нужно знать за последние 12 часов?» или «Что ждёт моего ответа?». Ожидается краткий ответ по материалам подключённой почты. Затем пользователь открывает Memory tab, при желании просматривает Markdown vault в Obsidian и подключает дополнительные источники. [Getting Started](https://tinyhumans.gitbook.io/openhuman/overview/getting-started)

Основное трение: учетная запись и разрешения на подключения нужны до персонального результата; синхронизация не обязана быть мгновенной; стандартный вариант отправляет запросы через облачный backend. Большая часть обещанной ценности появляется после подключения данных, а не при первом пустом чате. Документация не даёт обоснованной оценки «запуск за N минут».

## 5. Ежедневный UX и каналы

README описывает desktop app, browser UI, terminal client и Rust library на одном core. Getting Started подтверждает desktop-приложение. В документации каналов встроенный Web chat является локальным чатом приложения, а `cli` обслуживает бинарник `openhuman-core`; самостоятельный общий web-сервис и его доступность для обычного пользователя описаны менее ясно. iOS Companion есть в документации, но прямо помечен experimental / non-shipping: это тонкий клиент к desktop core, а не отдельный постоянно доступный агент. [README](https://github.com/tinyhumansai/openhuman/blob/main/README.md) · [iOS Companion](https://tinyhumans.gitbook.io/openhuman/features/ios-companion)

Telegram поддерживается через Bot API long-poll. Есть подключение через managed DM или собственный BotFather token; docs называют Telegram наиболее полно поддержанным каналом по typing indicator и live draft updates, а также единственным каналом с отдельной поверхностью для inline approvals. Также документированы Discord, Web, iMessage на macOS, Lark/Feishu, DingTalk, Yuanbao, Slack и другие. Семь каналов доступны из Settings, некоторые настраиваются только через `config.toml`. [Messaging Channels](https://tinyhumans.gitbook.io/openhuman/features/channels)

Сообщение канала превращается в `ChannelMessage`, затем core создаёт или возобновляет agent run и возвращает ответ в тот же канал. Для некоторых каналов доступны `/models` и `/model`; у Telegram есть remote-control commands, но найденная продуктовая документация не даёт полного перечня команд и не подтверждает адресацию конкретного worker по стабильному ID. Каналы могут поддерживать proactive delivery, но только если задан default delivery target.

Нет подтверждения, что Telegram, CLI и desktop показывают одну общую историю Conversation или связывают один и тот же пользовательский Task/Worker. Документация говорит об agent session конкретного отправителя, а не о переносимой глобальной переписке. Файловые вложения, голос и артефакты зависят от отдельных функций и каналов; одинаковая поддержка между ними неизвестна. Native voice описан для desktop. Управление расписаниями есть через Workflows и cron.

## 6. Возможности

| Функция | Что документировано для пользователя | Статус |
|---|---|---|
| Память и персонализация | Локальная SQLite Memory Tree и Markdown vault; подключённые источники индексируются, profile-факты редактируемы в `PROFILE.md` | Подтверждено документацией |
| Skills и обучение | Каталог Skills описан примерно как 90 тыс. записей, но in-app runtime для выполнения Skills удалён. Персонализация учится предпочтениям и позволяет pin/forget | Подтверждено документацией, но маркетинговый масштаб каталога не равен числу исполняемых навыков |
| Интеграции | Каталог OAuth-интеграций, включая Gmail, Slack, Notion и GitHub; авторизация по каждому источнику явная | Подтверждено документацией; количество зависит от страницы, встречаются «100+» и «118+» |
| MCP | Поиск и локальная установка MCP servers как инструментов агента; OpenHuman также может выступать MCP server | Подтверждено документацией |
| Browser/computer use | Указан в продуктовом описании и нативном toolbelt; стабильность и доступность каждого действия не проверялись | Обещано/документировано, не проверено руками |
| Код и файлы | README заявляет terminal client и набор coder tools; инструменты для файлов, git и тестов перечислены в описании проекта | Заявлено официальными материалами, UX не проверен |
| Поиск и исследование | Web search tool, по умолчанию managed Exa; можно BYOK Exa, Tavily, Brave, Querit либо отключить | Подтверждено документацией |
| Schedules и triggers | Workflow с cron, ручным запуском или событием интеграции; визуальная схема и approval gates | Подтверждено документацией |
| Долгая работа и sub-agents | Harness имеет синхронные и асинхронные subagent-вызовы, управление running child и worker threads | Подтверждены внутренние механизмы; пользовательский persistent Worker UX не доказан |

Источники: [Memory Tree](https://tinyhumans.gitbook.io/openhuman/features/obsidian-wiki/memory-tree.md), [Personalization](https://tinyhumans.gitbook.io/openhuman/features/personalization.md), [MCP & Skills](https://tinyhumans.gitbook.io/openhuman/features/integrations/mcp-and-skills.md), [Web Search](https://tinyhumans.gitbook.io/openhuman/features/native-tools/web-search.md), [Workflows](https://tinyhumans.gitbook.io/openhuman/features/workflows.md).

## 7. Делегирование: важное различие

У OpenHuman есть внутренний multi-agent harness. Официальный issue #3880, закрытый после merge PR #3887 в июне 2026 года, описывает reusable asynchronous subagents: `spawn_async_subagent`, `steer_subagent`, `wait_subagent` и сохранение child conversations в worker thread. Задумка в том, чтобы совместимого subagent можно было повторно использовать с сохранением контекста, а не создавать заново на каждый шаг. Это подтверждает механизм runtime, не автоматически понятный пользователю список persistent Workers. [Issue #3880](https://github.com/tinyhumansai/openhuman/issues/3880)

Не нашёл подтверждения пользовательского контракта вида «создать Task, получить постоянный Worker reference, написать этому Worker из другого канала завтра, закрыть Task». Неизвестно, может ли пользователь явно выбрать уже существующего child, привязать его к собственному Task или продолжить его из Telegram/CLI. Уведомления сообщают о завершении/ошибке sub-agent, но это ещё не равно пользовательскому управлению его жизненным циклом. Workflow nodes могут создавать ветвления и подзадачи, но workflow это автоматизация, а не Worker для произвольного follow-up.

**Hermes не исследовался в этом отчёте.** Поэтому предположение о наличии или отсутствии в Hermes subagents здесь не подтверждается; его нужно проверить в назначенном отчёте Hermes, не выводить из устройства OpenHuman.

## 8. Контроль и надёжность

Approval Gate включён по умолчанию для действий с внешним эффектом. Инструменты классифицируются на Read, Write, Network, Install и Destructive; уровни autonomy задают разрешение, запрос подтверждения или блокировку. В документации описан fail-closed подход для неизвестных операций. Workflow имеет отдельную настройку подтверждения внешних действий. [Approval Gate](https://tinyhumans.gitbook.io/openhuman/features/approval-gate.md)

Локальные секреты хранятся в Keychain, Windows Credential Manager или Linux Secret Service; при недоступности keyring app должен спросить согласие перед fallback. Управляемые OAuth tokens хранятся на backend. Activity/Notifications дают журнал автономных событий; системные уведомления сохраняются и показываются после следующего запуска. Это хорошие контрольные механизмы, но они не доказывают автоматическое восстановление незаконченной задачи после перезапуска.

Поведение при обрыве сети, restart core, сохранении активной модели-сессии и точном возобновлении долгого subagent task не удалось подтвердить из пользовательской документации. Условия sandbox также нельзя считать эквивалентом изолированной машины без проверки конкретных инструментов и разрешений.

## 9. Стоимость

OpenHuman как исходный код бесплатен по GPL-3.0. В managed варианте модельные и orchestration вызовы тарифицируются через кредиты TinyHumans. Тарифная страница на дату проверки: Basic $20 в месяц или $200 в год с $21 кредитов в месяц; Pro $200 в месяц с $210 кредитов; есть pay-as-you-go пополнение от $5. Один credit соответствует $1 использования. BYOK и локальные модели оплачиваются соответствующим провайдерам или собственным оборудованием. [Pricing](https://tinyhumans.ai/pricing) · [Model Routing](https://tinyhumans.gitbook.io/openhuman/features/model-routing.md)

Страница тарифов охватывает также Medulla, API и OpenCompany. Из неё нельзя вывести точную месячную цену OpenHuman при заданном числе запросов: стоимость зависит от модели, объёма и выбранного backend. Региональных ограничений и реального типового расхода документация не приводит. Уточнять стоимость конкретно для OpenHuman перед покупкой обязательно.

## 10. Приватность и владение

SQLite Memory Tree и Obsidian-совместимый vault остаются локальными и доступны для чтения/копирования. Стандартная конфигурация проксирует managed LLM, OAuth интеграции и поиск через сервисы TinyHumans; «локальная память» не означает, что каждый запрос и каждый подключённый источник остаются на устройстве. В `local_only` режиме, согласно docs, core блокирует cloud inference, сетевые инструменты, интеграции, web search и cloud embeddings, разрешая локальные runtimes; voice обозначен как исключение. [Privacy & Security](https://tinyhumans.gitbook.io/openhuman/features/privacy-and-security.md) · [Privacy Mode](https://tinyhumans.gitbook.io/openhuman/features/privacy-and-security/privacy-mode.md)

Исходники лицензированы GPL-3.0. Markdown памяти снижает зависимость от закрытого формата. Публичные источники не объясняют в одном месте экспорт всей переписки, перенос session history на новый компьютер и удаление серверной учётной записи/данных. On-premise в тарифах относится к Enterprise-услугам TinyHumans, не является подтверждением self-hosted режима всего OpenHuman backend.

## 11. Проверка сценария пользователя

| Шаг | Что подтверждено | Что не подтверждено или неизвестно |
|---|---|---|
| Telegram: исследуй 3 вакансии | Telegram канал и web search документированы; агент может отвечать через Telegram | Нет подтверждения, что один запрос разбивается на пользовательски видимые параллельные Workers или отдаёт прогресс по каждому |
| Терминал: продолжи кодовый Task | `openhuman-core` CLI и coder tools упомянуты в README | Нет доказательства, что CLI выберет тот же Task и тот же Worker, что Telegram |
| Telegram: статус и уточнение | Есть Telegram remote-control commands и subagent notifications | Статус конкретного Worker, его steering и сохранение binding из Telegram не подтверждены публичной UX-документацией |
| Закрыл ноутбук | Notification события могут сохраниться до следующего запуска; расписания workflow перерегистрируются при старте app | Неясно, продолжится ли обычный активный Task, пока машина выключена/спит. Experimental iOS Companion зависит от desktop core |
| Вернулся завтра | Memory Tree и некоторые child worker threads описаны как долговечные | Общая Personal Conversation, Worker identity, Task binding и восстановление runtime session между каналами не подтверждены |

Итог: отдельные части сценария существуют, но OpenHuman не документирует end-to-end контракт, нужный пользователю. Каналы направляют запросы в общий core, но из этого нельзя заключить, что они разделяют один conversation timeline и persistent Worker.

## 12. Преимущества и слабые стороны

**Подтверждённые плюсы:** память в локальных SQLite и Markdown, редактируемый профиль, много интеграций, Telegram как канал, смена модели через router, визуальные workflows, approval gate и открытая лицензия. Связка «поиск/интеграции + долговременная память + workflow» шире обычного terminal coding assistant.

**Слабые места:** beta-статус заявлен самим проектом; onboarding зависит от подключения аккаунтов и облачных сервисов; каталог Skills в основном метаданные, а не гарантированное выполнение; Telegram-remote control описан поверхностно; iOS-клиент не shipping. Модель subagent потенциально мощная, но продуктовая ясность persistent Task/Worker ниже, чем у требуемого Secretary.

Вторичное свидетельство: независимый обзор Superbash от 21 мая 2026 года сообщил о цикле авторизации и ошибке socket handshake, мешавшей получить ответ в тогдашней beta. Это единичный старый hands-on отчёт, не свидетельство текущего состояния после множества релизов. Официальный issue #1805 от мая перечислял недостающие тогда Telegram-функции и был закрыт после merge PR #2502 в августе. Закрытие issue подтверждает завершение GitHub-задачи, но не подтверждает каждый перечисленный UX-сценарий на практике. [Superbash, May 21](https://superbash.ai/videos/bSpjLglSh34) · [Issue #1805](https://github.com/tinyhumansai/openhuman/issues/1805)

## 13. Выводы для Secretary

OpenHuman показывает, что локальная читаемая память, выбор модели, approvals и разные каналы можно описывать как единый продукт, но пользовательский «один агент на все каналы» нельзя выводить только из общего runtime. Для Secretary дифференциацией может стать ясный durable контракт Personal Conversation → Task → Worker binding → Result, а не просто скрытая оркестрация.

Приоритетные продуктовые гипотезы для проверки, не задания на реализацию:

1. Общая история и адресация из любого канала могут быть важнее большого количества integrations, если пользователь перескакивает между terminal и Telegram.
2. Worker reference и явный статус Task снимут неоднозначность subagent UX, когда можно продолжить существующую работу, а не начинать похожую заново.
3. Result в исходную Personal Conversation должен быть постоянным и понятным, а live activity можно показывать отдельно, чтобы не засорять историю.
4. Явное объяснение, какие данные локальны, а какие уходят в backend, уменьшит разрыв между «local-first» обещанием и облачными модельными/интеграционными вызовами.
5. Практичный terminal и Telegram UX лучше проверять как единый сценарий с перезапуском и сном устройства, а не считать наличие двух адаптеров достаточным.

Это сравнение основано на целевом продукте из `CONTEXT.md`; перечисленные функции Secretary здесь не считаются уже реализованными.

## 14. Открытые вопросы и будущий hands-on тест

Неизвестны: единая ли сессия у Telegram, desktop и CLI; может ли пользователь выбрать и продолжить существующий subagent; что переживает остановку app/core; точная стоимость типового OpenHuman использования; полная процедура экспорта истории и удаления данных.

Будущий тест: на чистой машине установить релиз, пройти onboarding, подключить тестовую почту и Telegram bot, отправить один исследовательский и один кодовый запрос. Зафиксировать conversation/session identifiers, создать async subagent, отправить ему уточнение и проверить видимость статуса в Telegram. Затем закрыть app, перезапустить и сравнить историю, memory vault, Task/child transcript и расходы. Не считать сценарий подтверждённым, пока он не пройден целиком.

## 15. Источники

Основные первичные источники: [главная OpenHuman](https://tinyhumans.ai/openhuman), [GitHub repository](https://github.com/tinyhumansai/openhuman), [README](https://github.com/tinyhumansai/openhuman/blob/main/README.md), [Install](https://github.com/tinyhumansai/openhuman/blob/main/INSTALL.md), [Getting Started](https://tinyhumans.gitbook.io/openhuman/overview/getting-started), [Messaging Channels](https://tinyhumans.gitbook.io/openhuman/features/channels), [Approval Gate](https://tinyhumans.gitbook.io/openhuman/features/approval-gate.md), [Memory Tree](https://tinyhumans.gitbook.io/openhuman/features/obsidian-wiki/memory-tree.md), [MCP & Skills](https://tinyhumans.gitbook.io/openhuman/features/integrations/mcp-and-skills.md), [Workflows](https://tinyhumans.gitbook.io/openhuman/features/workflows.md), [Privacy & Security](https://tinyhumans.gitbook.io/openhuman/features/privacy-and-security.md), [Pricing](https://tinyhumans.ai/pricing), [Releases](https://github.com/tinyhumansai/openhuman/releases), [Issue #3880](https://github.com/tinyhumansai/openhuman/issues/3880), [Issue #1805](https://github.com/tinyhumansai/openhuman/issues/1805). Даты GitHub releases и issue указаны на страницах; документация GitBook менялась в 2026 году, дата проверки отчёта 2 октября 2026 года.
