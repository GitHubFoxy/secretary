# Журнал решений: первый проход

Это предварительный журнал: полный пофайловый анализ, в том числе всех особых случаев и тестов, ещё не завершён. Номера строк относятся к состоянию `0f487bf`. Реализация не менялась. История `git log --name-only` показывает, что изменения MCP, настроек и подтверждений уже затрагивали несколько перечисленных файлов.

## Исправления

1. Разместить описание MCP-инструмента, проверку доступа и обработчик рядом; общий транспорт должен только находить и вызывать инструмент.
   - Скрытая связь: `server/mcp.ts:28` объявляет имена и схемы инструментов отдельно от выбора обработчика в `server/mcp.ts:127`.
   - Скрытая связь: `server/persona.ts:40` и `server/persona.ts:87` повторяют инструкции об одних и тех же инструментах для двух ролей.
   - Лишняя связь: `server/mcp.ts:106` одновременно создаёт реализации нескольких независимых возможностей и принимает их как зависимости.

2. Представить запрос подтверждения и его результат как единый контракт; общие состояния и оформление карточки оставить общими, а содержимое и действия конкретной возможности — рядом с ней.
   - Скрытая связь: `server/chat.ts:85` отдельно сопоставляет события запроса и результата для skills, secrets и sudo, а `web/src/App.svelte:370` заново связывает их по виду и id.
   - Скрытая связь: `server/index.ts:87` и `web/src/components/MessageItem.svelte:77` отдельно перечисляют команды и карточки трёх подтверждений.
   - Повторение: `web/src/components/MessageItem.svelte:85` и `web/src/components/MessageItem.svelte:95` повторяют ввод пароля, кнопки и состояние ошибки.
   - Лишняя связь: `web/src/components/MessageItem.svelte:25` использует состояние с именем sudo для независимого подтверждения skill.
   - Скрытая связь: `web/src/App.svelte:477` передаёт secret-команды через интерфейс с именами `onsudo` и `sudoId`.

3. Вынести синхронизацию черновика в самостоятельную возможность с собственным состоянием и обработкой подключения.
   - Лишняя связь: `web/src/App.svelte:136` связывает проверку занятости перед обновлением с внутренним состоянием черновиков и ряда других возможностей.
   - Скрытая связь: `web/src/App.svelte:162` и `server/index.ts:93` вместе определяют протокол синхронизации черновиков, включая идентификатор вкладки.
   - Лишняя связь: `web/src/App.svelte:224` обрабатывает события черновика внутри общего обработчика сообщений приложения.

4. Сосредоточить протокол камеры и её жизненный цикл в одной клиентской возможности, согласованной с серверным контрактом.
   - Скрытая связь: `web/src/App.svelte:207` и `server/camera.ts:7` отдельно управляют состоянием доступности и ожидающими запросами камеры.
   - Лишняя связь: `web/src/App.svelte:383` объединяет управление Voice Call, камерой, снимком экрана и отправкой сообщений в одном объекте.

5. Сделать привязку оригинала к изображению явной в данных сообщения.
   - Скрытая связь: `web/src/components/MessageItem.svelte:61` сопоставляет оригиналы и изображения по неявному порядку в массивах.

6. Согласовать границы возможностей настроек: маршруты и обработку каждой возможности держать вместе, общий HTTP-адаптер оставить входной точкой.
   - Скрытая связь: `server/index.ts:216` содержит HTTP-операции skills отдельно от `server/skills.ts:18` и интерфейса `web/src/components/SkillsScreen.svelte:12`.
   - Скрытая связь: `server/index.ts:226` содержит HTTP-операции memory отдельно от `server/memory.ts:15` и интерфейса `web/src/components/MemoryEditor.svelte:13`.

7. Явно передавать общие экземпляры возможностей в MCP и HTTP, сохранив независимое управление их состоянием.
   - Скрытая связь: `server/index.ts:61` создаёт общие `Skills`, `Secrets` и `Sudo`, но `server/mcp.ts:106` допускает создание других экземпляров с отдельным состоянием.

8. Отделить сохранение событий от проекции для поиска, сохранив атомарность записи и обновления индекса.
   - Лишняя связь: `server/database.ts:2` импортирует представление чата, а `server/database.ts:62` использует его для наполнения поискового индекса.
   - Скрытая связь: `server/database.ts:48` достраивает индекс при открытии базы, но изменение правил `chatMessage` само по себе не перестраивает существующие записи.

9. Выделить общую подготовку секретов конфигурации внешних MCP-серверов, оставив формат аргументов каждому harness.
   - Повторение: `server/claude.ts:8` и `server/codex.ts:19` независимо разрешают реестр, создают случайные переменные окружения и сопоставляют им секреты.

10. Определить обработку ответа harness и правила скрытого ответа один раз, затем передавать результат в чат, синтез и отчёт.
   - Скрытая связь: `server/chat.ts:130`, `server/speech.ts:96` и `server/sessions.ts:139` независимо проверяют префикс `NO_REPLY`.
   - Повторение: `server/speech.ts:93` повторно разбирает события Claude Code и Codex, которые также разбирает `server/chat.ts:30`.

