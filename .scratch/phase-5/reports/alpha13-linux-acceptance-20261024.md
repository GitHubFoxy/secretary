# Secretary alpha.13: приёмка на omarchy

Дата: 24 октября 2026 года, Asia/Krasnoyarsk. Машинные отметки времени в evidence используют UTC, 23 октября.

Основной сценарий Codex и Claude Code Haiku 4.5 работает на Linux: Secretary отвечает, создаёт Worker, принимает Follow-up и публикует Results. Оба Worker сохранили native session и workspace после рестарта Server и Execution Node. Полная приёмка Linux остаётся незавершённой: восстановление Codex после ошибки команды не прошло, официальный установщик и Telegram в этом прогоне не проверялись. Код продукта не менялся.

## Кандидат и среда

| Поле | Фактическое значение |
| --- | --- |
| Релиз | [v0.1.0-alpha.13](https://github.com/GitHubFoxy/secretary/releases/tag/v0.1.0-alpha.13), prerelease |
| Revision | `90578a399e773a41f65ec4a715486949b7ea9798` |
| Архив | `secretary-v0.1.0-alpha.13-linux-amd64.tar.gz`, 147141946 bytes |
| SHA256 | `9f51c0e8e38bf8a21dadf7cf5e860cfdcdffe293793ef15f60a66b28de164529` |
| ОС | omarchy, Arch Linux x86_64, kernel `7.2.3-arch1-3`, glibc 2.44 |
| Пользователь | `coder`, UID 1000 |
| Codex | Managed CLI 0.163.0, ACP 2.2.2, модель `gpt-6.1-sol`, reasoning `low` |
| Claude Code | CLI 2.1.270, модель `claude-haiku-4-5-20251001`, API key auth |
| Claude API | Существующий `ANTHROPIC_BASE_URL=https://4router.net`, без изменения |
| Тестовый адрес | `127.0.0.1:18081` |
| Тестовое состояние | `/home/coder/.local/state/secretary-alpha13-linux-acceptance` |

Архив скачан из опубликованного GitHub release. Manifest, размер, SHA256 и встроенная revision совпали. После тестов каждый файл payload повторно сравнен с архивом: изменений нет. Локальная сборка и ручная установка npm-зависимостей не использовались.

На omarchy уже работал Secretary alpha.8 на порту 8081. Его units, текущий release и процессы сохранены. Alpha.13 запускался из опубликованного payload в отдельных временных user units, с отдельными HOME, данными, Node identity и портом. Существующая авторизация Codex и Claude подключалась к тестовой среде. Такое разделение состояния не ограничивает доступ Worker к файлам ОС.

## Результаты проверок

`PASS` означает фактически выполненную проверку. `FAIL` означает наблюдаемое несоответствие ожидаемому поведению. `BLOCKED` означает внешний предел этого прогона. `NOT RUN` означает отсутствие проверки. Зелёный CI и review не учитываются как live PASS.

| Проверка | Статус | Наблюдение |
| --- | --- | --- |
| Опубликованный Linux payload | PASS | Hash, manifest, revision и неизменность файлов подтверждены |
| Codex non-interactive setup | PASS | Пара Codex и ACP подготовлена самим опубликованным setup |
| Claude non-interactive setup | PASS | Существующая native auth использована |
| Interactive setup | PASS | Настоящий SSH PTY, обнаружены оба CLI, выбран `claude_code`, setup завершился |
| `secretary config` | PASS | Меню, Paths and operations, выход через Done; изменение параметров через меню не проверено |
| `doctor`, `node doctor` | PASS | Проверки готовности обоих выбранных harness завершились успешно |
| Node pairing, heartbeat, inventory | PASS | Отдельный Execution Node online; Worker выполнялись в процессе Node |
| Два последовательных ответа Codex | PASS | С явно выбранной поддерживаемой моделью, сохранён маркер `СОСНА-4927` |
| Codex с исходным `model=default` | FAIL | Native config выбирает неподдерживаемый для ChatGPT account `gpt-5.5`; подробности ниже |
| `spawn_worker` Codex | PASS | Создан ровно один projectless Worker, получен terminal Result |
| Вопрос в первом Codex Result | FAIL | Ответ утверждает, что вопрос задан, но не содержит самого вопроса |
| Follow-up через `message_worker` | PASS | Тот же Worker, session и workspace; следующий Result содержит вопрос |
| Codex exit 7 | PASS | Реальная команда завершилась exit 7, один Result `failed/tool_execution_failed` |
| Codex explicit recovery | FAIL | Exit 7 и затем разрешённая команда exit 0 выполнены, но Result остался failed |
| JSON с текстом `failed` как данные | PASS | Процитированный JSON не превратил успешную задачу в ошибку |
| Cancel Codex | PASS | Реальный `sleep 45` и наблюдаемый native процесс исчезли за 0.42 s; один canceled Result |
| Claude Haiku 5.5 | BLOCKED | Провайдер не содержит эту модель; пользователь затем выбрал Haiku 4.5 |
| Два последовательных ответа Claude 4.5 | PASS | Чистый пользовательский текст, сохранён маркер `БЕРЁЗА-2761` |
| Claude inventory и Worker model pin | PASS | Inventory содержит Haiku 4.5; реальные Secretary и Worker argv используют точный model ID |
| Claude Worker и документ | PASS | Ровно один Worker прочитал TXT, назвал маркер, сумму и вопрос |
| Claude exit 7 | PASS | Один Result `failed/tool_execution_failed`, без ложного успеха |
| Cancel Claude | PASS | Наблюдаемые потомки исчезли за 0.07 s; один canceled Result |
| Direct execution обоих Worker | PASS | UID 1000, тестовая ENV и тестовый файл вне workspace доступны |
| Рестарт Server и Node | PASS | Оба native session ID и workspace сохранены; Follow-up вспоминает прежние данные |
| Дедупликация | PASS | 14 Attempts и 14 Results; каждый Result опубликован ровно один раз |
| Повтор запроса с тем же idempotency key | PASS | HTTP 202, новая Attempt не появилась |
| Node offline и reconnect | PASS | Запрос offline отклонён HTTP 400; 13 Attempts и 13 Results не изменились после reconnect |
| TXT upload, download, чтение Worker | PASS | 57 bytes, SHA256 совпал; Worker вернул `ЛИНУКС-ФАЙЛ-8426` и 730 рублей |
| Неверные вложения | PASS | Пустой TXT: 400, EXE: 415, повреждённый PNG: 422 |
| API без owner session | PASS | Conversation, Workers, Nodes и отправка сообщения отклонены HTTP 401 |
| Web в настоящем браузере | PASS | Bootstrap, четыре шага onboarding, сообщение, reload с сохранением входа, Observer Follow-up |
| Повторный setup | PASS | Config, environment, Node config, identity и native mappings не изменились |
| Native CLI без авторизации | PASS | Отрицательные проверки диагностируют отсутствие auth; успешный login не заявляется |
| Claude CLI не установлен | PASS | Setup отклоняет отсутствующий CLI и сохраняет предыдущий config |
| CLI revision работающего Server | FAIL | `unknown`, хотя `/proc/PID/exe` доступен и указывает на опубликованный alpha.13 binary |

Итоговые terminal статусы: 9 succeeded, 3 failed и 2 canceled. Ошибочные и отменённые задачи здесь намеренно присутствуют в приёмке; каждое исполнение имеет один Result. Активных Attempts после проверки нет.

## Наблюдаемые ошибки и точные границы выводов

### Codex: разрешённое восстановление остаётся failed

Worker `wrk_6246f8764d0cfc0d8a07857f42027043`, Attempt `att_6ac92ae0b70b9667226fb6193ac7ab51`, Result `res_7382789c2fe125b24a93b74d7f98b13b`.

В одной задаче владелец явно разрешил восстановление. Native события подтверждают последовательность:

1. `/bin/sh -c "printf LINUX-RECOVERY-FAIL-7 >&2; exit 7"`, command ID `exec-29f92456-06ee-4753-bd45-da49f46d9f85`, exit 7.
2. `/bin/sh -c "printf LINUX-RECOVERY-OK"`, command ID `exec-3b0c5cdf-ff35-4d3a-b94f-edbabadf9c17`, exit 0.

Несмотря на вторую команду, canonical Result: `failed/tool_execution_failed`. Native final объясняет, что модель не может назвать обязательный `native toolCallId` первой команды для подтверждения восстановления. Это live FAIL сценария explicit recovery. Причина в исходном коде не установлена; отчёт не приписывает её без проверки ACP, профилю или модели. Повтором до зелёного результата ошибка не маскировалась.

### Codex: исходная модель и потеря причины ошибки в ответе

Существующий глобальный `~/.codex/config.toml` задаёт `gpt-5.5`. При тестовом `model=default` две реплики Secretary и запрос создания Worker завершились `native_terminal_invalid`. Native transcript содержит: `The 'gpt-5.5' model is not supported when using Codex with a ChatGPT account.`

Владелец получил общее сообщение об отсутствии подтверждённого адресованного ответа, которое не объясняет неподдерживаемую модель. После выбора `gpt-6.1-sol` только в тестовом Secretary основные сценарии прошли. Глобальная конфигурация Codex не менялась. Это ограничение исходного окружения вместе с проблемой диагностики, а не доказательство общей неисправности Codex runtime.

### Codex: первый вопрос отсутствует и в native final

Result `res_8586f460c396da993df1f9a33c2b7919` содержит: `Название: «ЕЛЬ-6812 · Идеи». Вопрос о стиле задан.` Самого вопроса нет. Native final совпадает, поэтому признаков удаления вопроса сериализацией Result нет. Follow-up с явным требованием включить вопрос вернул `Добавить эмодзи?` в той же session. Первоначальный критерий остаётся FAIL.

### Диагностика Server и сообщения интерфейса

CLI вывел `running Server: pid=653593 unknown (process executable path unavailable)`. Проверка `/proc/653593/exe` при работающем процессе указывала на опубликованный `secretaryd`. Node показывал правильную revision. Сценарий использует отдельный HOME и прямой запуск binary; поведение стандартной установленной службы этим наблюдением не доказано.

Явный `spawn_worker` Claude с `reasoning=default` был отклонён как неподдерживаемый. Worker не создавался. Однако в conversation осталась предварительная реплика `Сейчас запущу Worker`; наличие понятного окончательного объяснения в Web не подтверждено. Следующий корректный запрос без reasoning создал одного Worker. Отдельно Web показывает projectless Worker как `Unknown Project`.

## Сессии, файлы и модель

| Harness | WorkerRef | Native session | Workspace basename |
| --- | --- | --- | --- |
| Codex | `wrk_6246f8764d0cfc0d8a07857f42027043` | `01a169b4-4645-76b1-b316-39d6172c2f69` | `secretary-worker-3678308076` |
| Claude | `wrk_dc2cd344a4d8fcd8bf3353e829f4c894` | `5521c9fa-1c6b-4bdd-998c-58202dea45bb` | `secretary-worker-228083316` |

Session и workspace каждого Worker совпадают до и после рестарта. Последний Follow-up отправлен через реальный Web Observer. Claude вернул `WEB-WORKER-6842`, маркер документа и сумму из прежней сессии.

TXT artifact: `art_722c244e3a2dde64f51c29d9ceed6fdb`, SHA256 `8601112b4d3c1adfef16f798bbd7a50944f9c29d1f603426d4f0403bdfbdb0a3`.

Haiku 5.5 отсутствует в inventory настроенного провайдера. Прямой пробный запуск не завершился и был остановлен. После команды пользователя «change to 4.5» тестовые defaults переключены на `claude-haiku-4-5-20251001`. Другие модели не подставлялись. Реальные argv обоих Claude процессов и native session подтверждают Haiku 4.5. Запись `<synthetic>` в native истории не является дополнительной выбранной моделью.

## Что этот прогон не подтверждает

| Сценарий | Статус | Причина |
| --- | --- | --- |
| Полная чистая установка официальной командой | BLOCKED | Установщик управляет действующими user units alpha.8; их сохранение ограничило этот прогон |
| `start/stop/logs` стандартных установленных служб, login start, reboot | NOT RUN | Применялись отдельные временные test units |
| Изменение и сохранение параметра через TUI config | NOT RUN | Проверены просмотр меню и штатный выход |
| Telegram DM, Worker Result и Follow-up | NOT RUN | В Linux-прогон не включены |
| Все типы файлов, лимит 8 MiB, видео | NOT RUN | Проверены один TXT и три неверных вложения |
| Полный путь отсутствующего Codex CLI и интерактивная авторизация | NOT RUN | Проверены готовая auth и отрицательные auth probes |
| Документация о direct execution | NOT RUN | Проверено фактическое исполнение, без аудита документации |
| Mac alpha.13 и Windows | NOT RUN | Mac отложен пользователем; Windows в этот прогон не входит |

Codex setup без auth завершился exit 0, сообщил, что владельцу нужен native login; последующий doctor вернул exit 1. Это не PASS готовности к исполнению. Claude setup без auth вернул exit 1. Пустые тестовые HOME не содержали авторизацию владельца.

Ошибки организации теста отделены от продукта: первый Node был запущен до готовности Server; ручное переключение harness первоначально оставило неподходящий Codex reply contract; временный systemd unit был собран после stop и создан заново для reconnect. После исправления только тестовой конфигурации выполнен штатный Claude setup. Эти эпизоды не засчитаны как неисправности установщика или persistent units. URL порта 8081 в CLI также не засчитан как дефект: порт 18081 задан прямым тестовым запуском Server.

## Завершение и evidence

Тестовые Server и Node остановлены, активных Attempts нет. Временный браузер и SSH tunnel закрыты. Native auth и перечисленные provider config файлы сравнивались до и после тестов: содержимое сохранено; обе native auth проверки успешны. Постоянный release timer ранее удалён и не создавался повторно.

Действующий alpha.8 остался на тех же PID: Server `3409627`, Node `3409630`. Оба systemd unit active, executable paths по-прежнему относятся к alpha.8, health HTTP 200 `{"status":"ok"}`. В системе остались только его процессы: один `secretaryd`, один `secretary-node`, один `secretary-mcp`.

[Отобранные evidence](alpha13-linux-evidence.json) содержат canonical Results, native command ID и exit codes, отмену процессов, модели, continuity, Web и финальную проверку среды. Секреты, cookies, bootstrap token, полные environment/config файлы и чужие native sessions исключены.

Вердикт: ограниченный сценарий Codex и Claude Haiku 4.5 подтверждён на опубликованном Linux payload. Заявлять полный PASS alpha.13 или готовность Public v1 по этому прогону нельзя. Следующая работа по найденным ошибкам требует реализации и отдельной повторной приёмки; этот отчёт ничего не исправляет.
