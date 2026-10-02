# Delta от Zed: продуктовый отчёт

**Проверено: 2 октября 2026 года.** Отчёт основан на официальных страницах Zed/Delta и документации, не на hands-on тесте. Уверенность высокая в описании объявленных функций, тарифов и ограничений безопасности; средняя в текущей доступности отдельных функций, потому что несколько статусов roadmap ещё «In Progress». Сторонние независимые UX-свидетельства не использовались.

## 1. Краткое резюме

Delta сегодня позиционируется как совместная среда разработки с AI-агентами и проверки их изменений, а не как универсальный персональный ассистент. Центральный объект здесь не Personal Conversation и не постоянный Worker, а coding thread, объединяющий разговор, изменения файлов и работу над конкретным проектом. 16 сентября Delta перешла из private beta в public beta; для работы нужны Zed account и принятие Early Access Agreement. Это всё ещё beta, не GA. [Анонс Delta](https://zed.dev/blog/introducing-delta), [public beta](https://zed.dev/blog/delta-public-beta), [установка](https://delta.dev/docs/installation).

Для разработчиков сильная сторона Delta в том, что обсуждение и незакоммиченная работа синхронизируются вместе. Для пользователя Secretary, которому нужна одна личная беседа между Pi в терминале и Hermes в Telegram, совпадение слабое: Telegram нет, общего личного inbox нет, а persistent worker с адресуемым Task не является документированной базовой моделью продукта.

## 2. Масштаб и перспективы

Delta открыта в public beta и доступна для скачивания на macOS, Linux и Windows, а также через web. Самостоятельного показателя пользователей, MAU или размера сообщества в просмотренных источниках нет. В официальной документации и страницах продукта не обнаружен отдельный OSS-репозиторий Delta, которому можно было бы честно приписать GitHub stars. Stars редактора Zed здесь не показатель аудитории Delta.

Признак активности разработки есть: release notes дошли до версии 0.18.0 от 30 сентября; там описаны мобильная навигация по комментариям, поиск subthreads и attached agents, работа с существующими Git worktrees и новые модели. Версия 0.15.0 датирована 16 сентября, в день запуска public beta. Это сигнал частых beta-изменений, но не мера популярности. [Release notes](https://delta.dev/docs/whats-in-the-latest).

## 3. Установка и доступ

Требования: macOS 13+ на Apple Silicon; Linux с glibc на x86_64 или ARM64; Windows на x86_64 или ARM64. Нужны интернет, Zed account и принятие Early Access Agreement. Установщик берётся со страницы [Download](https://delta.dev/download). На macOS инструкция предлагает распаковать `Delta.app.zip`, переместить приложение в `/Applications` и выполнить:

```sh
xattr -dr com.apple.quarantine /Applications/Delta.app
```

Дальше открыть Delta и войти. Официальные инструкции для [Linux и Windows](https://delta.dev/docs/installation) зависят от скачанного пакета; команды установки здесь не воспроизвожу, поскольку в проверенном фрагменте страницы они не были видны.

Встроенная цепочка onboarding ведёт в Welcome to Delta, затем просит выбрать Git-репозиторий. Для первого полезного изменения Quick Start советует открыть корень репозитория, выбрать `Isolated Workspace`, подключить модель и отправить задачу. Delta требует настроенный `origin`. Подключить модель можно через hosted models платного плана, API key, некоторые существующие подписки либо сторонний агент. API keys настраиваются в `Settings > LLM Providers` либо переменными в `~/.config/delta/.env`. [Quick Start](https://delta.dev/docs/getting-started/quick-start), [Models & Providers](https://delta.dev/docs/agents/models-and-providers).

Тариф Personal стоит $0: свои ключи или external agents. Pro стоит $10 в месяц, включает $5 токенов в месяц, далее списание зависит от использования; есть двухнедельный trial с $5 кредитов без ввода карты. У Zed и Delta общий аккаунт и баланс. Страница не указывает региональные ограничения. [Pricing](https://delta.dev/pricing).

## 4. Первые 30 минут: реконструкция, не тест

По Quick Start пользователь принимает соглашение, входит, проходит Welcome, выбирает проект и подключает модель. Пример первого запроса: «Найди небольшое улучшение в этом репозитории, объясни план, внеси изменение и запусти подходящую проверку». Ожидаемый путь: агент предлагает задачу и способ проверки, изучает файлы, редактирует отдельный checkout, показывает diff; пользователь проверяет его и переносит результат в исходный проект. Это реконструкция документированного сценария, а не подтверждение реального времени выполнения. Официальная инструкция не даёт основания обещать, что setup займёт именно 30 минут.

Главное трение возникает до первой генерации: необходимо выбрать репозиторий с настроенным `origin`, модельный доступ и способ работы с checkout. Важно прочитать предупреждение: Delta отправляет на свои серверы Git-tracked файлы и untracked файлы, которые не игнорируются Git. Изолированный checkout отделяет изменения, но не ограничивает доступ агента к машине.

## 5. Ежедневный UX и каналы

Есть desktop-приложение, web-клиент и мобильный браузер, но не отдельное нативное мобильное приложение. На web можно открыть thread, писать сообщения, комментировать, смотреть файлы и diff, делать review. Mobile Firefox официально не поддержан. В browser turn можно использовать hosted model для synced files, но у такого исполнения нет disk checkout и shell. Desktop-агент работает в checkout на машине участника; изменения участников синхронизируются в thread в реальном времени. [Delta on the Web](https://delta.dev/docs/collaboration/delta-on-the-web), [Delta Worktrees](https://delta.dev/docs/concepts/worktrees).

Это не одна история для разных каналов Secretary. Логин и список Delta threads переходят между устройствами; каждый thread относится к проекту и работе над ним. Официальных Telegram, Slack, Discord или иных каналов сообщений нет. Можно комментировать код и сообщения, следить за агентом и просматривать diff. В документации есть работа с изображениями в модельном контексте и терминалом в thread; голос, универсальная загрузка документов и push-уведомления не подтверждены.

Тезис из присланного скриншота о совместном сохранении thread и worktree подтверждается: DeltaDB сохраняет сообщения, комментарии и file deltas; у участников свои локальные checkout, синхронизируемые между собой. Для сотрудничества не нужно сначала делать commit или push. [DeltaDB](https://zed.dev/blog/introducing-deltadb), [обзор Delta](https://zed.dev/blog/introducing-delta), [Delta & Git](https://delta.dev/docs/concepts/delta-and-git).

## 6. Возможности

| Функция | Что подтверждено | Статус и источник |
|---|---|---|
| Память и персонализация | История thread связана с проектом; агент может искать другие threads и читать ограниченные выдержки. `AGENTS.md` и skills-файлы задают инструкции. Это не личная межканальная память Secretary. | Подтверждено документацией: [AI Privacy](https://delta.dev/docs/privacy-and-security/privacy), [Agentic Safety](https://delta.dev/docs/privacy-and-security/agentic-safety) |
| Skills и обучение | Документированы skills и правила репозитория. Самообучение по предпочтениям пользователя не заявлено. | Skills подтверждены; обучение неизвестно: [Skills](https://delta.dev/docs/agents/skills) |
| Browser/computer use | Browser есть как UI. Агент в web turn не имеет shell/checkout; browser automation или управление компьютером не подтверждены. | Неизвестно / не заявлено в проверенных docs: [Delta on the Web](https://delta.dev/docs/collaboration/delta-on-the-web) |
| Connectors и MCP | MCP указан на roadmap как `In Progress`; готовые Telegram/Slack connectors не обнаружены. | Обещано, не считать shipped: [Roadmap](https://delta.dev/roadmap) |
| Код и файлы | Агент читает и редактирует проект, запускает команды; diff и review связаны с thread. DeltaDB сохраняет промежуточные изменения. | Подтверждено: [Quick Start](https://delta.dev/docs/getting-started/quick-start) |
| Поиск и исследование | Поиск по проекту и threads есть; продуктовая веб-исследовательская функция не подтверждена. | Частично подтверждено; веб-поиск неизвестен |
| Schedules/triggers/proactive work | В документации и roadmap не нашёл расписаний или автоматических триггеров. | Неизвестно, не заявлено |
| Длительные задачи | Thread сохраняет разговор и изменения; агент на локальном runtime привязан к машине. Persistent remote runtime пока на roadmap. | Thread подтверждён, продолжение в cloud обещано / `In Progress`: [Roadmap](https://delta.dev/roadmap) |
| Командная работа | Приглашённые участники видят thread, комментируют и работают в синхронизированных checkout; есть review subthreads. | Подтверждено: [Collaborate in a Thread](https://delta.dev/docs/collaboration/collaborate-thread) |

## 7. Делегирование и роль Pi

Delta умеет subagents: пользователь просит основного агента разделить независимую работу, каждый subagent ведёт отдельный разговор и отчитывается в parent thread. Это делегирование внутри coding thread, а не самостоятельный persistent Worker, которому пользователь назначает Task, затем адресует follow-up из другого канала. Текущие release notes упоминают поиск subthreads и attached agents; документация описывает отслеживание subagent work. Подтверждения, что завершённый subagent остаётся доступен для отдельного follow-up как постоянная Worker identity, нет. [Subagents](https://delta.dev/docs/agents/subagents), [release notes 0.18](https://delta.dev/docs/whats-in-the-latest).

Совместное использование thread с человеком и agent даёт continuation кода и решений внутри команды. Это не внутренний multi-agent pipeline Secretary и не универсальная маршрутизация Task. Web поддерживает сообщения к thread и работу с subthreads; терминальная интеграция ограничена тем, что официально заявлено.

Анонс Delta обещал синхронизацию сторонних agent harnesses, начиная с Claude Code. Но на проверенную дату Delta CLI и ACP перечислены в roadmap как `In Progress`. Поэтому не считаю уже подтверждённой текущую интеграцию Pi. Документация Zed перечисляет Pi Coding Agent для редактора Zed, но это не доказывает работу Pi как harness внутри продукта Delta. [Анонс Delta](https://zed.dev/blog/introducing-delta), [Roadmap](https://delta.dev/roadmap), [External Agents в Zed](https://zed.dev/docs/ai/external-agents).

## 8. Контроль и надёжность

Ограничения серьёзные и задокументированы: у Delta пока нет agent permission system, агент не спрашивает перед вызовом инструментов, включая потенциально разрушительные, и не работает в sandbox. Отдельный `Isolated Workspace` защищает исходный checkout от изменений, но не изолирует права агента на устройстве. Репозиторий может запускать `.agents/prepare` или `.delta/prepare`, `direnv`-настройки и инструкции `AGENTS.md`/skills. Roadmap обещает sandbox и permissions, но на проверенную дату они не поставлены. [Agentic Safety](https://delta.dev/docs/privacy-and-security/agentic-safety).

Delta документирует шифрование in transit и at rest и redaction распознанных секретов до синхронизации или отправки модели. Redaction не охватывает произвольные файлы и может пропустить неизвестные форматы. Ошибка сети при запуске workspace требует проверить соединение; для восстановления checkout предусмотрена отдельная troubleshooting-инструкция. Поведение runtime после падения процесса, автоматический restart и гарантии resumability не установлены источниками. Стоимость Pro видна в виде кредитов и token-based billing, но итоговый расход зависит от модели и запросов.

## 9. Приватность и владение данными

DeltaDB хранит содержимое проекта и историю thread и локально, и на серверах Delta; это необходимо для синхронизации и командной работы. Не self-hosted и не OSS в проверенных источниках. Zed сообщает, что не использует код и разговоры для обучения моделей. Запросы могут включать текст thread, сообщения и комментарии команды, изображения, вывод tools/terminal и прочитанное агентом содержимое файлов. Для hosted models запрос идёт через Zed Cloud, для собственных provider credentials обычно прямо провайдеру. Телеметрия связана с аккаунтом, отключить её настройкой нельзя. [Data Storage & Deletion](https://delta.dev/docs/privacy-and-security/data-storage), [AI Privacy & Telemetry](https://delta.dev/docs/privacy-and-security/privacy), [Security](https://delta.dev/docs/privacy-and-security/security).

Документация содержит разделы удаления thread и аккаунта, но из проверенного материала не удалось подтвердить сроки окончательного удаления, самостоятельный экспорт полного архива или регион хранения. Перед загрузкой чувствительного репозитория это открытые вопросы. Git остаётся путём переносимости кода, но перенос conversation, comments и DeltaDB history в другой продукт не подтверждён.

## 10. Проверка сценария пользователя

| Шаг | Что Delta подтверждает | Что отсутствует или неизвестно |
|---|---|---|
| Telegram: «исследуй 3 вакансии» | Ничего про Telegram. | Нет документированного канала; web turn не равен Telegram и его web research не подтверждён. |
| Терминал: продолжи кодовый Task | Можно продолжать кодовую работу в thread на desktop; Delta worktrees синхронизируются. | Pi/CLI/ACP handoff пока `In Progress`; нельзя подтвердить, что тот же runtime Pi продолжится из Delta. |
| Telegram: статус и уточнение | Web позволяет открыть thread и послать сообщение. | Общей Personal Conversation с Telegram нет. |
| Закрыл ноутбук | Анонс Delta описывает перенос задачи на cloud runner с продолжающимся агентом. | На roadmap remote runtime всё ещё `In Progress`; нельзя утверждать, что текущий пользователь может на это рассчитывать. Локальный процесс и выключенный ноутбук не тождественны облачной работе. |
| Вернулся завтра | Сохранённый thread и DeltaDB worktree можно снова открыть; archived checkout восстанавливается при наличии истории. | Автоматическое восстановление активного agent runtime и сохранение идентичности Worker не подтверждены. |

Итого: общий Delta thread и кодовая история между desktop/web подтверждены. Общая беседа пользователя, постоянная Worker identity, единый Task routing и runtime session через Telegram, Pi и следующий день не подтверждены. [Web](https://delta.dev/docs/collaboration/delta-on-the-web), [Worktrees](https://delta.dev/docs/concepts/worktrees), [Roadmap](https://delta.dev/roadmap).

## 11. Преимущества и слабые стороны

**Подтверждённые продуктовые плюсы:** разговор связан с точными промежуточными изменениями, а не только с коммитом; ревьюер видит исходную дискуссию; участники работают со своими локальными копиями до commit/push; review subthread позволяет экспериментировать изолированно от parent thread. DeltaDB при этом остаётся совместимой с обычным Git-потоком.

**Подтверждённые минусы:** продукт заточен под разработку и Git-проекты, а не личную работу во всех мессенджерах; при открытии проекта значимая часть его файлов загружается на сервер; полноценная работа агента зависит от desktop runtime, пока remote runtime не готов; безопасность агента сейчас слаба по собственным docs Delta. Это вывод по документации. Независимые отзывы о реальном UX и сбоях отдельно не собирались.

## 12. Выводы для Secretary: гипотезы

1. Заимствовать единый thread timeline, связывающий запрос, обсуждение, ход работы и Result. Delta показывает ценность сохранения причины изменения рядом с результатом.
2. Не смешивать persistent Worker с subagent. В Secretary Follow-up требует явного Worker binding и сохранения адреса существующей runtime session, а не только поиска дочернего разговора.
3. Дифференцироваться channel-agnostic Personal Conversation. Delta охватывает web и coding desktop, но Telegram, Pi и Hermes не образуют у неё одного inbox.
4. Сделать remote execution и permissions проверяемыми продуктовым контрактом. У Delta cloud continuation пока на roadmap, а отсутствие approvals/sandbox явно задокументировано как риск.
5. Рассмотреть безопасную передачу кода и истории между участниками без commit как UX-паттерн, но только если Secretary работает с файлами: показывать, что именно синхронизируется и кому это доступно.

Это гипотезы для продукта, не описание уже реализованных возможностей Secretary.

## 13. Открытые вопросы и hands-on план

Открыты: доступен ли remote runtime обычному beta-пользователю за пределами roadmap; может ли Delta продолжить активный runtime после закрытия приложения или reboot; когда станут доступны CLI/ACP и можно ли подключить Pi; как экспортировать и окончательно удалить данные; какие регионы хранения доступны; как субагенты отображаются и адресуются после завершения.

Будущий hands-on тест: установить Delta на отдельной машине с тестовым Git-репозиторием; пройти onboarding и записать модельный выбор; создать thread с незакоммиченными правками, проверить их в web и на втором desktop; закрыть приложение и ноутбук, затем измерить, продолжает ли агент работу; отдельно попробовать Pi/ACP и subagent follow-up; проверить приглашения и права; удалить тестовый thread и запросить экспорт/удаление аккаунта. До такого теста рекламное описание cloud runner и перемещения thread на телефон не следует считать доказательством полного удалённого выполнения.

## 14. Основные первичные источники

Дата проверки всех страниц: 2 октября 2026 года.

- [Introducing Delta](https://zed.dev/blog/introducing-delta), 12 августа 2026.
- [Delta public beta](https://zed.dev/blog/delta-public-beta), 16 сентября 2026.
- [Introducing DeltaDB](https://zed.dev/blog/introducing-deltadb), 11 июня 2026.
- [DeltaDB: product page](https://zed.dev/deltadb).
- [Install Delta](https://delta.dev/docs/installation), [Quick Start](https://delta.dev/docs/getting-started/quick-start), [Models & Providers](https://delta.dev/docs/agents/models-and-providers).
- [Delta Worktrees](https://delta.dev/docs/concepts/worktrees), [Delta on the Web](https://delta.dev/docs/collaboration/delta-on-the-web), [Subagents](https://delta.dev/docs/agents/subagents).
- [Roadmap](https://delta.dev/roadmap), [Pricing](https://delta.dev/pricing), [Release notes](https://delta.dev/docs/whats-in-the-latest).
- [Agentic Safety](https://delta.dev/docs/privacy-and-security/agentic-safety), [AI Privacy & Telemetry](https://delta.dev/docs/privacy-and-security/privacy), [Data Storage & Deletion](https://delta.dev/docs/privacy-and-security/data-storage).