11. Явно создавать хранилище сопряжения при запуске и передавать его потребителям.
   - Скрытая связь: `server/auth.ts:6` при импорте создаёт каталог, открывает базу и изменяет её схему, поэтому одно лишь подключение модуля меняет состояние Host.

## Special cases

Решения предварительные: при реализации проверить внешние ограничения и поведение на ошибках.

- `server/mcp.ts:114` — убрать молчаливое подавление ошибок моделей: возвращать частичный результат с явной ошибкой для соответствующего harness.
- `server/harness.ts:46` — оставить обработку уже завершённого процесса, но отличать её от непредвиденной ошибки остановки.
- `server/harness.ts:52` — убрать молчаливую потерю ошибки записи в stdin, передав её в результат хода.
- `server/harness.ts:55` — оставить ограниченное ожидание stderr: дочерний процесс инструмента может удерживать поток открытым.
- `server/codex.ts:91` — уточнить и отображать ошибку прерывания, если она влияет на результат; сейчас она подавляется.
- `server/mcp.ts:23` — оставить запасное значение версии при отсутствии сборки, но не скрывать произвольную ошибку чтения или повреждённый JSON.
- `server/secrets.ts:36` — оставить продолжение очереди после ошибки предыдущей записи, сохранив передачу ошибки исходному вызывающему.
- `server/external-mcp.ts:25` — оставить попытку очистки временного файла, но не терять неожиданную ошибку очистки.
- `server/preview.ts:63` — различать отсутствие срока и другие ошибки чтения вместо общего пустого значения.
- `server/smart-turn.ts:39` — оставить ограничение ожидания внешнего процесса и проверить, что запасной результат соответствует договорённости Voice Call.
- `server/update.ts:47` — оставить ограниченные повторы при запуске обновлённого сервера, проверив откат после исчерпания попыток.
- `web/src/connection.js:28` — оставить повтор подключения с ограниченной задержкой: сеть может временно пропасть.
- `web/src/App.svelte:210` — убрать молчаливое подавление ошибки объявления камеры или явно восстановить состояние при переподключении.
- `web/src/App.svelte:214` — передавать ошибку ответа камеры в состояние Voice Call, чтобы запрос не завершался только по тайм-ауту.
- `web/src/App.svelte:30` — оставить восстановление после испорченного необязательного локального кэша.
- `web/src/components/ProfileScreen.svelte:45` — показывать ошибку загрузки настроек вместо молчаливого пустого состояния.
- `web/src/updates.js:12` — оставить текущую версию при отсутствии сети: проверка обновления необязательна для работы.
- `server/skills.ts:59` — убрать соглашение `session = 0` для восстановления версии; явно разделить сохранение версии и предложение владельцу.
- Отключённых тестов по поиску `test.skip`, `describe.skip`, `it.skip`, `test.todo`, `describe.todo` не найдено; остальные формы требуют отдельной проверки.

## Порядок работ

1. Завершить обследование всех файлов и особых случаев, сверить граф импортов и историю изменений; проверка: все области охвачены, каждая запись ссылается на существующую строку.
2. **Сделано.** Согласовать контракт подтверждений и его преобразование в историю; проверка: сценарии skill, secret и sudo, включая отказ, тайм-аут и перезагрузку. После контракта клиентскую карточку и серверную реализацию можно делать параллельно в разных файлах.
3. **Сделано.** Сблизить описание MCP-инструментов и обработчики; проверка: MCP-тесты доступности, схем и прав обеих ролей.
4. **Сделано.** Убрать неявное создание отдельных экземпляров возможностей; проверка: запрос MCP виден HTTP и обрабатывается тем же состоянием. Шаги 3 и 4 затрагивают `server/mcp.ts`, выполнять последовательно.
5. **Сделано.** Выделить синхронизацию черновиков; проверка: две вкладки, переподключение и смена Session. Можно параллельно с шагом 6 при раздельных изменениях `App.svelte` либо после выделения границ.
6. **Сделано.** Выделить протокол камеры; проверка: включение, отключение, запрос кадра и обрыв соединения. Исправлена ошибка: после переподключения включённая камера снова объявляется серверу.
7. **Сделано.** Явно связать оригиналы с изображениями; проверка: несколько фотографий, видео и прежняя история. Исправлена ошибка выбора первого видео вместо соответствующего оригинала.
8. **Сделано.** Разместить маршруты настроек рядом с возможностями; проверка: операции skills и memory и ошибки версий. Можно параллельно со шагом 7: файлы разные.
9. **Сделано.** Отделить индекс поиска от хранения событий и проверить сохранение индекса при записи и обновлении правил проекции. Индекс перестраивается при изменении версии проекции; версию нужно менять вместе с её правилами.
10. **Сделано.** Согласовать подготовку внешних MCP и проверку скрытых ответов; проверить оба harness, отчёты и синтез. Можно параллельно со шагом 9: файлы разные.
11. **Сделано.** Перенести открытие базы сопряжения в явное создание; проверить сопряжение и перезапуск. Можно параллельно со шагами 9 и 10: файлы разные.
12. **Частично сделано.** После структурных изменений проверить Comments и Tests: удалить комментарии, пересказывающие код, и убрать или переписать тесты, не проверяющие поведение; затем запустить тесты сервера и web и проверить приложение.


