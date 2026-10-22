# Приёмка Secretary alpha.11 на реальных машинах

Дата начала: 2026-10-22.
Tracker: https://github.com/GitHubFoxy/secretary/issues/170
Кандидат: https://github.com/GitHubFoxy/secretary/releases/tag/v0.1.0-alpha.11
Commit: `5ac77cce9ec1952099b663416c58d7ed002ff421`.

## Порядок работы с владельцем

Первоначально владелец подтверждал каждый шаг отдельно. Последнее указание разрешило закончить тестирование только на Mac и затем представить итог. Linux и Windows в завершающем прогоне не использовались.

Владелец разрешил полностью удалить старые установки и данные Secretary без резервных копий и миграции. Авторизация и установка самих Codex и Claude Code сохраняются. Репозиторий исходников и несвязанные файлы сохраняются.

Текущий прогон Mac завершён. Ошибки записаны без исправления продукта. Ниже приведены итог, оставшиеся пробелы и хронологические результаты отдельных проверок.

После проверки владелец разрешил продолжить тестирование Worker E2E и записывать ошибки, отложив исправления. Ошибки приложения Node на Mac и его индикации `Server: Offline` считаются отложенными замечаниями по решению владельца. Они остаются FAIL соответствующих сценариев и не блокируют дальнейший прогон; исправление и повторная приёмка ещё не выполнены.

## Итог Mac на 2026-10-22

Проверен опубликованный `v0.1.0-alpha.11`, revision `5ac77cce9ec1952099b663416c58d7ed002ff421`, на macOS 15.8.1 arm64. Бинарники RC не менялись. Основной сценарий Claude Code работает через CLI. Полная приёмка RC не пройдена.

| Сценарий | Итог | Фактический результат |
| --- | --- | --- |
| Чистая официальная установка, interactive setup/config | PASS | Без исходников, global npm и ручного исправления пакета |
| Secretary Codex: два связанных ответа | PASS | Маркер восстановлен в той же native session |
| Secretary Codex: настоящий Worker | FAIL | Дважды «Создано.», но 0 Secretary Workers; native `spawn_agent` вместо `spawn_worker` |
| Claude Code: Secretary → Worker → Result → Follow-up | PASS | Отдельный Execution Node, прежние native session и workspace |
| Restart и продолжение Claude Worker | PASS | Нет повторного выполнения или дублирующих Results |
| Cancel: terminal status и идемпотентность | PASS | Один `canceled` Result, binding сохранён |
| Cancel: завершение дочерних процессов | FAIL | Shell и sleep остались живы; тестовые остатки очищены вручную |
| Follow-up после Cancel | PASS | Прежняя сессия продолжилась |
| Web: вход, onboarding, ответ, Observer, Follow-up | PASS | Реальный видимый браузер; вход и история сохранены после restart |
| Один текстовый документ | PASS | Загрузка, сохранение, точное скачивание, Worker прочитал код и посчитал 600 рублей |
| Offline Node и reconnect | PASS | Запрос отклонён без создания Attempt; скрытого выполнения после reconnect нет |
| Неавторизованные API-запросы | PASS | HTTP 401 |
| Direct execution без sandbox | PASS | UID пользователя, наследование тестовой переменной, чтение тестового файла вне workspace |
| Повторный setup | PASS | Environment, config, Node identity и остановленный Node state сохранены |
| Ошибка инструмента | FAIL | В тексте неудача и exit 7, в Result `succeeded` |
| Telegram config и диагностика | FAIL | Меню сохранило неполную конфигурацию, Server отказался запускаться, doctor сообщил успех |
| Telegram DM E2E | BLOCKED | Нет настроенного публичного HTTPS origin и входящего owner DM |
| Native Mac app | FAIL, отложено владельцем | Первый pairing не проходит; индикация Server ошибочна |
| Claude без авторизации: диагностика | PASS | Пустой отдельный `CLAUDE_CONFIG_DIR`; оба doctor отклоняют отсутствие auth |
| Полный onboarding без auth, CLI не установлен | NOT RUN | Требуется отдельная чистая среда; основная авторизация сохранена |
| Claude API key на Mac | NOT RUN | Реальные вызовы выполнены с существующим OAuth |

