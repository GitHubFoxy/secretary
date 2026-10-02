# OpenClaw: продуктовый отчёт

**Проверено:** 2026-10-02. **Уверенность:** высокая по функциям, прямо описанным в документации; средняя по собранному UX, поскольку продукт не запускался; низкая по поведению после сбоев и по отдельным каналам, если документация не задаёт контракт. Это исследование источников, не hands-on тест.

## 1. Краткое резюме

OpenClaw сегодня это self-hosted персональный AI-ассистент с Gateway на машине пользователя или его VPS. Он принимает сообщения через web, терминал, мобильные приложения и мессенджеры; использует выбранную модель, инструменты и Markdown-файлы памяти. Проект доступен как MIT open source, а не как закрытая бета или SaaS с единым тарифом. В проверенных материалах нет обозначения «beta» для основного продукта; GitHub показывает регулярные стабильные релизы. Для установки нужны Node/runtime и авторизация модели; мессенджер требует отдельного bot account. Постоянная работа требует обслуживания Gateway.

Лучше всего подходит техническому пользователю, который хочет подключить свои каналы и инструменты к одному самостоятельно размещённому агенту и готов отвечать за его безопасность и доступность. Это близко к запросу на общую Secretary identity между каналами, но не даёт из коробки того же продуктового контракта, что Secretary: Task с закреплённым Worker, который отдельно живёт и адресуется, а его Result попадает в одну Personal Conversation.

## 2. Масштаб и перспективы

На GitHub у `openclaw/openclaw` около **391 тыс. stars** и 82 тыс. forks при проверке 2 октября 2026 года. Релиз `2026.9.7` опубликован 30 сентября; в его metadata указаны 518 прямых commits, 2 818 pull requests и 334 contributors. Страница Releases также показывала `2026.8.34` как extended-stable релиз, опубликованный 2 октября. Это сигналы интереса и разработки, но не число активных пользователей.