### Проверка шага 12

- Удалены комментарии, пересказывающие код; сохранены объяснения ограничений, причин и контрактов. Исправлен тест соединения, который не ожидал отклонения запроса.
- Добавлены проверки синхронизации черновиков, камеры после переподключения, сопоставления подтверждений, идентификаторов оригиналов, перестроения поискового индекса и завершения MCP-запроса секрета после подтверждения. Тест MCP вызывает подтверждение через общий объект сервера, а не через интерфейс.
- `bun test`: server — 100 успешно, web — 46 успешно. Vite собрал интерфейс в `/tmp/secretary-step12-build`; изолированный сервер с временным `HOME` выдал страницу входа и после сопряжения отдал HTML и JavaScript сборки.
- Не проверено: карточка в настоящем браузере, полный путь MCP → карточка → HTTP-ответ, а также пользовательские сценарии камеры и медиа в браузере. Helium установлен, но в окружении нет графической сессии; скилл better-axi запрещает headless.
- Перечень тестов и подозрительные проверки просмотрены, но полный ручной разбор каждого теста на связь с внутренним устройством и способность поймать регрессию ещё не завершён. Шаг 1 по-прежнему отложен до финальной проверки.

## Инвентаризация файлов и граф импортов
Ниже перечислены 128 файлов заданной области и буквальные импорты, найденные поиском в исходном тексте, включая зависимости вне области. Это машинный проход, а не отметка о завершённом ручном разборе каждого файла. Импорты в строках тестовых сценариев могут быть ложными совпадениями; динамические выражения не раскрыты. Для направлений зависимостей подтверждённое замечание записано в исправлении 8, полная оценка изменчивости остаётся в шаге 1.
- `server/auth.test.ts` — сканирован; импортирует `bun:test`, `node:fs`, `node:os`, `node:path`, `./auth`.
- `server/auth.ts` — сканирован; импортирует `bun:sqlite`, `node:crypto`, `node:fs`, `node:os`.
- `server/camera.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./camera`.
- `server/camera.ts` — сканирован; импортирует `./files`, `./images`.
- `server/chat.test.ts` — сканирован; импортирует `bun:test`, `bun:sqlite`, `./chat`.
- `server/chat.ts` — сканирован; импортирует `bun:sqlite`, `./images`, `./files`.
- `server/claude.ts` — сканирован; импортирует `./external-mcp`, `./harness`.
- `server/codex.ts` — сканирован; импортирует `./external-mcp`, `./commands`, `./harness`, `./protocol/v2/ThreadStartParams`, `./protocol/v2/TurnStartParams`.
- `server/commands.test.ts` — сканирован; импортирует `bun:test`, `./commands`.
- `server/commands.ts` — сканирован; импортирует `./harness`.
- `server/config.ts` — сканирован; импортирует `node:os`, `node:path`.
- `server/database.test.ts` — сканирован; импортирует `bun:test`, `bun:sqlite`, `node:fs`, `node:os`, `node:path`, `./database`.
- `server/database.ts` — сканирован; импортирует `bun:sqlite`, `./chat`, `node:fs`, `node:crypto`.
- `server/drafts.ts` — сканирован; импортирует `bun:sqlite`.
- `server/external-mcp.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./external-mcp`, `./secrets`.
- `server/external-mcp.ts` — сканирован; импортирует `node:fs`, `node:path`, `./secrets`.
- `server/files.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./files`, `./chat`, `./database`, `./sessions`, `node:assert`.
- `server/files.ts` — сканирован; импортирует `node:fs/promises`, `node:fs`, `node:crypto`, `node:path`, `./config`.
- `server/harness.test.ts` — сканирован; импортирует `bun:test`, `node:fs`, `node:os`, `node:path`, `./claude`, `./codex`, `./harness`, `./persona`.
- `server/harness.ts` — сканирован; импортирует `./images`.
- `server/images.test.ts` — сканирован; импортирует `bun:test`, `./images`.
- `server/images.ts` — сканирован; импортирует нет найденных импортов.
- `server/index.ts` — сканирован; импортирует `./skills`, `./observability`, `./camera`, `./preview`, `./auth`, `./commands`, `./references`, `./chat`, `./claude`, `./codex`, `./config`, `./files`, `./database`, `./mcp`, `./memory`, `./sudo`, `./secrets`, `./external-mcp`, `./drafts`, `./notes`, `./models`, `./persona`, `./sessions`, `./speech`, `./voice`, `./smart-turn`.
- `server/mcp-exec.ts` — сканирован; импортирует нет найденных импортов.
- `server/mcp.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./database`, `./harness`, `node:fs`, `./mcp`, `./persona`, `./sessions`, `./observability`, `./secrets`, `./external-mcp`.
- `server/mcp.ts` — сканирован; импортирует `./skills`, `node:fs`, `./config`, `./preview`, `./files`, `./models`, `./persona`, `./session`, `./sessions`, `./worktree`, `./sudo`, `./secrets`.
- `server/memory.test.ts` — сканирован; импортирует `bun:test`, `node:fs`, `node:os`, `node:path`, `./memory`.
- `server/memory.ts` — сканирован; импортирует `node:crypto`, `node:fs`, `node:path`.
- `server/models.test.ts` — сканирован; импортирует `bun:test`, `./models`.
- `server/models.ts` — сканирован; импортирует `./harness`.
- `server/notes.ts` — сканирован; импортирует `bun:sqlite`.
- `server/observability.ts` — сканирован; импортирует `node:fs`, `./claude`, `./codex`, `./mcp`, `./external-mcp`, `./memory`, `./persona`, `./secrets`, `./skills`, `./session`, `./sessions`, `./harness`.
- `server/persona.ts` — сканирован; импортирует `./skills`, `node:fs`.
- `server/preview.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./preview`.
- `server/preview.ts` — сканирован; импортирует `node:fs`, `node:fs/promises`, `node:crypto`, `node:path`, `./config`.
- `server/queue.ts` — сканирован; импортирует `bun:sqlite`, `./database`, `./images`, `./files`.
- `server/references.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./references`.
- `server/references.ts` — сканирован; импортирует нет найденных импортов.
- `server/schedules.test.ts` — сканирован; импортирует `bun:test`, `./database`, `./queue`, `./schedules`, `./sessions`.
- `server/schedules.ts` — сканирован; импортирует `bun:sqlite`, `./sessions`.
- `server/secrets.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./secrets`.
- `server/secrets.ts` — сканирован; импортирует `node:fs/promises`, `node:path`, `./config`.
- `server/security-headers.test.ts` — сканирован; импортирует `bun:test`, `node:fs`, `node:os`, `node:path`.
- `server/session.test.ts` — сканирован; импортирует `bun:test`, `bun:sqlite`, `./chat`, `./database`, `./drafts`, `./notes`, `./harness`, `./session`, `./sessions`.
- `server/session.ts` — сканирован; импортирует `bun:sqlite`, `./chat`, `./database`, `./harness`, `./images`, `./files`, `./queue`.
- `server/sessions.ts` — сканирован; импортирует `./schedules`, `./database`, `bun:sqlite`, `./chat`, `./config`, `./harness`, `./persona`, `./session`, `./speech-text`.
- `server/skills-routes.test.ts` — сканирован; импортирует `bun:test`, `bun:sqlite`, `node:crypto`, `node:fs`, `node:os`, `node:path`, `./secrets`, `./external-mcp`.
- `server/skills.test.ts` — сканирован; импортирует `bun:test`, `node:fs`, `node:os`, `node:path`, `./skills`.
- `server/skills.ts` — сканирован; импортирует `node:fs`, `node:crypto`.
- `server/smart-turn.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./smart-turn`.
- `server/smart-turn.ts` — сканирован; импортирует `node:os`, `node:path`, `bun`.
- `server/speech-text.test.ts` — сканирован; импортирует `bun:test`, `./speech-text`.
- `server/speech-text.ts` — сканирован; импортирует нет найденных импортов.
- `server/speech.test.ts` — сканирован; импортирует `bun:test`, `./speech`.
- `server/speech.ts` — сканирован; импортирует `node:https`, `node:path`, `./config`, `./speech-text`, `node:http`.
- `server/sudo.test.ts` — сканирован; импортирует `bun:test`, `./sudo`, `./database`.
- `server/sudo.ts` — сканирован; импортирует нет найденных импортов.
- `server/update.ts` — сканирован; импортирует `bun:sqlite`, `node:fs`, `node:os`, `node:path`.
- `server/voice.test.ts` — сканирован; импортирует `bun:test`, `node:fs/promises`, `node:os`, `node:path`, `./voice`.
- `server/voice.ts` — сканирован; импортирует `node:fs/promises`, `node:os`, `node:path`.
- `server/worktree.ts` — сканирован; импортирует `node:crypto`, `node:path`.
- `web/src/activity-status.js` — сканирован; импортирует нет найденных импортов.
- `web/src/activity-status.test.js` — сканирован; импортирует `bun:test`, `./activity-status.js`.
- `web/src/agents.js` — сканирован; импортирует нет найденных импортов.
- `web/src/agents.test.js` — сканирован; импортирует `bun:test`, `./agents.js`.
- `web/src/browser-notifications.js` — сканирован; импортирует нет найденных импортов.
- `web/src/browser-notifications.test.js` — сканирован; импортирует `bun:test`, `./browser-notifications.js`.
- `web/src/codex-limits.js` — сканирован; импортирует нет найденных импортов.
- `web/src/codex-limits.test.js` — сканирован; импортирует `bun:test`, `./codex-limits.js`.
- `web/src/components/sleep-timer.js` — сканирован; импортирует нет найденных импортов.
- `web/src/components/sleep-timer.test.js` — сканирован; импортирует `bun:test`, `./sleep-timer.js`.
- `web/src/connection.js` — сканирован; импортирует нет найденных импортов.
- `web/src/connection.test.js` — сканирован; импортирует `bun:test`, `./connection.js`.
- `web/src/file-size.js` — сканирован; импортирует нет найденных импортов.
- `web/src/history.js` — сканирован; импортирует нет найденных импортов.
- `web/src/history.test.js` — сканирован; импортирует `bun:test`, `./history.js`.
- `web/src/interrupt-feedback.js` — сканирован; импортирует нет найденных импортов.
- `web/src/interrupt-feedback.test.js` — сканирован; импортирует `bun:test`, `./interrupt-feedback.js`.
- `web/src/main.js` — сканирован; импортирует `svelte`, `./App.svelte`, `./app.css`.
- `web/src/markdown.js` — сканирован; импортирует `marked`, `dompurify`.
- `web/src/markdown.test.js` — сканирован; импортирует `bun:test`, `jsdom`, `./markdown.js`.
- `web/src/prepare-image-worker.js` — сканирован; импортирует нет найденных импортов.
- `web/src/prepare-image.js` — сканирован; импортирует нет найденных импортов.
- `web/src/prepare-image.test.js` — сканирован; импортирует `bun:test`.
- `web/src/prepare-video.js` — сканирован; импортирует `./voice.js`.
- `web/src/slash-commands.js` — сканирован; импортирует нет найденных импортов.
- `web/src/slash-commands.test.js` — сканирован; импортирует `bun:test`, `./slash-commands.js`.
- `web/src/speech.js` — сканирован; импортирует нет найденных импортов.
- `web/src/speech.test.js` — сканирован; импортирует `bun:test`, `./speech.js`.
- `web/src/updates.js` — сканирован; импортирует нет найденных импортов.
- `web/src/updates.test.js` — сканирован; импортирует `bun:test`, `./updates.js`.
- `web/src/viewport.js` — сканирован; импортирует нет найденных импортов.
- `web/src/viewport.test.js` — сканирован; импортирует `bun:test`, `./viewport.js`.
- `web/src/voice-trim.test.js` — сканирован; импортирует `bun:test`, `./voice.js`.
- `web/src/voice-turn.js` — сканирован; импортирует нет найденных импортов.
- `web/src/voice-turn.test.js` — сканирован; импортирует `bun:test`, `./voice-turn.js`.
- `web/src/voice-vad.js` — сканирован; импортирует нет найденных импортов.
- `web/src/voice-vad.test.js` — сканирован; импортирует `bun:test`, `./voice-vad.js`.
- `web/src/voice.js` — сканирован; импортирует нет найденных импортов.
- `web/src/voice.test.js` — сканирован; импортирует `bun:test`, `./voice.js`.
- `web/src/App.svelte` — сканирован; импортирует `svelte`, `./browser-notifications.js`, `./connection.js`, `./history.js`, `./interrupt-feedback.js`, `./updates.js`, `./viewport.js`, `./components/TopBar.svelte`, `./activity-status.js`, `./components/Notes.svelte`, `./components/SessionDrawer.svelte`, `./components/SettingsPanel.svelte`, `./components/MessageList.svelte`, `./voice.js`, `./speech.js`, `./components/Composer.svelte`, `./components/VoiceMode.svelte`, `./components/Modal.svelte`, `./components/ProfileScreen.svelte`, `./components/DownloadsScreen.svelte`, `./components/CodexLimits.svelte`, `./components/SpeakingPill.svelte`, `./agents.js`, `@capacitor/core`.
- `web/src/components/AgentView.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/AttachmentPopover.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/AttachmentStrip.svelte` — сканирован; импортирует `./Icon.svelte`, `../file-size.js`.
- `web/src/components/CodexLimits.svelte` — сканирован; импортирует `../codex-limits.js`.
- `web/src/components/Composer.svelte` — сканирован; импортирует `svelte`, `./Icon.svelte`, `../slash-commands.js`, `./AttachmentPopover.svelte`, `./AttachmentStrip.svelte`, `./VideoRecorderOverlay.svelte`, `../voice.js`, `../prepare-image.js`, `../prepare-video.js`.
- `web/src/components/DownloadsScreen.svelte` — сканирован; импортирует `svelte`, `@capacitor/core`, `./Icon.svelte`.
- `web/src/components/Icon.svelte` — сканирован; импортирует нет найденных импортов.
- `web/src/components/InstructionsEditor.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/Markdown.svelte` — сканирован; импортирует `svelte`, `../markdown.js`, `./Icon.svelte`.
- `web/src/components/McpServers.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/MediaPlayer.svelte` — сканирован; импортирует `svelte`, `./sleep-timer.js`.
- `web/src/components/MemoryEditor.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/MessageItem.svelte` — сканирован; импортирует `./Markdown.svelte`, `./MediaPlayer.svelte`, `./Report.svelte`, `./Icon.svelte`, `./Modal.svelte`, `../voice.js`, `../prepare-video.js`, `../file-size.js`, `../agents.js`.
- `web/src/components/MessageList.svelte` — сканирован; импортирует `./MessageItem.svelte`, `./Icon.svelte`, `svelte`.
- `web/src/components/Modal.svelte` — сканирован; импортирует `svelte/transition`, `svelte`, `./Icon.svelte`.
- `web/src/components/ModelPicker.svelte` — сканирован; импортирует `./Icon.svelte`, `../agents.js`.
- `web/src/components/Notes.svelte` — сканирован; импортирует `svelte`, `./Icon.svelte`.
- `web/src/components/ProfileScreen.svelte` — сканирован; импортирует `../browser-notifications.js`, `@capacitor/core`, `./Icon.svelte`, `./AgentView.svelte`, `./SkillsScreen.svelte`, `./InstructionsEditor.svelte`, `./MemoryEditor.svelte`, `./SpecialistModel.svelte`, `./McpServers.svelte`, `../agents.js`.
- `web/src/components/Report.svelte` — сканирован; импортирует `./Markdown.svelte`, `svelte`.
- `web/src/components/SessionDrawer.svelte` — сканирован; импортирует `svelte/transition`, `./Icon.svelte`, `@capacitor/core`, `../agents.js`.
- `web/src/components/SettingsPanel.svelte` — сканирован; импортирует `./Icon.svelte`, `./ModelPicker.svelte`, `../agents.js`.
- `web/src/components/SkillsScreen.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/SpeakingPill.svelte` — сканирован; импортирует нет найденных импортов.
- `web/src/components/SpecialistModel.svelte` — сканирован; импортирует `./Icon.svelte`, `./ModelPicker.svelte`.
- `web/src/components/TopBar.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/VideoRecorderOverlay.svelte` — сканирован; импортирует `./Icon.svelte`.
- `web/src/components/VoiceMode.svelte` — сканирован; импортирует `svelte`, `./Icon.svelte`, `../voice.js`, `../voice-vad.js`, `../voice-turn.js`.