### Зарегистрированные ошибки

1. **MAC-01: ложное создание Codex Worker.** Воспроизведено дважды, включая явное требование использовать `spawn_worker`. Блокирует подтверждение основного Codex E2E. Причина выбора native инструмента не установлена.
2. **MAC-02: Cancel оставляет дочерние процессы.** Native Claude завершился, shell стал orphan, sleep продолжал работать не менее 45.205 секунд. Прежние PID проверены отдельно от дерева Node. Их ручная очистка не считается исправлением Cancel.
3. **MAC-03: успешный статус при неудаче задания.** Result `res_7db3188491eda41f2f60191ab308dce2` имеет `succeeded`, хотя команда вернула 7 и summary сообщает «Задача не выполнена». Текст честный, структурированный итог противоречит заданному критерию успеха.
4. **MAC-04: Telegram включается с неполной конфигурацией.** Меню не запросило обязательный `SECRETARY_TELEGRAM_WEB_BASE_URL`. После restart весь Server не стартовал. `secretary doctor` при этом вернул 0. Telegram отключён штатным меню, Server восстановлен.
5. **MAC-05: clean-install запуск Mac app не выполняет pairing.** CLI Node работает, app Node выходит со status 1. Menu bar сообщает `Server: Offline` при работающем Server. Владелец отложил исправление.
6. **Замечания интерфейса.** Projectless Worker обозначен `Unknown Project`; у завершённого idle Worker видна подпись `Accepted`; canceled summary выглядит как `Claude Code returned no answer`. Это отдельные замечания к понятности состояния.

### Решение по выпуску

- Внутренний Claude preview на этом Mac: основной пользовательский путь подтверждён через CLI. Полная приёмка остаётся FAIL из-за Cancel и обработки неудач.
- Внутренний Codex preview: Secretary отвечает, но Worker E2E не принят.
- Публичная Codex alpha: NO-GO по результатам Mac. Другие платформы в этом прогоне не проверялись.
- Полный Public v1: не принят. Не завершены платформенная матрица, Telegram, сценарии установки без CLI и авторизации, API-key сценарий Claude и остальные типы файлов.

### Состояние Mac после проверки

В `2026-10-22T07:43:56Z` health = `ok`. Работают ровно один Server PID `46144` и один Execution Node PID `41648`; реальные executable и revision соответствуют alpha.11. Node online, оба Worker idle. В БД 2 Worker, 9 Attempt, 9 Result. Незавершённых Attempt нет; у каждого Result ровно одна запись в Personal Conversation. Тестовых orphan PID 33637/33638 больше нет.

Secretary и default Worker используют `claude_code`. Telegram выключен через меню. Существующие Codex и Claude Code остаются авторизованными. Web доступен на `http://127.0.0.1:8081/`; отдельный браузер оставлен для просмотра. Launch-at-login в этом прогоне не принимался: процессы запущены штатными CLI в сохраняемой терминальной сессии.

Доказательства финального состояния: `/private/tmp/secretary-rc-alpha11/macos-final-state.json`. Каталог доказательств содержит безопасные результаты сценариев и снимки экрана. Полные provider traces и auth stores в отчёт не копировались.

## Обязательные сценарии установки

Для каждого из Codex и Claude Code нужно отдельно проверить три исходных состояния:

1. CLI установлен и авторизован. Пользователь уже работает с native harness и хочет установить Secretary.
2. CLI установлен, но не авторизован. Проверить понятный путь входа, корректную диагностику и отсутствие сохранённой неработающей конфигурации.
3. CLI не установлен. Проверить фактическое поведение установщика, подготовку зависимостей и понятную диагностику без скрытого требования global npm.

Установка Secretary, обнаружение CLI, native авторизация, ACP readiness и успешный модельный вызов фиксируются раздельно. Успешный `auth status` не считается live E2E.