Не нашёл официального MAU, числа платящих клиентов или проверяемого benchmark качества. Аудитория OpenClaw как продукта неизвестна. Звёзды репозитория и публикации об агентах в целом нельзя выдавать за аудиторию OpenClaw. [GitHub repository](https://github.com/openclaw/openclaw), [releases](https://github.com/openclaw/openclaw/releases), [release 2026.9.7](https://github.com/openclaw/openclaw/releases/tag/v2026.9.7).

## 3. Установка и доступ

Поддерживаются macOS, Linux и Windows. Требуется Node.js 24.16+ либо 26.1+; документация рекомендует Node 26, а installer может установить runtime. Варианты: локальный Gateway, отдельный постоянно включённый компьютер или пользовательский VPS. Официальный hosted subscription в проверенных материалах не найден. 

Короткий официальный путь на macOS/Linux/WSL2:

```bash
curl -fsSL https://openclaw.ai/install.sh | bash
```

На Windows PowerShell:

```powershell
iwr -useb https://openclaw.ai/install.ps1 | iex
```

Installer открывает onboarding. Quick start обнаруживает доступный вход Claude Code/Codex CLI или API key, просит выбрать соединение и проверяет его реальным completion. Custom setup раскрывает больше настроек. Без обнаруженного рабочего auth нужен ручной выбор провайдера и его credentials. После Quick start foreground Gateway можно остановить `Ctrl+C`, поставить в автозапуск `openclaw gateway install`, проверить `openclaw gateway status` и открыть UI через `openclaw dashboard`. Быстрый Telegram путь требует bot token, затем подтверждения pairing-кода от владельца:

```bash
openclaw channels add --channel telegram --token <bot-token>
```

Нужно заранее иметь аккаунт/доступ к модели или ключ и, для Telegram, создать бота в BotFather. Указанные docs «about 5 minutes» это оценка quickstart, не измерение в этом исследовании; удалённый хост и дополнительные настройки займут больше времени. Источники: [Getting started](https://docs.openclaw.ai/start/getting-started), [CLI onboarding](https://docs.openclaw.ai/start/wizard), [Install](https://docs.openclaw.ai/install), [Telegram setup](https://docs.openclaw.ai/channels/telegram).

## 4. Первые 30 минут: реконструкция по документации

Пользователь устанавливает CLI, выбирает Quick start, подключает и проверяет модель, открывает Control UI и задаёт простой запрос вроде «Составь список трёх вариантов и укажи источники». Первый ожидаемый результат - ответ модели в Home/main session. Затем он включает Telegram, создаёт бота, вставляет token, пишет боту и подтверждает pairing. Для локального компьютера Gateway должен оставаться запущенным; автозапуск ставится отдельно.

Трение: credentials модели, pairing и решение, где держать Gateway. Quick start оставляет полный доступ, а sandbox выключен, поэтому первый ответ не означает безопасно ограниченный доступ к файлам и shell.

## 5. Ежедневный UX и общая история

У OpenClaw есть web Control UI, CLI/TUI, macOS menu-bar app с Quick Chat, iOS/Android apps и Windows Hub. Каналы включают Telegram, WhatsApp, Slack, Discord, Signal, Microsoft Teams и другие. Gateway владеет сессиями и transcript-ами. По умолчанию direct messages попадают в main session; `session.identityLinks` может связать peer identities разных каналов. Групповые чаты и комнаты обычно изолированы. Клиенты одного Gateway используют данные его сессии.

Продолжить Gateway session можно через `openclaw tui <target>`; UI умеет выдать команду `openclaw resume` для продолжения конкретной сессии в терминале. Это не подтверждает автоматическое продолжение сессии из произвольного Pi CLI. Документация предлагает свой TUI и ACP-интеграции; совместимость с уже работающей Pi session как с тем же живым контекстом не подтверждена.

Control UI показывает сессии и активность. Инструменты работы с файлами, shell, browser, web search, сообщениями, вложениями и голосом зависят от канала и plugins. Telegram поддерживает файлы и голосовые заметки. Задачу можно остановить или направить управляющее сообщение; subagents контролируются slash-командами, не отдельным списком задач.

Источники: [Session synchronization](https://docs.openclaw.ai/concepts/session-attachment), [main session](https://docs.openclaw.ai/concepts/main-session), [session management](https://docs.openclaw.ai/concepts/session), [channels](https://docs.openclaw.ai/channels), [macOS app](https://docs.openclaw.ai/platforms/macos).

## 6. Возможности

| Возможность | Что пользователь может сделать | Статус | Источник |
|---|---|---|---|
| Память и персонализация | Просить сохранить предпочтение; модель пишет `USER.md`, `MEMORY.md` и дневные заметки. Поиск памяти использует Markdown-файлы. | Подтверждено документацией | [Memory](https://docs.openclaw.ai/concepts/memory) |
| Импорт памяти | Импортировать Markdown-память из Codex, Claude Code и Hermes. Это перенос заметок, не runtime sessions и не credentials. | Подтверждено | [Memory](https://docs.openclaw.ai/concepts/memory) |
| Skills и обучение | Добавлять `SKILL.md` с процедурами; подключать bundled/community skills. Это инструкции для агента, не обучение базовой модели. | Подтверждено | [Skills](https://docs.openclaw.ai/tools/skills) |
| Browser/computer use | Навигация, клики, ввод и screenshots в выделенном browser профиле; есть вариант подключения к авторизованному браузеру. Node apps дают доступ к возможностям устройства. | Подтверждено, границы зависят от policy | [Browser](https://docs.openclaw.ai/tools/browser) |
| Connectors и MCP | Каналы, plugins, tool integrations; `openclaw mcp serve` для внешнего MCP клиента. | Подтверждено | [Channels](https://docs.openclaw.ai/channels), [ACP/MCP](https://docs.openclaw.ai/tools/acp-agents) |
| Код и файлы | Читать/редактировать workspace, выполнять shell и background processes, запускать coding harness через ACP; выбирать рабочую папку. | Подтверждено | [Tools](https://docs.openclaw.ai/tools/index), [ACP](https://docs.openclaw.ai/tools/acp-agents) |
| Поиск и исследование | `web_search`, `web_fetch`; для JS/login использовать Browser. Search provider может требовать отдельный ключ и плату. | Подтверждено | [Web search](https://docs.openclaw.ai/tools/web) |
| Schedules и proactive work | Cron/automations с сохранёнными заданиями и доставкой в канал; heartbeat периодически активирует main session. | Подтверждено | [Automations](https://docs.openclaw.ai/automation/cron-jobs), [Heartbeat](https://docs.openclaw.ai/heartbeat) |
| Долгие задачи | Запускать background subagent или persistent ACP session, проверять status/log, получать completion в requester session. | Подтверждено; не эквивалент Task/Worker Secretary | [Subagents](https://docs.openclaw.ai/tools/subagents), [ACP](https://docs.openclaw.ai/tools/acp-agents) |
| Совместная работа | Несколько изолированных agents/workspaces, team Gateway и общие сессии; общий Gateway считается одной trust boundary. | Подтверждено с ограничением модели доверия | [Multi-agent](https://docs.openclaw.ai/concepts/multi-agent), [Teams](https://docs.openclaw.ai/start/teams) |

## 7. Делегирование и persistent workers

Есть два разных механизма. Обычные **subagents** запускаются из agent turn в отдельных session, работают в фоне и объявляют результат родительской сессии. Это годится для параллельного исследования и внутренних QA, но run завершается. Команда `/subagents list`, `/subagents info` и `/subagents log` показывает статус и transcript; в Control UI такие runs видны в transcript, но не становятся отдельной sidebar строкой родителя. Это внутреннее делегирование, а не автоматически созданный пользовательский Worker с Task binding.

Для отдельного контекста можно создать видимую persistent session или ACP session. ACP документирует `mode: "session"`, `resumeSessionId` и команды `/acp status`, `/acp steer`, `/acp cancel`, `/acp close`. Follow-up можно направить в существующий runtime по session key, ID или label, пока binding не закрыт, не отсоединён или не истёк. Но OpenClaw не обещает модель «один Task владеет Worker до явного закрытия» с сохранённым binding в одном личном чате.

Для нативных subagent sessions `thread: true` поддерживают Discord и Matrix. Telegram и ряд других каналов не могут создать отдельную thread для ребёнка, и такой spawn отклоняется. Обычный subagent может вернуть объявление результата в исходный чат, но его нельзя считать persistent Worker, которым пользователь напрямую управляет из Telegram. ACP умеет привязываться к текущему разговору, однако это меняет маршрутизацию follow-up в сторону harness; проверенные страницы не обещают именно Telegram-UX, где Secretary продолжает принимать сообщения параллельно с bound worker. Для Hermes предположение об отсутствии subagents здесь не проверялось: он вне назначенного scope, выводов о нём нет.

## 8. Контроль и надёжность

Gateway по умолчанию слушает loopback; неизвестный DM получает pairing code, группы ограничиваются allowlist/mention policy. Но Quick start включает Full Access, а sandbox выключен. Разрешённые инструменты в Full Access работают без отдельного approval; ограниченные permission profiles требуют human approval. Sandbox снижает доступ к host, но не является полной security boundary. Открывать Gateway наружу без auth и сетевой защиты небезопасно.

Gateway хранит session history, routing state и agent state на своей машине; память это обычные Markdown-файлы в workspace. Установка как service переживает закрытие терминала, но не выключение хоста. Для работы при закрытом ноутбуке нужен удалённый Gateway. Automations сохраняют расписания. ACP управляет жизнью harness session и допускает resume, но общей гарантии восстановления active run после падения сети или host нет.

Активность можно смотреть через `/status`, `/subagents` и `/acp status`; история subagent доступна через log/transcript. Отмена ACP прерывает текущий turn, close завершает сессию и снимает binding. Сложные разграничения прав и сообщений доступны в настройках, но требуют понимания конфигурации. Для проверки настроек безопасности есть `openclaw security audit`. Источники: [Security](https://docs.openclaw.ai/gateway/security), [Tool permissions](https://docs.openclaw.ai/gateway/security/tool-permissions), [Sandboxing](https://docs.openclaw.ai/gateway/sandboxing), [Secrets and storage](https://docs.openclaw.ai/gateway/security/secrets-and-storage).

## 9. Стоимость

У проекта нет подтверждённой цены за OpenClaw Gateway или официального hosted paid tier: пользователь запускает ПО сам. Основной переменный расход это модельный provider; API key может тарифицироваться по токенам, а OAuth/CLI/subscription routes имеют правила самого provider. Отдельно могут тарифицироваться web search, voice и другие plugin APIs. Self-hosting на VPS добавляет инфраструктурный счёт; конкретная сумма зависит от провайдера, региона и размера VM.

`/status` показывает оценку ответа при наличии usage metadata и локальной цены; Control UI суммирует оценки из transcript-ов, но это не счёт provider. `/usage` показывает токены и иногда примерную стоимость. Это не общий лимит расходов и не точный invoice. [API usage and costs](https://docs.openclaw.ai/reference/api-usage-costs).

## 10. Приватность и владение

Проект MIT. Self-hosted Gateway держит workspace, credentials и session data у владельца; prompts всё равно передаются настроенному model provider, а сообщения проходят через выбранные chat platforms. Plugins и внешние tools добавляют собственные передачи данных. В репозитории заявлены только ежедневная проверка версии по умолчанию и opt-in anonymous feature stats; проверка версии отключается настройкой. Для безопасности документация рекомендует шифрование диска и выделенного OS user.

Markdown-память читаема и переносима; есть импорт заметок из Hermes, Codex и Claude Code. История Gateway хранится в его session store, и поддержка простого полного экспорта/удаления истории как переносимого пользовательского формата в просмотренных материалах не подтверждена. Привязка к модели снижена за счёт выбора разных providers, но остаётся зависимость от локального Gateway, плагинов, channel accounts и их собственных правил. [README](https://github.com/openclaw/openclaw), [Memory](https://docs.openclaw.ai/concepts/memory), [Telemetry](https://docs.openclaw.ai/gateway/telemetry), [MIT license](https://github.com/openclaw/openclaw/blob/main/LICENSE).

## 11. Проверка сценария пользователя

Сценарий: «Telegram: исследуй 3 вакансии; терминал: продолжи кодовый Task; Telegram: статус и уточнение; закрыл ноутбук; вернулся завтра».

| Шаг | Что подтверждено | Чего нет или что неизвестно |
|---|---|---|
| Telegram: исследовать 3 вакансии | Telegram канал, web search и browser tools документированы. Сообщения DM могут быть частью main session. | Нет гарантии качества/полноты результатов или выделенного persistent Worker на каждую вакансию. |
| Терминал: продолжить кодовый Task | OpenClaw TUI и `openclaw resume` могут продолжить Gateway-owned session; ACP умеет persistent coding session. | Не подтверждено продолжение уже открытого Pi Task как того же runtime session. Это возможно лишь если работа была начата через совместимый OpenClaw session/harness и выбран нужный session key. |
| Telegram: статус и уточнение | Background completion может вернуться в requester session; ACP status/steer и subagent list/log дают контроль. | Telegram не поддерживает отдельные child threads для thread-bound subagent. Не подтвержден единый экран статуса для всех задач и каналы не обязаны показывать полный transcript друг друга. |
| Закрыл ноутбук | Если Gateway на другом доступном хосте, Gateway может оставаться запущенным. Для этого есть обычная self-hosted установка на VPS. | Если ноутбук был host и выключился/уснул, Telegram endpoint и выполнение недоступны. Восстановление активного runtime после прерывания не гарантировано общей документацией. |
| Вернулся завтра | Gateway хранит историю; persistent ACP session можно адресовать и resume, пока runtime/session доступна. | Не подтверждено, что Worker process переживёт ночь или что любой его harness восстановит сессию после restart. Нет общего lifecycle Task/Worker с Result и явным закрытием по образцу Secretary. |

Общая Personal Conversation между Telegram и терминалом достижима через один Gateway и настроенную identity/session routing. Это не объединяет произвольные сессии Pi и OpenClaw. Subagent result может вернуться в родительский чат; persistent worker и его Task binding требуют отдельного управления сессией.

## 12. Преимущества и слабые стороны

**Подтверждённый UX:** один Gateway связывает DM-каналы; TUI и Control UI используют общие сессии. Есть локальная память, инструменты, расписания, обычные subagents и persistent ACP sessions. Пользователь выбирает host и model provider.

**Конкретные слабые стороны:** значимая часть опыта зависит от настройки Gateway, credentials, каналов и policy. Полный доступ является Quick start default, sandbox выключен, а ограничение полномочий требует осознанной настройки. Нативные subagents чаще являются фоновыми runs с результатом родителю, а не адресуемыми Worker-ами. Persistent ACP есть, но канал thread binding зависит от адаптера; Telegram не позволяет создать отдельный child thread. Управление состоянием распределено между slash-командами и Control UI, а не собрано вокруг пользовательских Task.

**Независимое свидетельство, не тест текущей версии:** KrebsOnSecurity 8 марта 2026 года пересказал публичный эпизод, где Summer Yue сообщила о массовом удалении писем OpenClaw и не смогла остановить действие из телефона. Это единичный описанный инцидент, не контролируемая проверка и не доказательство поведения версии октября. Он согласуется с документированным риском выдачи агенту широких прав, но не заменяет собственную проверку актуальных ограничений. [KrebsOnSecurity](https://krebsonsecurity.com/2026/03/how-ai-assistants-are-moving-the-security-goalposts/).

## 13. Выводы для Secretary

Стоит изучить: единый Gateway-owned transcript для web/terminal/mobile; понятное продолжение конкретной session в терминале; простую явную identity linking; разные по назначению background run и persistent session; status/log/cancel/steer без необходимости читать сырой лог; оценку model usage рядом с ответом. Не стоит копировать конфигурационный UX, при котором для ясного task lifecycle нужно знать session keys и команды runtime.

Возможная дифференциация Secretary не в количестве каналов или инструментов, а в надёжной Personal Conversation, которой принадлежат Task, Worker binding и Result независимо от того, где пользователь написал. Это проектная гипотеза по контексту, не утверждение о готовом поведении Secretary.

Гипотезы для последующего обсуждения:
1. Показывать одну общую беседу и источник каждого ответа, сохраняя единый порядок Conversation entries. Это упрощает переключение каналов без слияния чужих identity.
2. Делать Task и Worker видимыми объектами с понятными статусом, последним обновлением и follow-up target. Иначе persistent session остаётся скрытой runtime сущностью.
3. Отделить Steering от Queued message и Cancel, показывая пользователю, когда сообщение попадёт в работающий Worker. Это решает неоднозначность «он прочитал или нет?».
4. Сделать Result отдельным сохранённым сообщением Personal Conversation с Task binding, terminal status и ссылками на artifacts, не заставляя Secretary пересказывать или менять вывод Worker.
5. Предупреждать о недоступности Execution node и исходе незавершённого Attempt после рестарта до обещания resumability. OpenClaw показывает, насколько рискованно полагаться на общее «persistent» без runtime-specific гарантий.

## 14. Открытые вопросы и hands-on план

Не выяснены: сохранение ACP/native session после выключения host и Gateway restart; как именно Telegram связывает persistent ACP с текущим DM; доставка статусов и follow-up в раздельных каналах; экспорт/удаление полной истории; реальные лимиты и цена при типовом использовании. Для будущего теста на изолированной машине: подключить одну модель и Telegram, связать DM identity с web/TUI, создать persistent ACP code task, параллельно запустить обычный subagent, отправить уточнение и cancel из Telegram, проверить transcript/result/status, перезапустить Gateway и host, затем проверить resume и cost report. До такого теста не следует обещать, что сценарий пользователя гарантирован.

## 15. Источники

Большинство страниц без даты публикации; материалы проверены 2026-10-02. Основные первичные источники:

- [Getting started](https://docs.openclaw.ai/start/getting-started), [Install](https://docs.openclaw.ai/install), [Telegram](https://docs.openclaw.ai/channels/telegram).
- [Main session](https://docs.openclaw.ai/concepts/main-session), [Session synchronization](https://docs.openclaw.ai/concepts/session-attachment).
- [Subagents](https://docs.openclaw.ai/tools/subagents), [Thread-bound sessions](https://docs.openclaw.ai/tools/subagents/thread-bound-sessions), [ACP sessions](https://docs.openclaw.ai/tools/acp-agents/sessions).
- [Security](https://docs.openclaw.ai/gateway/security), [Tool permissions](https://docs.openclaw.ai/gateway/security/tool-permissions), [Sandboxing](https://docs.openclaw.ai/gateway/sandboxing).
- [API usage and costs](https://docs.openclaw.ai/reference/api-usage-costs), [Memory](https://docs.openclaw.ai/concepts/memory), [GitHub repository](https://github.com/openclaw/openclaw), [Releases](https://github.com/openclaw/openclaw/releases), [MIT license](https://github.com/openclaw/openclaw/blob/main/LICENSE).