## Кандидаты на особые случаи: машинный поиск

Здесь перечислены совпадения в заданных файлах, включая тесты и строки комментариев. Совпадение не означает проблему: нужно проверить контекст и для каждого применимого случая записать решение «оставить» или «убрать». Поиск построчный и не гарантирует обнаружение многострочных условий и всех форм отключённых тестов.

### catch (136 строк)

- `server/camera.ts:36`
- `server/codex.ts:91`
- `server/codex.ts:93`
- `server/commands.ts:53`
- `server/external-mcp.ts:16`
- `server/external-mcp.ts:25`
- `server/external-mcp.ts:52`
- `server/files.ts:41`
- `server/files.ts:47`
- `server/files.ts:61`
- `server/files.ts:71`
- `server/harness.ts:46`
- `server/harness.ts:52`
- `server/index.ts:69`
- `server/index.ts:148`
- `server/index.ts:171`
- `server/index.ts:222`
- `server/index.ts:224`
- `server/index.ts:235`
- `server/index.ts:237`
- `server/mcp.ts:23`
- `server/mcp.ts:114`
- `server/mcp.ts:181`
- `server/mcp.ts:184`
- `server/mcp.ts:249`
- `server/mcp.ts:260`
- `server/memory.ts:15`
- `server/memory.ts:21`
- `server/memory.ts:28`
- `server/memory.ts:40`
- `server/models.ts:72`
- `server/observability.ts:19`
- `server/observability.ts:23`
- `server/preview.test.ts:27`
- `server/preview.ts:44`
- `server/preview.ts:55`
- `server/preview.ts:63`
- `server/preview.ts:75`
- `server/preview.ts:92`
- `server/preview.ts:96`
- `server/preview.ts:97`
- `server/secrets.ts:13`
- `server/secrets.ts:19`
- `server/secrets.ts:29`
- `server/secrets.ts:36`
- `server/session.ts:136`
- `server/session.ts:214`
- `server/session.ts:236`
- `server/session.ts:242`
- `server/session.ts:245`
- `server/smart-turn.ts:24`
- `server/smart-turn.ts:29`
- `server/smart-turn.ts:43`
- `server/smart-turn.ts:45`
- `server/speech.ts:40`
- `server/speech.ts:62`
- `server/speech.ts:102`
- `server/sudo.ts:51`
- `server/update.ts:56`
- `server/update.ts:70`
- `server/voice.test.ts:23`
- `web/src/browser-notifications.js:4`
- `web/src/browser-notifications.js:14`
- `web/src/connection.test.js:14`
- `web/src/connection.test.js:15`
- `web/src/connection.test.js:16`
- `web/src/prepare-image-worker.js:19`
- `web/src/prepare-image.js:5`
- `web/src/prepare-video.js:50`
- `web/src/updates.js:12`
- `web/src/voice-turn.js:6`
- `web/src/App.svelte:30`
- `web/src/App.svelte:61`
- `web/src/App.svelte:66`
- `web/src/App.svelte:73`
- `web/src/App.svelte:78`
- `web/src/App.svelte:82`
- `web/src/App.svelte:87`
- `web/src/App.svelte:92`
- `web/src/App.svelte:98`
- `web/src/App.svelte:117`
- `web/src/App.svelte:146`
- `web/src/App.svelte:159`
- `web/src/App.svelte:172`
- `web/src/App.svelte:188`
- `web/src/App.svelte:210`
- `web/src/App.svelte:214`
- `web/src/App.svelte:215`
- `web/src/App.svelte:222`
- `web/src/App.svelte:248`
- `web/src/App.svelte:280`
- `web/src/App.svelte:311`
- `web/src/App.svelte:324`
- `web/src/App.svelte:337`
- `web/src/App.svelte:345`
- `web/src/App.svelte:380`
- `web/src/components/AgentView.svelte:10`
- `web/src/components/Composer.svelte:77`
- `web/src/components/Composer.svelte:87`
- `web/src/components/Composer.svelte:136`
- `web/src/components/Composer.svelte:143`
- `web/src/components/Composer.svelte:167`
- `web/src/components/DownloadsScreen.svelte:17`
- `web/src/components/DownloadsScreen.svelte:25`
- `web/src/components/InstructionsEditor.svelte:16`
- `web/src/components/InstructionsEditor.svelte:25`
- `web/src/components/Markdown.svelte:13`
- `web/src/components/McpServers.svelte:8`
- `web/src/components/McpServers.svelte:14`
- `web/src/components/McpServers.svelte:30`
- `web/src/components/MediaPlayer.svelte:18`
- `web/src/components/MediaPlayer.svelte:25`
- `web/src/components/MediaPlayer.svelte:41`
- `web/src/components/MediaPlayer.svelte:48`
- `web/src/components/MediaPlayer.svelte:57`
- `web/src/components/MediaPlayer.svelte:64`
- `web/src/components/MemoryEditor.svelte:13`
- `web/src/components/MemoryEditor.svelte:21`
- `web/src/components/MemoryEditor.svelte:30`
- `web/src/components/MemoryEditor.svelte:40`
- `web/src/components/MessageItem.svelte:21`
- `web/src/components/MessageItem.svelte:27`
- `web/src/components/MessageItem.svelte:55`
- `web/src/components/ModelPicker.svelte:28`
- `web/src/components/Notes.svelte:21`
- `web/src/components/Notes.svelte:34`
- `web/src/components/Notes.svelte:40`
- `web/src/components/ProfileScreen.svelte:45`
- `web/src/components/SkillsScreen.svelte:12`
- `web/src/components/SkillsScreen.svelte:24`
- `web/src/components/SpecialistModel.svelte:14`
- `web/src/components/VoiceMode.svelte:32`
- `web/src/components/VoiceMode.svelte:48`
- `web/src/components/VoiceMode.svelte:59`
- `web/src/components/VoiceMode.svelte:96`
- `web/src/components/VoiceMode.svelte:149`