Первым выполняется сценарий 1 на Mac и Linux. Windows проверяется с исходным отсутствием авторизации; наличие CLI уточняется при инвентаризации. Сценарии 2 и 3 остаются обязательными последующими проверками. Их следует проводить в отдельной тестовой среде, сохраняя авторизацию основного пользователя.

## Исходное состояние Mac

- macOS 15.8.1, arm64.
- Codex CLI 0.163.0 установлен; `codex login status` завершился с кодом 0.
- Claude Code 2.1.306 установлен; `claude auth status --json`: `loggedIn=true`, `authMethod=oauth_token`, `apiProvider=firstParty`.
- API-key модельный вызов Claude на Mac не проверен.
- Обнаружены старые данные `~/.local/share/secretary`, `~/.config/secretary`, `~/.secretary`, бинарники `~/.local/bin/secretary*`, два приложения Secretary Node и `~/Library/Application Support/SecretaryNode`.
- Загружены launchd labels `dev.secretary.cli` и `dev.secretary.codex-remote-node`.
- `dev.secretary.cli` использует `/private/tmp/t18/secretary`; определение загружено из временного `codex-home/Library/LaunchAgents/dev.secretary.cli.plist`.
- Отдельно работают Server и Node старого стенда `/private/tmp/secretary-live-pr177` на порту 18081.
- `/private/tmp/secretary-rc-alpha11` содержит проверенные установочные пакеты и отчёт проверки RC, а не старую установку.

## Исходное состояние Linux

- SSH alias: `omarchy`, пользователь `coder`, Linux x86_64.
- Работают `secretary.service` и `secretary-node.service`, фактически alpha.8 из `~/.local/share/secretary/releases/secretary-v0.1.0-alpha.8-linux-amd64`.
- Сохранились старые drop-ins `~/.config/systemd/user/secretaryd.service.d`.
- Codex CLI пользователя 0.135.0 установлен; `codex login status` завершился с кодом 0. Managed Codex alpha.8 хранится отдельно внутри Secretary state.
- Claude Code 2.1.270 установлен; `loggedIn=true`, `authMethod=api_key`, `apiProvider=firstParty`.
- Реальный модельный вызов Claude пока не выполнен.
- Обнаружены state `~/.local/share/secretary`, ссылки `~/.local/bin/secretary*` и старый `~/secretary-bridge-poc` со ссылкой `secretary-bridge`.

## Исходное состояние Windows

- SSH alias `elizabeth` доступен, пользователь `Elizabeth`, Windows 11 build 10.0.26200.
- В PATH SSH-сеанса не обнаружены Secretary, Codex, Claude Code, Node.js и npm.
- В проверенных обычных каталогах приложений, процессах, службах и scheduled tasks Secretary не обнаружен.
- Файл `~/.codex/auth.json` существует. Это не доказательство действующей авторизации: Codex CLI пока не найден, login status не выполнялся. Содержимое файла в отчёт не включено.
- Файл `~/.claude/.credentials.json` отсутствует. По сообщению владельца native harness не авторизован.
- После установки CLI состояние авторизации будет проверено фактически; при необходимости владелец выполнит login.

## Хронология проверок

