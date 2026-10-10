# Проверка раннего ответа Secretary

Status: verified

## Исходная причина

Legacy runtime сохранял обычный ответ Secretary только после terminal. Telegram дополнительно удерживал такой ответ до завершения хода. Старый профиль явно требовал после Dispatch перечислить Worker и Node. Адаптер Telegram сам добавлял заголовок «Задача от Secretary:» к prompt.

## Решение и границы

Отдельное раннее подтверждение сохраняется через canonical conversation с identity текущего input. Оно не является финальным addressed reply и не удовлетворяет terminal completion guard. После успешного делегирования точное повторение подтверждения не должно создавать второе сообщение. Отличающийся финальный ответ, в том числе ошибка делегирования, остаётся видимым.

Подтверждение сохраняется до рабочего tool. Telegram доставляет его через упорядоченный outbox, не ожидая terminal. Момент фактической доставки зависит от Telegram и сети; синхронное подтверждение получения Telegram до запуска backend tool не является контрактом.

Общие правила находятся в external Worker profile: минимальный полезный результат и необходимый источник, краткая блокировка, запрет выдумывать факты. Task prompt сохраняет суть запроса и существенные ограничения пользователя.

## Проверки

Первый код `c2be91f`: полный `go test -p 1 ./...` прошёл, race для core/MCP/secretaryd прошёл. Immutable build всех пяти binaries для Linux amd64 и Darwin arm64 прошёл; Web 19/19 tests и оба bundles прошли.

Первый независимый Spec review: 0 findings по реализации, живая модель ещё ожидает проверки. Standards review: два findings, которые исправляются до deployment:

- P1: crash после удаления отправленного acknowledgement из outbox, но до сохранения event cursor мог вызвать повтор отправки. Нужна durable отметка доставки по send identity.
- P2: проверка наличия acknowledgement перед stream delta была вне транзакции сохранения delta. Нужна атомарная проверка и запись, чтобы поздний delta не повторял сообщение.

Production этим первым candidate не обновлялся. Исправления находятся в `6b8f826`: оба новых regression подтвердили RED на `c2be91f` и GREEN после исправления. Полный повтор `go test -p 1 ./...` прошёл, focused race с тремя повторениями прошёл. Один прогон поймал неизменённый OpenCode fixture timeout (1s); отдельный повтор и последующий полный прогон прошли, посторонний timeout не менялся.

Повторные независимые reviews полного диапазона `cff9725...6b8f826`: Standards 0 findings, Spec 0 findings. P1 и P2 закрыты. Живой native Codex на финальном build ещё проверяется.

## Production

До обновления: source `af1290a`, main `cff9725`; оба Nodes online, активных Secretary turns/Worker Attempts нет, Node outbox пуст, SQLite `quick_check=ok`, 39 Telegram topics. Погодный Worker пользователя сохранён.

Custom profiles, которые необходимо обновить явно:

- `profiles/secretary-codex-phase5.md`
- `profiles/worker-template-luna6-low-9e3-v1.md`

Mac Node получает compiled Worker profile от сервера.

Развёрнут immutable candidate `response-style-6b8f826-20261010T151452Z`. Manifest: `/Users/beruseruko/.local/share/secretary/response-style-builds/release-6b8f826-20261010T151359Z/manifest.json`, source SHA256 `9e82a0ef00cb5047372cd4612e632ba6dd3e0657425e1b99388cedc8f2b64de2`. Все десять binaries проверены. Backup omarchy: `/home/coder/.local/share/secretary/backups/response-style-20261010T151501Z`; Mac: `/Users/beruseruko/.local/share/secretary/codex-remote-node/backups/response-style-20261010T151459Z`. После cutover оба Nodes online, Node outbox0, health/quick_check ok; digests всех 42 Worker bindings/profile snapshots/statuses и 39 topics совпали.

## Живая проверка Telegram

Запрос «Какая сейчас погода в Барнауле? Ответь одним коротким абзацем.» отправлен через обычный Telegram General. Turn `stn_0b8fa6cf9f89041b8547a0c1f9ae676a` succeeded. Модель сама первой вызвала `acknowledge_user`. Canonical «Сейчас проверю.» записано в `15:16:25.226Z`, Telegram delivery checkpoint в `15:16:25.709Z`, затем `list_nodes` в `15:16:27.887Z` и `message_worker` в `15:16:34.787Z`. AXI подтвердил видимое короткое сообщение до делегирования; canonical Secretary entry ровно одна, terminal завершился в `15:16:39.320Z` без повторного сообщения.

Secretary продолжил существующий погодный Worker пользователя. Его прежние snapshot и длинный topic title сохранены. Result пришёл один раз; Worker сообщил, что более свежие данные подтвердить не удалось. Достоверность текущей погоды этим probe не заявляется.

Live выявил лишние собственные требования в Follow-up: источник и время данных, которые пользователь не просил. Уточнение только Secretary profile проверено отдельно двумя reviewers (0 findings), config test прошёл; source `95af546`, cherry-pick на main `494f2a7`.