### таймер или повтор (39 строк)

- `server/camera.ts:7`
- `server/camera.ts:22`
- `server/files.test.ts:53`
- `server/harness.test.ts:42`
- `server/harness.test.ts:82`
- `server/harness.ts:5`
- `server/harness.ts:49`
- `server/harness.ts:55`
- `server/index.ts:159`
- `server/mcp.test.ts:247`
- `server/preview.ts:97`
- `server/secrets.test.ts:13`
- `server/secrets.ts:45`
- `server/session.test.ts:35`
- `server/session.ts:239`
- `server/smart-turn.ts:39`
- `server/speech.test.ts:17`
- `server/speech.test.ts:21`
- `server/speech.test.ts:28`
- `server/sudo.ts:24`
- `server/sudo.ts:36`
- `server/update.ts:47`
- `web/src/components/sleep-timer.test.js:2`
- `web/src/connection.js:1`
- `web/src/interrupt-feedback.js:1`
- `web/src/App.svelte:180`
- `web/src/App.svelte:397`
- `web/src/components/CodexLimits.svelte:39`
- `web/src/components/Composer.svelte:47`
- `web/src/components/Composer.svelte:50`
- `web/src/components/Composer.svelte:56`
- `web/src/components/DownloadsScreen.svelte:16`
- `web/src/components/Markdown.svelte:38`
- `web/src/components/MediaPlayer.svelte:6`
- `web/src/components/MediaPlayer.svelte:59`
- `web/src/components/MediaPlayer.svelte:60`
- `web/src/components/Notes.svelte:36`
- `web/src/components/VoiceMode.svelte:86`
- `web/src/components/VoiceMode.svelte:100`