- Запись сценариев: выполнена.
- Инвентаризация Mac, Linux и Windows: выполнена только чтением.
- Очистка Mac: PASS после отдельного подтверждения владельца. Удалены старые службы, процессы, binaries, приложения, state и временные стенды. Backup и migration не выполнялись.
- Проверка после очистки Mac: процессов и загруженных служб Secretary нет, порты 8081 и 18081 свободны, прежние пути отсутствуют. Codex и Claude остаются установленными и авторизованными; native auth/config не изменились.
- Доказательства очистки Mac: `/private/tmp/secretary-rc-alpha11/macos-cleanup.json`.
- Очистка Linux и Windows: NOT RUN.
- Установка alpha.11 на Mac: PASS. Выполнен официальный `scripts/install.sh` из tag `v0.1.0-alpha.11` с `SECRETARY_VERSION=0.1.0-alpha.11` и `SECRETARY_INSTALLER_REF=v0.1.0-alpha.11`. Пакет загружен установщиком из опубликованного релиза; исходники, global npm и ручные исправления установки не использовались.
- Установленная версия: `0.1.0-alpha.11`; revision `5ac77cce9ec1952099b663416c58d7ed002ff421`. `secretary` доступен в PATH через `~/.local/bin/secretary`; `current` указывает на соответствующий release directory.
- `secretary setup --interactive`: PASS, обнаружены `Codex=true` и `Claude Code=true`, выбран `codex`. Setup завершился с кодом 0.
- `secretary doctor`: PASS, код 0, `Doctor found no problems.`; Server и Execution Node пока остановлены.
- `secretary node doctor`: PASS, код 0, `Node doctor found no problems.`
- Codex 0.163.0: ACP `initialize/load/steering` подтверждены, native models=7. Это проверка готовности, а не успешный модельный ответ.
- Native авторизация после установки: Codex `Logged in using ChatGPT`; Claude `loggedIn=true`, `authMethod=oauth_token`, `apiProvider=firstParty`.
- Запуск Server через `secretary start --no-open`: HTTP `/v1/health` = 200, тело `{"status":"ok"}`. Неавторизованный `/v1/web/session` = 401. Первый запуск из короткой tool-сессии сообщил успех, но Server затем исчез без ошибки в журнале. Повтор той же команды в сохраняемой PTY-сессии устойчив; зависимость от завершения tool-сессии вероятна, но ещё не доказана отдельным воспроизведением вне инструмента.
- Первый запуск `~/Applications/Secretary Node.app` через обычный `open`: FAIL. Дочерний Node завершился с `Node is not paired; --pair-token or SECRETARY_NODE_PAIRING_TOKEN is required for first enrollment`. Native app остаётся открытым, но его дочерний Node не работает. Конфигурация указывает на правильный локальный Server; скрытого исправления environment или package не выполнялось. Это отдельный дефект clean-install onboarding Mac, не закрытый успехом CLI.
- Запуск через штатный `secretary node start` в сохраняемой PTY-сессии: PASS. CLI загрузил сохранённый environment, выполнил pairing `Mac.IGD_Rostelecom` и установил outbound WebSocket.
- Server API показывает ровно один Node, `online=true`, `capacity=1`, `draining=false`, `revoked=false`. Heartbeat продвинулся с `2026-10-22T06:57:20.781364Z` до `2026-10-22T06:57:35.782543Z`.
- Inventory: Codex 0.163.0 `ready`, authenticated, 7 models; Claude Code 2.1.306 `ready`, authenticated OAuth, model_ids не объявлены. Модельные вызовы пока NOT RUN.
- Проверены реальные executable через `proc_pidpath`: Server PID 14356 = release `bin/secretaryd`, Node PID 14673 = release `bin/secretary-node`; оба из `secretary-v0.1.0-alpha.11-darwin-arm64`. `secretary status` подтверждает revision `5ac77cce9ec1952099b663416c58d7ed002ff421` у обоих процессов. Node работает отдельным OS process; реальный Dispatch Worker ещё не проверен.
- Доказательства запуска и inventory: `/private/tmp/secretary-rc-alpha11/macos-node-start.json`.
- Secretary Codex, два последовательных реальных ответа: PASS через HTTP owner API опубликованного candidate. Session bootstrap HTTP 201, оба POST `/v1/messages` HTTP 202. Настоящая браузерная приёмка пока NOT RUN.
- Первый запрос попросил запомнить `СОСНА-4826`; ответ: `Маркер СОСНА-4826 запомнен.` Второй запрос не содержал маркера и попросил его вспомнить; ответ: `СОСНА-4826`.
- Оба Secretary turn завершились `succeeded`: `stn_17b54019531d078b5304aa5c54e48c2d` и `stn_20eb4dd4a7ad18f4c7995905b909ab00`. Сохранено ровно два пользовательских сообщения и два Secretary reply, без повторов. Workers в БД: 0.
- Native Codex session `01a16265-d359-7f52-8073-0ef758eda655` содержит оба тестовых сообщения. ACP journal подтверждает два `session/prompt` без нового `session/new`/`session/load` во время проверки. Native workspace: `/var/folders/r8/v8cmn1xd3lg85k461jsnmc240000gn/T/secretary-worker-1842768323`.
- Доказательства: `/private/tmp/secretary-rc-alpha11/macos-codex-two-replies.json`. Native transcript не копировался в отчёт; сохранены только ссылка на файл и безопасные metadata. Ошибочные утверждения временного проверочного скрипта (`completed` вместо реального `succeeded`, сравнение ISO timestamps как строк) исправлены без повторной отправки первого сообщения и без изменения продукта.
- Codex Worker E2E, первый запрос: FAIL. Secretary turn `stn_ca31637fe31d6085060782b03e42f012` завершился `succeeded` и сохранил ответ `Создано.`, но `workers`, `phase4_attempts`, `phase4_results` остались пустыми. Ложное подтверждение создания Worker. Запрос содержал неоднозначное `Не используй инструменты`, поэтому отдельно проверяется уточнённый запрос с явным разрешением lifecycle-инструментов Secretary. Первый FAIL не снимается успехом уточнённого запроса. Доказательства: `/private/tmp/secretary-rc-alpha11/macos-codex-worker.json`.
- Codex Worker E2E, уточнённый запрос: FAIL повторно. Явно разрешены lifecycle-инструменты Secretary, запрошен настоящий `spawn_worker`, ограничения на команды/файлы отнесены только к задаче Worker. Turn `stn_84d1a58b4820d1253bb66c5833aa6b41` опять `succeeded`, ответ `Создано.`, но Worker/Attempt/Result = 0.
- Native transcript показывает `spawn_agent` в 07:04:16.313Z и 07:05:09.152Z вместо Secretary lifecycle `spawn_worker`. Создание native subagent не создаёт Secretary Worker, binding или маршрутизацию Result. Это наблюдение, а не доказанная причина неправильного выбора инструмента. `secretary_runtime_launches` также пуст; доступность MCP отдельно не доказана. Доказательства: `/private/tmp/secretary-rc-alpha11/macos-codex-worker-explicit.json` (без сырых аргументов native tool calls).
- Codex Worker Result, same-Worker Follow-up и native Worker session/workspace continuity: BLOCKED отсутствием реального Secretary Worker. Ручной вызов приватного API, подмена binding и исправление RC не выполнялись.
- Переключение Codex → Claude Code через `secretary config`: PASS, меню → 1 → `claude_code` → 5. Изменены Secretary и default Worker harness штатным меню. `secretary restart --no-open` отклонил неподдерживаемый флаг без побочных эффектов; использованы штатные `secretary stop` и `secretary start --no-open`. Новый Server PID 22902, прежний отдельный Node PID 14673 сохранился. Работающая revision по `secretary status` совпадает с alpha.11.
- Claude Code Secretary reply: PASS. `19 + 23` → `42`, ровно один сохранённый ответ, turn `stn_dea72c9937f29dbbedb2abacb60d25d4` = `succeeded`. Использован существующий OAuth на Mac, API-key сценарий не проверен. Доказательства: `/private/tmp/secretary-rc-alpha11/macos-claude-reply.json`.
- Claude Code Worker spawn и первый Result: PASS. Secretary turn `stn_d3a4efa9689db8fbcc18939f198d0a29` создал ровно одного projectless Worker `wrk_0286c9950ebd9383d53a400cf1a5e1f2` на `Mac.IGD_Rostelecom/claude`. Attempt `att_f4fcff8bd55ed848911e91c15598d7d9` = `succeeded`, Result `res_4e972ba53f106979f163ba3f1f4d2097` с вопросом о строгом/весёлом стиле. Worker затем `idle`.
- Node mapping сохраняет native Claude session `5c37a46f-07de-4ea0-a03f-29535eab5874`, workspace `/Users/beruseruko/.local/share/secretary/node/data/workspaces/secretary-worker-1344021580`. В server Worker workspace пока пуст, фактический Node workspace присутствует в mapping; само по себе это не объявляется ошибкой.
- Native Claude Code same-Worker Follow-up: PASS. Ответ владельца `Выбираю весёлый стиль...` маршрутизирован тому же Worker. Новый Attempt `att_ed0f33b9e23c3281f972c8e86f1a2867` завершился `succeeded`; Result `res_ee72ebee5100c480cdd6268e8fc4ac4e` = `**ЛИПА-7319: Закупки без паники**`. Кодовое слово отсутствовало в Follow-up и восстановлено из прежнего задания.
- После Follow-up в БД всё ещё ровно один Worker, два Attempt и два Result. Оба Result опубликованы в Personal Conversation по одному разу, verbatim. Оба Node mapping ссылаются на прежние native session `5c37a46f-07de-4ea0-a03f-29535eab5874` и workspace. Настоящий Claude transcript найден, содержит исходное задание и продолжение; содержимое transcript в отчёт не копировалось.
- Реальное дерево процессов во время Follow-up: отдельный Node PID 14673 → Claude PID 24117 → `secretary-node` PID 24128 (роль последнего по одному имени не устанавливалась). Это подтверждает выполнение native Worker в отдельном Execution Node, а не embedded LocalNode (#179). Снимок дерева сохранён в основном отчёте сценария.
- Доказательства всего сценария: `/private/tmp/secretary-rc-alpha11/macos-claude-worker.json`. После теста Worker остаётся `idle`, Server и Node работают, конфигурация Secretary/default Worker = `claude_code`. Файлы и каналы ещё NOT RUN.
- Restart continuity Claude Worker: PASS после отдельного подтверждения владельца. Штатные `secretary node stop` и `secretary stop` действительно завершили прежние PID 14673 и 22902. Затем `secretary start --no-open` и `secretary node start` запущены в сохраняемой PTY-сессии. Новые Server PID 27534 и Node PID 27581 проверены через `proc_pidpath`; пути принадлежат пакету alpha.11, работающая revision совпадает с целевой.
- После restart Server health = `ok`, прежний Node `Mac.IGD_Rostelecom` online, heartbeat продвинулся, Node mappings сохранены. До нового Follow-up счётчики остались 1 Worker / 2 Attempt / 2 Result: повторного выполнения при перезапуске не обнаружено.
- Follow-up после restart: Secretary turn `stn_8f72854470b71e5bfae12d7480638387` = `succeeded`, прежний Worker `wrk_0286c9950ebd9383d53a400cf1a5e1f2`, новый Attempt `att_f6a50f731e706e9df3d159826859c39c` = `succeeded`, Result `res_01f81e818eafddab0d4bab19ca6929b8`. Worker правильно повторил прежнее название `ЛИПА-7319: Закупки без паники` и предложил `ЛИПА-7319: Тележка на взлёте`; исходное название и кодовое слово отсутствовали в новом пользовательском сообщении.
- После restart Follow-up native session осталась `5c37a46f-07de-4ea0-a03f-29535eab5874`, workspace остался `/Users/beruseruko/.local/share/secretary/node/data/workspaces/secretary-worker-1344021580`. Новый ввод и ответ находятся в прежнем реальном Claude transcript. Всего 1 Worker / 3 Attempt / 3 Result; каждый Result опубликован ровно один раз. Проверен штатный CLI-путь, дефект приложения menu bar этим не закрыт.
- Доказательства restart continuity: `/private/tmp/secretary-rc-alpha11/macos-restart-continuity.json`. Ошибок продукта в этом сценарии не обнаружено. Первая проверка временного скрипта ошибочно требовала полного пользовательского routing-текста в native Worker transcript; проверка исправлена с учётом штатного удаления Secretary lifecycle-инструкций, дополнительного модельного запроса не было.
- Cancel Claude Worker: partial PASS / overall FAIL. Через штатный owner endpoint `POST /v1/workers/{worker_ref}/message` запущено foreground ожидание `/bin/sleep 180` без записи файлов. Реальный `/bin/sleep` PID 33638 с parent `/bin/zsh` PID 33637 наблюдался в дереве потомков отдельного Node. Attempt `att_8065db745f3697ea0501bcc41e62b97e` действительно был `active` до отмены.
- `POST /v1/workers/{worker_ref}/cancel` вернул HTTP 202. Attempt стал `canceled`, создан один Result `res_fd57dac6b115816d7e60a47b550c63bc` со status `canceled`, failure_code `canceled`, summary `Claude Code returned no answer`. Result опубликован один раз, успешный ответ `ОЖИДАНИЕ-ЗАВЕРШЕНО-6248` не появлялся. Повтор Cancel с тем же idempotency key = HTTP 202 без нового Attempt или Result. Worker остался `idle`, `archived=0`; binding не закрыт.
- FAIL cleanup descendants: native Claude завершился, но `/bin/zsh` PID 33637 стал orphan (PPID 1), а его `/bin/sleep` PID 33638 продолжал работать не менее 45.205 секунд после Cancel. Проверка только текущего дерева Node ошибочно показала бы отсутствие потомков; проверены исходные PID, снятые до Cancel.
- Остаточные процессы принадлежат только этому тесту. После фиксации FAIL выполнена отдельная ручная очистка: SIGTERM тестовому sleep, shell затем завершился сам. Проверено, что оба PID больше не существуют. Это очистка стенда, не исправление или успешная повторная приёмка Cancel в продукте.
- Отдельное UX-замечание Cancel: summary `Claude Code returned no answer` не объясняет владельцу отмену, хотя структурированный status корректно `canceled`. Причина утечки дочерних процессов не исследовалась и код не менялся по просьбе владельца.
- Доказательства Cancel: `/private/tmp/secretary-rc-alpha11/macos-claude-cancel.json`. Итоговые счётчики: 1 Worker / 4 Attempt / 4 Result. Server PID 27534 и Node PID 27581 продолжают работать из alpha.11. Follow-up после Cancel ещё NOT RUN.
- Browser и Telegram: NOT RUN.

## Завершающие проверки Mac

- **Follow-up после Cancel: PASS.** Прежний Worker восстановил `ЛИПА-7319` и `Тележка на взлёте`. Native session и workspace сохранены. Доказательства: `macos-after-cancel.json`.
- **Web: PASS.** Вход bootstrap, четыре шага onboarding, отправка сообщения и реальный ответ `WEB-MAC-8362` выполнены в отдельном видимом Helium. После перезапуска Server браузер восстановил соединение; refresh сохранил session и историю. Observer открыл сохранённые activity и terminal outcomes. Доказательства: `macos-web-login.txt`, `macos-web-onboarding.txt`, `macos-web-refresh.txt`, `macos-web-observer-recovered.txt`, `macos-web-observer.png`.
- **Web Follow-up: PASS.** В Observer введено продолжение и нажата кнопка Follow-up. Result `res_88dc13b9ca8608b7425c6ec4d4efae6a` содержит прежнее кодовое слово, название и `WEB-FOLLOWUP-9426`, виден в браузере и сохранён один раз. Native session осталась `5c37a46f-07de-4ea0-a03f-29535eab5874`; нового Worker нет. Доказательства: `macos-web-worker-followup.json`, `macos-web-followup-complete.txt`.
- **Узкий экран: базовая проверка PASS.** При запросе viewport 390×844 фактическая ширина CSS viewport = 392 и document width = 392. Горизонтального переполнения нет, поле ввода и кнопка Send видны. Это не полная мобильная приёмка. Скриншот: `macos-web-mobile.png`.
- **Документ: PASS.** `mac-acceptance-note.txt`, 236 bytes, artifact `art_02757cc5c335e323820d150d3674d3cd`. SHA256 `1955e06ef85c1071451db0950be2866cd5a746ba8b29be3d69b5c632530b497e`. Сохранённые bytes и authenticated download совпали с fixture. Claude Worker `wrk_f2a03948902b5b9512596d2092ebe75e` вернул `ФАЙЛ-6427` и 600 рублей. Result `res_567ad7690dc278e851f5b02a3376a8ba` виден в браузере и сохранён один раз. Upload выполнен через input.files и штатный обработчик браузера: AXI upload wrapper имел несовместимую схему аргументов. Это ограничение инструмента автоматизации, не ошибка Secretary. Доказательства: `macos-document.json`, `macos-web-file-selected.txt`.
- **API auth: PASS.** Неавторизованные GET conversation/workers/nodes, POST messages и неверный bootstrap вернули HTTP 401. Доказательства: `macos-auth-rejections.json`.
- **Offline Node: PASS.** Штатно остановленный Node дал HTTP 400 на прямое сообщение Worker. Счётчики 6 Attempt / 6 Result не выросли. После restart Node online, маркер `OFFLINE-EXECUTED-5183` не появился, повторного выполнения нет. Доказательства: `macos-node-offline.json`.
- **Повторный setup: PASS.** Interactive setup сохранил Claude. Повторный noninteractive setup при остановленном Node сохранил хеши environment, config, identity и полного node-state. При живом Node node-state меняется из-за heartbeat; это не дефект setup. Доказательства: `macos-repeat-setup.json`.
- **Direct execution: PASS.** Реальные shell-команды Worker вернули UID `501`, унаследованную тестовую переменную `MAC-ENV-9074` и содержимое fixture вне workspace `TEST-ONLY-OUTSIDE-WORKSPACE-7894`. Использовались только тестовые данные. Документация прямо указывает отсутствие дополнительной Worker isolation. Доказательства: `macos-direct-execution.json`.
- **Tool failure: FAIL, MAC-03.** Реальная команда `/bin/sh -c "printf SECRETARY-TOOL-FAIL-7 >&2; exit 7"` не повторялась. Summary честно сообщает неудачу, но terminal Result имеет `succeeded` и пустой failure_code. Доказательства: `macos-tool-failure.json`.
- **Claude без auth: диагностика PASS.** Native `claude auth status` с отдельным пустым `CLAUDE_CONFIG_DIR` и без переданных auth-переменных вернул loggedIn=false и exit 1. Оба doctor завершились с exit 1 и `missing: Claude Code auth`. Основной config/auth не менялся. Это проверка диагностики, а не полного login/setup. Доказательства: `macos-claude-unauthed-readiness.json`.
- **Отсутствующий CLI: NOT RUN.** Ограничение PATH не скрыло Claude: штатный discovery нашёл его через fallback и doctor прошёл. Этот опыт не считается сценарием «CLI не установлен». Доказательства: `macos-missing-discovery.json`.
- **Telegram: configuration FAIL, DM BLOCKED.** Bot API подтвердил доступность тестового бота; webhook отсутствовал, updates/private DM = 0. Штатное меню включило Telegram без публичного origin. Server отказался стартовать с требованием `SECRETARY_TELEGRAM_WEB_BASE_URL`, doctor при этом сообщил успех. Полный DM E2E не выполнен: нет публичного HTTPS origin и owner DM. После записи ошибки Telegram выключен тем же меню, Server восстановлен. Raw startup log после следующего start не сохранился; точное наблюдение записано отдельно. Доказательства: `macos-telegram-precheck.json`, `macos-telegram-config.json`.

Имена файлов доказательств в этом разделе относятся к `/private/tmp/secretary-rc-alpha11/`.

## Оставшиеся проверки

1. После исправлений повторить Mac-сценарии MAC-01–MAC-05 на новом опубликованном candidate. Исправления в рамках текущего прогона не выполнялись.
2. Выполнить Telegram DM E2E с публичным HTTPS origin и тестовым owner DM.
3. Проверить полный onboarding обоих harness без auth и без установленного CLI в отдельной чистой среде.
4. Проверить Claude API-key путь. На Mac использован OAuth.
5. По отдельному указанию продолжить Linux и Windows. Сейчас их установка и службы не менялись.
6. Для полного Public v1 принять остальные заявленные каналы и типы файлов. Один TXT не подтверждает image/video/PDF сценарии.

Секреты, auth stores и полные provider traces в этот отчёт не включаются.