Первый Web probe показал, что буквальная передача всего запроса переносит в Worker prompt ещё и адресованные Secretary команды создать Worker и выбрать Node/Project. Последнее уточнение отделяет эти указания от основной задачи: source `07e41c0`, cherry-pick на main `4cba003`. Отдельные Standards/Spec проверки этой правки: 0 findings. Создание дочернего Worker не было доказано: `Worker.id` и `Worker.worker_ref` первого probe относятся к одному Worker, а не к двум.

В production установлен внешний Secretary profile из `4cba003`, SHA256 `ca3fd984e770a87322238ed57b434a4a13210b4b2817ec160d2602d85ae08d78`. Binaries остаются проверенным `6b8f826`; Worker profile остаётся 610 bytes, SHA256 `3d6606309deac3263f5da69b60b480392cb6f54a89c1caa6bc0c749b4ed4b8de`. Изменения после `6b8f826` затрагивают только текст внешнего Secretary profile. Backup до применения последнего профиля: `/home/coder/.local/share/secretary/backups/response-style-profile-4cba003-20261010T152521Z`. Перезапускался только Secretaryd; оба Nodes не прерывались.

## Финальная живая проверка Web и Telegram

Обычный Web composer отправил запрос создать отдельного Worker на omarchy в Project `codex-mvp-20261010` для чтения отсутствующего файла. Turn `stn_5d9276d8c6d9f4f83ee74a49eb5cf627` succeeded. Native модель первой вызвала `acknowledge_user`; canonical entry `ent_a949877810450cc4fbce2ad98f59b863` в `15:27:05.314Z`, до `list_nodes` в `15:27:05.326Z`. Telegram checkpoint в `15:27:05.824Z`, до Dispatch. Физическая доставка по сети не объявляется синхронной гарантией до любого inventory tool. Web и Telegram показали одно «Сейчас проверю.», после terminal в `15:27:18.491Z` второго подтверждения нет.

Модель сама создала `wrk_f2480571c3e7be2b3b351faaacaf34c6`, передав только «Прочитай файл /tmp/secretary-style-final-missing-20261010.txt и кратко сообщи, что там.». Lifecycle/placement указания остались в `preferences`, дополнительные процедуры и требования не появились. Его immutable profile snapshot содержит точно новый Worker profile 610 bytes с указанным SHA256.

Attempt `att_68ef10d4f07616524b8455544281edbe` succeeded. Result `res_1f8673dd45fb25f1b84be63411318140` кратко сообщил, что файл отсутствует и прочитать его не удалось. Содержимое не выдумано. Canonical Result entry ровно одна. AXI подтвердил исходный prompt в topic 390 без «Задача от Secretary:», затем один Result. General показывает один Result с короткой ссылкой на topic.

Два собственных probe Workers `wrk_e6f2b39448bd51849a5f88a0d80f8d44` и `wrk_f2480571c3e7be2b3b351faaacaf34c6` закрыты штатным owner API после проверки idle и отсутствия queue. Их snapshots, Results и topics сохранены. Погодный Worker пользователя не закрывался.

## Standards

Финальный runtime diff и обе последующие правки профиля проверены независимо. 0 findings; P1 и P2 устранены, completion guards сохранены, запрещённых комментариев и существенных признаков лишней сложности нет.

## Spec

Финальный runtime diff и обе последующие правки профиля проверены независимо. 0 findings. Живая native проверка подтвердила раннее подтверждение, простой task prompt, новый Worker profile, краткую блокировку и Telegram без добавляемого заголовка.

## Перезапуск и итоговое состояние

После завершения собственных probes выполнен controlled restart только `secretaryd.service`, backup `/home/coder/.local/share/secretary/backups/response-style-replay-20261010T153147Z`. До и после совпали digests всех 44 Workers (35 closed, 7 idle, 2 offline), 62 Attempts и 62 Results, 41 Telegram topic. Три собственных canonical acknowledgement и два canonical Results не изменились; новых запусков нет. Telegram delivery checkpoints и processed updates совпали сразу после restart и при дополнительной проверке. AXI снова показал прежние единственные сообщения final probe, без дублей.

Пять собственных записей общего Core conversation delivery ledger имели `pending` до и после restart. Это не выдаётся за Core delivery receipt; фактическая доставка подтверждена Telegram checkpoint и Web/Telegram UI. Node outbox пуст на обоих Nodes.

Финальный Secretaryd PID `2681844`; omarchy Node `2674151`, Mac Node `21496`. Оба Nodes online capacity1, активной работы нет, HTTPS health и SQLite `quick_check` ok. Config и profiles сохранили byte hashes, native auth/identity не менялись, погодный Worker пользователя idle и не закрыт.

Пользовательские dirty `spec/THE spec.md` и `spec/memory.md` не включены в commits и сохранили SHA256 `4a8b8d28de155b146953cc3477a7facc8b207535d6d0f93c66fda2571c3a525f` и `afca7e1eadaa5e4ad57a30d628e9af0ac4f1edabcd367d464b0629904c5d1267`.