### сравнение в if (154 строк)

- `server/chat.ts:31`
- `server/chat.ts:46`
- `server/chat.ts:56`
- `server/chat.ts:76`
- `server/chat.ts:77`
- `server/chat.ts:85`
- `server/chat.ts:86`
- `server/chat.ts:87`
- `server/chat.ts:88`
- `server/chat.ts:89`
- `server/chat.ts:90`
- `server/chat.ts:91`
- `server/chat.ts:93`
- `server/chat.ts:94`
- `server/chat.ts:95`
- `server/chat.ts:105`
- `server/chat.ts:106`
- `server/chat.ts:111`
- `server/chat.ts:113`
- `server/chat.ts:114`
- `server/chat.ts:120`
- `server/chat.ts:121`
- `server/chat.ts:126`
- `server/chat.ts:127`
- `server/chat.ts:128`
- `server/chat.ts:133`
- `server/chat.ts:139`
- `server/chat.ts:140`
- `server/claude.ts:44`
- `server/codex.ts:28`
- `server/codex.ts:59`
- `server/commands.ts:25`
- `server/commands.ts:34`
- `server/commands.ts:35`
- `server/external-mcp.ts:16`
- `server/external-mcp.ts:44`
- `server/external-mcp.ts:50`
- `server/external-mcp.ts:51`
- `server/harness.test.ts:104`
- `server/harness.ts:44`
- `server/index.ts:50`
- `server/index.ts:51`
- `server/index.ts:52`
- `server/index.ts:53`
- `server/index.ts:57`
- `server/index.ts:165`
- `server/index.ts:174`
- `server/index.ts:177`
- `server/index.ts:178`
- `server/index.ts:181`
- `server/index.ts:183`
- `server/index.ts:188`
- `server/index.ts:193`
- `server/index.ts:195`
- `server/index.ts:197`
- `server/index.ts:201`
- `server/index.ts:202`
- `server/index.ts:203`
- `server/index.ts:206`
- `server/index.ts:207`
- `server/index.ts:208`
- `server/index.ts:211`
- `server/index.ts:212`
- `server/index.ts:213`
- `server/index.ts:216`
- `server/index.ts:219`
- `server/index.ts:221`
- `server/index.ts:222`
- `server/index.ts:226`
- `server/index.ts:229`
- `server/index.ts:230`
- `server/index.ts:232`
- `server/index.ts:242`
- `server/index.ts:243`
- `server/index.ts:246`
- `server/index.ts:247`
- `server/index.ts:248`
- `server/index.ts:254`
- `server/mcp.test.ts:35`
- `server/mcp.ts:256`
- `server/mcp.ts:257`
- `server/mcp.ts:258`
- `server/memory.ts:12`
- `server/memory.ts:21`
- `server/observability.ts:22`
- `server/observability.ts:23`
- `server/observability.ts:46`
- `server/observability.ts:53`
- `server/preview.ts:14`
- `server/secrets.ts:19`
- `server/session.test.ts:27`
- `server/sessions.ts:48`
- `server/sessions.ts:127`
- `server/speech.test.ts:10`
- `server/speech.ts:47`
- `server/speech.ts:73`
- `web/src/activity-status.js:4`
- `web/src/activity-status.js:5`
- `web/src/activity-status.js:6`
- `web/src/activity-status.js:8`
- `web/src/agents.js:23`
- `web/src/browser-notifications.js:11`
- `web/src/connection.js:20`
- `web/src/main.js:5`
- `web/src/markdown.js:18`
- `web/src/speech.js:7`
- `web/src/App.svelte:213`
- `web/src/App.svelte:218`
- `web/src/App.svelte:224`
- `web/src/App.svelte:225`
- `web/src/App.svelte:226`
- `web/src/App.svelte:227`
- `web/src/App.svelte:228`
- `web/src/App.svelte:233`
- `web/src/App.svelte:234`
- `web/src/App.svelte:235`
- `web/src/App.svelte:236`
- `web/src/App.svelte:237`
- `web/src/App.svelte:238`
- `web/src/App.svelte:239`
- `web/src/App.svelte:240`
- `web/src/App.svelte:244`
- `web/src/App.svelte:246`
- `web/src/App.svelte:248`
- `web/src/App.svelte:298`
- `web/src/App.svelte:354`
- `web/src/App.svelte:361`
- `web/src/App.svelte:362`
- `web/src/App.svelte:366`
- `web/src/App.svelte:396`
- `web/src/App.svelte:397`
- `web/src/components/AttachmentPopover.svelte:14`
- `web/src/components/Composer.svelte:47`
- `web/src/components/Composer.svelte:55`
- `web/src/components/Composer.svelte:65`
- `web/src/components/Composer.svelte:71`
- `web/src/components/Composer.svelte:82`
- `web/src/components/Composer.svelte:98`
- `web/src/components/Composer.svelte:147`
- `web/src/components/Composer.svelte:163`
- `web/src/components/Composer.svelte:224`
- `web/src/components/Composer.svelte:225`
- `web/src/components/Composer.svelte:226`
- `web/src/components/Composer.svelte:228`
- `web/src/components/Composer.svelte:306`
- `web/src/components/DownloadsScreen.svelte:16`
- `web/src/components/McpServers.svelte:28`
- `web/src/components/MediaPlayer.svelte:36`
- `web/src/components/MessageItem.svelte:255`
- `web/src/components/Modal.svelte:14`
- `web/src/components/Notes.svelte:42`
- `web/src/components/SessionDrawer.svelte:25`
- `web/src/components/SessionDrawer.svelte:67`
- `web/src/components/SkillsScreen.svelte:19`

### отключённый тест (0 строк)
