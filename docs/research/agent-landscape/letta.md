# Letta Agent: продуктовый разбор

**Проверено: 2 октября 2026 года.** Отчёт основан на официальном сайте, актуальной документации и GitHub, а также на одном публичном пользовательском issue. Продукт не устанавливался и не тестировался. Поэтому «подтверждено» ниже означает «так описано в документации», а не «проверено руками».

## 1. Краткое резюме

Letta сегодня продаёт не старого MemGPT-сервера, а набор для персональных долговременных агентов: Letta Code (CLI и desktop app), облачные агенты в Constellation, браузерный `chat.letta.com`, SDK и каналы вроде Telegram. Главная идея продукта: постоянный агент с изменяемой памятью и навыками, который сохраняет идентичность между разговорами и может работать с локальными файлами или удалённым окружением. Это подходит разработчикам, техническим пользователям и тем, кто готов сам выбирать размещение и провайдера модели. Описание для личного помощника есть, но наиболее конкретные UX-документы сосредоточены на Letta Code и coding workflows. [Страница продукта](https://www.letta.com/agent), [Handbook](https://docs.letta.com/handbook)

Продукт доступен через CLI, приложения для macOS, Windows и Linux и облачный web. Каналы помечены как beta. Letta быстро меняется: документация местами не синхронизирована с релизами. Уверенность высокая по установке, моделям, каналам и тарифам; средняя по межканальному поведению в конкретных режимах; низкая по восстановлению незавершённой работы и управлению делегированными исполнителями.

## 2. Масштаб и перспективы

У актуального репозитория [letta-code](https://github.com/letta-ai/letta-code) GitHub показывал около **3,5 тыс. stars** на дату проверки. Последний релиз на странице GitHub был `v0.34.1`, выпущен 30 сентября 2026 года. Это признаки заметного интереса разработчиков и активной разработки, но не число пользователей продукта. Letta не публикует проверяемых MAU или использования Letta Agent, поэтому аудиторию продукта оценить нельзя. Не следует подменять её аудиторией старого репозитория `letta` или самой компании. [Releases](https://github.com/letta-ai/letta-code/releases)

## 3. Установка и доступ

**CLI.** Официальная команда: `npm install -g @letta-ai/letta-code`, затем `letta`. В руководстве self-hosting указан Node.js 22.19+; промо-страница Letta Agent всё ещё сообщает Node.js 18+, так что минимальное требование в источниках расходится. Для установки CLI разумно ориентироваться на более новое требование и перепроверить его перед развёртыванием. [Getting started](https://www.letta.com/agent), [Self-hosting](https://docs.letta.com/self-hosting)

**Desktop и web.** Приложение доступно для macOS Apple Silicon, Windows x64/ARM64 и Linux x64/ARM64. `chat.letta.com` работает с агентами в Letta Cloud; для локальных агентов нужен CLI или desktop app. Отдельное нативное мобильное приложение не подтверждено, мобильный браузер упоминается как вариант доступа к web. [Setup](https://docs.letta.com/handbook/setup), [FAQ](https://docs.letta.com/reference/faq)

Дальше пользователь выбирает, где хранится агент:

- **Local mode:** состояние, память и история находятся на устройстве, аккаунт Letta не обязателен. Резервное копирование и перенос пользователь обеспечивает сам.
- **Constellation / cloud:** состояние централизовано и доступно через облачные интерфейсы. Исполнение инструментов может оставаться на компьютере либо идти в облачном sandbox, это отдельный выбор.
- **Self-hosted App Server:** агентский runtime можно разместить на собственном сервере и подключать к нему несколько клиентов через SDK. Это технический путь, а не готовый управляемый персональный сервис. [Self-hosting](https://docs.letta.com/self-hosting)

Затем пользователь подключает модель через `/connect` или desktop UI: собственный API key, поддерживаемый coding plan или локальную модель вроде Ollama/LM Studio. Для cloud-агента подключаются Letta Auto или BYOK. До первого полезного результата нужны рабочая модель и, если агент должен менять проект, выбранное рабочее окружение и понятные права инструментов. Документы не дают надёжной оценки времени первого запуска.

## 4. Первые 30 минут: реконструкция по документации

Путь в терминале: установить CLI, запустить `letta`, выбрать или создать агента, подключить модель, открыть проект и попросить: «Изучи этот репозиторий, опиши его назначение, важные команды и три риска, которые стоит проверить». Затем запустить `/init`, чтобы агент собрал начальную память о проекте, и `/remember` для явного правила, например «Перед изменениями запускай тесты». Можно проверить память через `/memory`, поискать прежние сообщения через `/search` и продолжить разговор позднее через `letta` или `letta --resume`. [Quickstart](https://docs.letta.com/quickstart), [CLI reference](https://docs.letta.com/letta-code/cli-reference)

Ожидаемый результат по docs: ответ по доступному проекту, команды/файлы через локальные инструменты и сохранённое правило для последующих разговоров. Это не проверенный результат теста. Трение: настройка модели; выбор между локальным и cloud state; неожиданно широкие права в `unrestricted`; необходимость держать доступное устройство для локальных tools, каналов и расписаний.

## 5. Ежедневный UX

В CLI команда `letta` возвращает пользователя к последнему агенту и его default conversation; `--resume` открывает выбор прошлых сессий, `--agent` и `--conversation` задают их явно. Desktop даёт чат, память, навыки, каналы и расписания. Облачный `chat.letta.com` открывает cloud agents. Telegram, Slack, Discord и WhatsApp заявлены как дополнительные каналы, но документация называет Channels beta. Для каналов сообщения входят в разговор агента, а не в отдельный продуктовый «таск». Один и тот же агент и одна и та же conversation могут связывать несколько поверхностей; автоматическая единая история между любыми аккаунтами, агентами и режимами не обещана. Telegram pairing прямо хранит привязку к agent и conversation. [CLI reference](https://docs.letta.com/letta-code/cli-reference), [Channels](https://docs.letta.com/letta-code/channels), [Telegram](https://docs.letta.com/letta-code/channels/telegram)

Рабочий контекст файлов локален для выбранного компьютера или remote environment. Можно закрыть терминал и выбрать conversation снова, но это не доказывает, что выполняющийся на компьютере процесс переживёт сон или перезапуск. Поддержка голосового ввода, надёжной доставки файлов и уведомлений различается по каналам; полного межканального контракта в изученных документах нет. Letta Agent рекламирует proactive notifications, а Telegram guide описывает отправку сообщений агентом, но это не гарантия фонового исполнения при выключенном хосте.

## 6. Возможности

| Функция | Что пользователь может сделать | Статус и источник |
|---|---|---|
| Память и персонализация | Сохранять/править память, давать долговременные правила, искать прошлые сообщения; переключать модель, сохраняя агента | Подтверждено документацией: [Quickstart](https://docs.letta.com/quickstart), [модели](https://docs.letta.com/configuration/models) |
| Skills и обучение | Подключать skills, импортировать их, просить агента создать повторяемый навык; использовать `/remember` | Возможности описаны, качество самообучения не гарантировано: [Quickstart](https://docs.letta.com/quickstart), [Desktop](https://docs.letta.com/letta-code/desktop-app) |
| Browser/computer use | Запускать локальные shell/file tools; подключать remote computer. Cloud agents имеют web search/fetch; local setup требует своих skills/tools | Частично подтверждено: [How it works](https://docs.letta.com/letta-code/how-it-works), [Models](https://docs.letta.com/configuration/models); полноценное браузерное управление не подтверждено |
| Каналы и MCP | Связать Telegram, Slack, Discord, WhatsApp; разработчику подключить custom channels/MCP | Каналы beta; cloud/local инструкции расходятся: [Channels](https://docs.letta.com/letta-code/channels), [SDK MCP](https://docs.letta.com/agent-sdk/mcp/) |
| Код и файлы | Читать/менять файлы и запускать команды в окружении агента | Подтверждено; команды исполняются на клиентском компьютере в CLI: [How it works](https://docs.letta.com/letta-code/how-it-works) |
| Поиск и исследование | Искать web-источники в cloud agent, свои инструменты подключать локально; искать прошлые сообщения | Подтверждено с ограничениями режима: [Models](https://docs.letta.com/configuration/models), [CLI](https://docs.letta.com/letta-code/cli-reference) |
| Schedules и proactive work | Создавать разовые и повторные prompts, вручную или попросив агента | Подтверждено; требуется подключённый `letta server`, до 50 активных задач на агента: [Schedules](https://docs.letta.com/letta-code/scheduling) |
| Длительные задачи и совместная работа | Держать постоянных агентов и conversations; через SDK собрать team с отдельными agent/conversation | Платформенная возможность подтверждена; готовый UX persistent task-workers не найден: [Integration patterns](https://docs.letta.com/platform/app-server/integration-patterns) |

## 7. Делегирование: не путать subagents и persistent Workers

В Letta есть subagents и multi-agent способы работы. Workflow может запускать короткоживущих субагентов на том же компьютере и в том же рабочем каталоге; по описанию workflow каждый имеет отдельный временный transcript, собственной памяти у него нет, а родителю возвращается финальный ответ. Это полезно для параллельного исследования или ограниченного подзапроса, но не эквивалент Worker, чей identity и Task остаются адресуемыми после завершения запуска. [Dynamic workflows](https://docs.letta.com/configuration/workflows)

На уровне SDK разработчик может смоделировать команду как постоянных агентов с несколькими conversations, отправлять им работу и сохранять task state/result в своём приложении. Документация прямо возлагает durable job state, результаты, reconnect и UI routing на controller приложения. В Letta Code есть переключение агентов и conversations; подтверждения, что обычный пользователь может адресовать уже созданного subagent как persistent Worker, передать ему follow-up и увидеть его как отдельную задачу с сохранённой привязкой, я не нашёл. Это ключевое отличие от целевой модели Secretary.

Telegram-канал связывает чат с конкретными agent и conversation, а не с subagent или worker task. В CLI можно выбрать те же идентификаторы. При этом UI-кнопки для выбора существующего Worker, просмотра его progress, steering, cancel и возврата результата в единый Personal Conversation не документированы. Новая задача через субагент не равна гарантированному продолжению прежнего контекста. Наличие субагентов в Letta подтверждается; предположение пользователя об их отсутствии в Hermes в этом отчёте не проверялось, так как назначен только Letta.

## 8. Контроль и надёжность

Права особенно важны: документация Permissions говорит, что интерактивный CLI запускается в `unrestricted`; режим `standard` запрашивает разрешение для shell, правок и subagents. Есть `acceptEdits`, deny/allow patterns, ограничение набора tools и защита директорий памяти других агентов. Следовательно, нельзя считать подтверждение каждого действия дефолтным. Пусть договорённость на команду, сетевой sandbox и безопасное хранение credentials остаются отдельными вопросами. [Permissions](https://docs.letta.com/letta-code/permissions)

CLI исполняет Bash/Read/Write на той машине, где работает клиент. Cloud agent не означает, что локальные файлы отправлены в изолированный sandbox. Для schedules устройство должно быть доступно, а `letta server` подключён; разовые задачи с опозданием больше пяти минут отмечаются как missed. Remote VM предлагается как непрерывно доступный вариант. История разговора и состояние агента устойчивее runtime-процесса, но документы не подтверждают универсальное восстановление незавершённого tool call после сетевого обрыва/перезапуска. У App Server SDK есть runtime sync/replay, однако durable job state и checkpoints должны храниться в приложении-контроллере. [Schedules](https://docs.letta.com/letta-code/scheduling), [SDK sessions](https://docs.letta.com/agent-sdk/sessions/), [integration patterns](https://docs.letta.com/platform/app-server/integration-patterns)

## 9. Стоимость

На странице Personal Pricing указаны Free за $0 с ограниченным использованием Letta Auto и максимум тремя cloud-managed агентами, а Pro за $20/месяц с квотой Letta Auto и максимумом 20 агентов. Перерасход Letta Auto оплачивается по API rates через кредиты. BYOK и внешние coding plans позволяют платить провайдеру отдельно. Бесплатное local использование с собственными ключами или локальной моделью описано. Документация не даёт фиксированной цены на конкретный объём личной работы; точная стоимость зависит от модели, tool usage и quota. Developer/API тарифы отдельные. Региональные ограничения на изученных страницах не установлены. Проверять цену и доступность нужно в актуальном аккаунте. [Pricing](https://docs.letta.com/letta-code/pricing)

## 10. Приватность и владение

Local mode оставляет сообщения, память и настройки провайдера на устройстве, но резервная копия на пользователе. Self-hosted App Server переносит контроль на собственную инфраструктуру; отправка prompt внешнему model provider всё равно передаёт ему данные, если inference не локальный. Cloud state требует доверия Letta как хостингу. Git-backed MemFS обещает inspect/version context, но это не заменяет понятный экспорт полной истории или гарантированное удаление cloud-данных. Процедуры полного экспорта и удаления не удалось подтвердить в выбранных источниках. Letta Code назван open source, но точный license field GitHub в полученной выборке не был доступен; лицензию стоит проверить в репозитории до внедрения. Закрытые облачные услуги остаются зависимостью, даже если CLI открыт. [Self-hosting](https://docs.letta.com/self-hosting), [GitHub](https://github.com/letta-ai/letta-code)

## 11. Проверка сценария пользователя

1. **Telegram: исследуй 3 вакансии.** Telegram channel есть. После pairing сообщение идёт в выбранные agent+conversation. Нужен источник веб-поиска и модель. Сможет ли агент создать отдельного persistent Worker для этой задачи, не подтверждено.
2. **Терминал: продолжи кодовый Task.** `letta --agent <id> --conversation <id>` может открыть того же агента и разговор; `letta` возобновляет default conversation. Но связь вакансий с отдельным Worker не переносится автоматически. Разные локальные рабочие каталоги также дают разные execution contexts.
3. **Telegram: статус и уточнение.** Сообщение попадёт в привязанную conversation. Оно не адресует отдельную задачу/исполнителя без пользовательского task-routing слоя. Общая переписка агента возможна, worker status API для пользователя не подтверждён.
4. **Ноутбук закрыт.** Длительная работа, зависящая от локальных tools или запущенного server, не гарантирована. Расписания требуют подключённого сервера. Удалённый VM/компьютер может работать постоянно, но это не переносит туда автоматически все локальные файлы и сессии.
5. **Вернулся завтра.** Conversation и агент можно снова открыть. Сохранение идентичности отдельного Worker, Task, текущего Attempt и возможность безопасно продолжить прерванную runtime session как единый сценарий не подтверждены.

Итого: у Letta есть общие agent/conversation и channel routing, но не подтверждённый эквивалент связки Secretary + persistent Worker + Follow-up + Result в Personal Conversation.

## 12. Преимущества и слабые стороны

**Документированные плюсы:** агент и память не привязаны к одному model provider; память можно смотреть, менять и версионировать; есть CLI, desktop и облачный web; каналы привязываются к agent conversation; local и self-hosted режимы дают контроль над средой. [Models](https://docs.letta.com/configuration/models), [MemFS](https://docs.letta.com/letta-code/memfs)

**Документированные издержки:** пользователю приходится выбирать backend, inference provider, рабочее устройство и permission mode; локальные инструменты требуют доступного устройства; канал beta, а расписания зависят от подключённого runtime. Инструкции о поддержке каналов локальными агентами противоречат друг другу: Channels сообщает, что Local mode недоступен интеграциям, а self-hosting предлагает запускать channels на собственном сервере с local backend. Это реальное препятствие для доверенного onboarding, пока не выяснено, какой путь сейчас поддерживается.

**Внешнее свидетельство:** GitHub issue [#3811](https://github.com/letta-ai/letta-code/issues/3811), опубликованный в августе 2026 года, описывает разделение сообщений Telegram-группы на несколько conversations при выключенных Topics. Это сообщение пользователя, а не воспроизведённый дефект. Оно согласуется с риском сложной маршрутизации, но не позволяет оценить частоту проблемы.

## 13. Выводы для Secretary

Secretary может отличаться не большей «агентностью», а ясным обещанием: один Personal Conversation, задача с явным Worker binding и возвращаемым Result при переключении Telegram/Pi. Letta показывает, что память и выбор модели могут быть постоянными, но оставляет control plane для команд и task state разработчику.

Гипотезы для будущего продукта, не задания на реализацию:

1. **Показывать одной карточкой Task, Worker, Attempt и последний Result.** Это закрывает разрыв Letta между persistent agent/conversation и task lifecycle.
2. **Разрешить follow-up без повторного выбора адресата.** Сохранять Worker binding и отделять steering, очередь, новую Attempt и закрытие Task.
3. **Синхронизировать Telegram и терминал по одной Personal Conversation, не по неявным channel threads.** Явная привязка уменьшит неоднозначность routing.
4. **Показывать, где физически выполняется задача и что остановится при закрытии ноутбука.** Letta разделяет cloud state и локальный execution, но этот нюанс требует от пользователя технической модели.
5. **Сделать безопасный профиль прав очевидным до первого tool call.** В документах Letta `unrestricted` указан как CLI default; Secretary стоит гипотезно рассмотреть менее удивительный approval-first сценарий.

## 14. Открытые вопросы и будущий hands-on тест

Неизвестно, насколько гладко один облачный agent/conversation открывается одновременно в Telegram и CLI; переживает ли активный run потерю сети и перезапуск; как именно устроены approvals в разных UI; какие export/delete и credential-at-rest гарантии действуют; какой deployment сейчас официально поддержан для Channels при Local mode.

Короткий тест после установки: создать cloud agent и conversation; привязать Telegram к той же паре agent/conversation; отправить вопрос в Telegram и продолжить в CLI; попросить параллельное исследование subagents, затем адресовать follow-up тому же исполнителю; включить `standard`, проверить approval и cancel; запустить задачу на локальном файле, закрыть клиент/отключить сеть и проверить восстановление; повторить с remote VM и проверить расписание. Зафиксировать, где видны transcript, worker identity, стоимость, tool activity и ошибки. Не считать описанное выше результатом такого теста.

## 15. Источники

Основные первичные источники:

- [Letta Agent](https://www.letta.com/agent), продуктовая страница.
- [Quickstart](https://docs.letta.com/quickstart) и [Set up Letta](https://docs.letta.com/handbook/setup), установка и onboarding.
- [CLI reference](https://docs.letta.com/letta-code/cli-reference), команды и conversations.
- [Channels](https://docs.letta.com/letta-code/channels) и [Telegram](https://docs.letta.com/letta-code/channels/telegram), каналы и pairing.
- [Permissions](https://docs.letta.com/letta-code/permissions), режимы и правила доступа.
- [Schedules](https://docs.letta.com/letta-code/scheduling), сроки и ограничения фоновой работы.
- [Pricing](https://docs.letta.com/letta-code/pricing), личные и developer планы.
- [Self-hosting](https://docs.letta.com/self-hosting), local runtime и сервер.
- [Workflows](https://docs.letta.com/configuration/workflows) и [Integration patterns](https://docs.letta.com/platform/app-server/integration-patterns), модели делегирования.
- [GitHub repository](https://github.com/letta-ai/letta-code) и [releases](https://github.com/letta-ai/letta-code/releases), открытый проект и активность. Снимок метаданных сделан 2 октября 2026 года.
- Независимый сигнал: [GitHub issue #3811](https://github.com/letta-ai/letta-code/issues/3811), пользовательский отчёт о маршрутизации Telegram, опубликован в августе 2026 года.
