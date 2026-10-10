# Приёмка Codex MVP через Web и Telegram — 2026-10-10

Итог: **Codex MVP PASS**. [Краткая финальная матрица](codex-mvp-final-20261010.md). Разделы ниже сохраняют хронологию первоначальных FAIL, исправлений и новых живых проверок; прежние указания «ещё выполняется» относятся к тому этапу. Claude Code отложен, его поддержка не объявлена проверенной.

Текущая приёмка по явному решению пользователя проверяет Codex; Claude Code отложен, его поддержка не объявляется доказанной. Проверки выполняются через AXI в существующем авторизованном Career Helium, Telegram tab 1 и собственный production Web tab 2 (восстановленная AXI-сессия `secretary-v2-codex-continue` после перезагрузки Mac). Основной профиль, посторонние вкладки, native auth и исторические темы не изменяются.

## Исходная ошибка и негативная проверка

Первый настоящий запрос в General содержал уникальный marker `CODEX_TELEGRAM_GENERAL_P5_201`. Он дошёл до production Secretary и создал turn `stn_2095562deca0bc2df3723fb71f820977`. До native Prompt сборка canonical context отказала из-за исторического FX Worker, отсутствующего в текущем inventory. Никакого неизвестного исполнения или скрытого повторного запроса не было.

Telegram показал один входящий error bubble `4294967568`, после пользовательского bubble `4294967567`. Сохранённый `secretary.turn.finished` event seq7397 содержит failed status; GET публичного turn stream возвращает его после повторного подключения. Негативная Telegram доставка подтверждена, это не положительный Codex round-trip.

В Web пользовательский input виден, но первоначальная загрузка не подключала stream последнего turn из Telegram. Исправление `c545ef0` добавляет scoped latest turn ID и replay на загрузке/обнаружении внешнего turn. Исправление `47b9a2d` представляет валидную историческую HarnessInstance как unavailable из immutable ProjectSnapshot, сохраняя старые binding и текущий inventory. Положительный General203 и replay interrupted General202 ниже проверяют эти исправления на новой сборке.

## Положительные сценарии

На исполняемой сборке `43cd444b4f4d558a73f5546bc8dedc995b56749e` новый General input `CODEX_TELEGRAM_GENERAL_P5_203` завершён успешно. Turn `stn_857a15e634c98dcb6fc616fa18f8f6cd`, Input `sin_ebdf544a43b9bda1f390e92c028486b0`, одна canonical entry `ent_7b9e8f63d4990ebf1c6df065aa73945f`, Conversation seq184, global event seq7704; один terminal seq7705. Telegram: пользовательский bubble `4294967589`, ровно один входящий ответ `4294967590`. Web DOM содержит один paragraph ответа, live stream показывает SUCCEEDED без второй копии текста. Ответ появился через настоящий native Codex `gpt-6.1-sol/medium`; синтетического terminal или автоматического retry не было.

Предшествующий General202 сохранился interrupted после контролируемого restart: в Telegram один error bubble `4294967588`; после reload Web показывает INTERRUPTED и `role=alert` с `runtime restarted before completion was proven`. Native input 202 не повторялся. Причина и исправление shared ACP reader описаны в [отчёте](acp-terminal-reader-20261010.md), независимые [Spec](acp-terminal-review-spec-20261010.md) и [Standards](acp-terminal-review-standards-20261010.md) без замечаний.

Через авторизованный owner MCP bridge (не через решение Secretary модели) ранее созданы два диагностических Workers. Их короткие Results с native MCP/profile и idle Follow-up появились в General и отдельных topics274/276. Для Worker215 topic содержит исходный Task prompt bubble `4294967571`, Result `4294967580` и отдельный idle Result `4294967582`; Task prompt не считается дубликатом Result. Это подтверждает права бота и маршрут доставки, но не заменяет модельное делегирование Secretary.

В работе: создание Workers самим Secretary через native MCP, direct steering и `/q` через Telegram, Markdown/code и последующий общий restart/replay. Fixtures, успешный login status и доступный health endpoint не заменяют эти сценарии.

## Делегирование самой моделью Secretary

General input `SECRETARY_DISPATCH_ACCEPTED_P5_223` (`sin_edba11f6fa9c24c9c6250e80919eba7f`) завершился succeeded, с одной canonical entry. Native Secretary вызвал `list_nodes` (seq7731) и два `spawn_worker` (seq7735/7743). Ручные API spawn не подменяли этот сценарий.

- omarchy: `wrk_b51550807cda85a72170b0f1d839ca7c`, topic296 «Codex MVP omarchy», native session `01a12530-42e9-7452-a7c3-6140585f362b`, Attempt `att_f2e30c047310379cc2744327d3971c4f`, Result `res_84b492489d13d6b7cdc600146a26f3a6`, canonical entry `ent_0706c0d59cc59f129835c1d5fff158dd` seq7882.
- Mac: `wrk_889d134968f452ab2e044f1e5f753089`, topic298 «Codex MVP Mac», native session `01a12530-6fd6-71d1-ba70-245360a33d4d`, Attempt `att_4e759814aae4a9d080fe63d7b684dcbe`, Result `res_ac46a8866b77601f9f104d3454465c90`, canonical entry `ent_d6bae83d5f76ff7acc9d61c469c36983` seq7940.

Оба Worker прочитали MCP на своей машине и вернули точный локальный marker плюс developer profile `P5_PRODUCTION_PROFILE_213`. После Results новый Secretary turn не создавался. Telegram General показывает confirmation223 bubble `4294967596`; omarchy Result — bubble `4294967601`. Topic296 показывает исходную задачу `4294967593` и один Result `4294967600`, с заголовком, списком и блоком JavaScript. Исходный Task prompt не считается повторной доставкой Result.

Новый direct topic input224 виден как пользовательский bubble `4294967605`, но на 09:48Z Worker оставался idle с одним Attempt/Result. Steering и queue пока не объявляются пройденными: проверяется реальный ingress, без обхода через owner API.

## Direct input и FIFO через настоящие темы

Omarchy topic296: обычный Follow-up224 принят, наблюдён собственный `sleep 60` в ancestry production Node2557324. Attempt `att_60ff5e89e80eeb06669b568876974f0d`; steering225 изменил его Result на `STEER_FINAL_P5_225` с прежним context224/profile213. Result `res_13d25fdc037ac6fc444ac783d3431bcd` accepted09:49:11.309Z. `/q`226/227 созданы pending09:49:07.642Z/09:49:10.524Z, до terminal; delivery1 после него09:49:12, Result1 `res_45ab84a5a7a071fa9fefae6b4af55ab2`09:49:27; delivery2 только09:49:28, Result2 `res_58a0810c573959cba617d88726358b4a`. Итого4Attempts/4Results и прежняя native session. Telegram topic: queue confirmations7611/7612, steering Result7613, FIFO2 Result7619; General:7614/7617/7620. Prefix `/q` отсутствует в сохранённом prompt.

Mac topic298: наблюдён собственный `sleep 90`, ancestry67106→66900→66898→66897→Node58487. Attempt `att_accf6b23d03e446d364cca6f46442086`, steering229 применён в том же Attempt, Result `res_b0e23b4126f61a0e0bb0a3923e354c69`09:50:54.576Z, с context228/profile213. `/q`230/231 pending09:50:51.019Z/51.585Z до terminal, delivery09:50:55/09:51:10 только после предыдущего Result. Results `res_a518e876751d12cecdf35bb00fa15860` и `res_fd32beb8f67be894ac5dcd04c91ccbcb`, прежняя session, итого4/4. General steering7629, FIFO7632/7637. Раннее окончание sleep90 отдельно проверяется по native metadata; elapsed sleep не выдаётся за90 секунд.

General232 дал один ответ bubble7635, независимо от прямых Worker inputs. Web UI input233 отправлен через textarea/Send; DOM показывает один canonical paragraph, Telegram General — один bubble7638 `CODEX_WEB_TO_TELEGRAM_P5_233`.

## Длинный Markdown и code

Worker Follow-up234 вернул длинный Result с75 numbered lines, start/end markers, прежним context224 и fenced JavaScript. Topic296 получил два incoming chunks7640/7641, длина DOM4093/3473 (включая UI metadata). General получил два chunks7642/7643, длина4013/3572. Start и end присутствуют по одному в соответствующих частях; code DOM содержит32 символа `console.log("LONG_CODE_P5_234")` без дополнительных backslashes. Task input7639 исключён из подсчёта Results.

Короткий code221/222 тоже содержит корректный `console.log("ok")` (17 символов), zero backslashes в decoded DOM. Внешняя JSON-сериализация AXI визуально экранирует кавычки, это не дефект formatter; [независимая проверка](telegram-code-quotes-review-20261010.md) подтверждает неизменный body через Bot API.

Production Web observer правильно показывает обе server-owned queued messages как Delivered и точный binding. Однако native text deltas пока отображаются отдельными карточками по токену: readability дефект04 исправляется перед закрытием gate.

## Restart и сохранение контекста

Контролируемый restart43 выполнен при idle: до/после нормализованные snapshots совпали по всем Attempt/Result IDs/counts и Worker bindings. Model-created omarchy5/5, Mac4/4; диагностические215=2/2,216=3/3. Native identities42e9/6fd6 сохранены, все36 topic mappings совпали, оба outbox пусты; автоматических Prompt не было. HTTPS healthok и оба Nodes online. Новый explicit topic Follow-up236 не повторял marker224 в input, но ответил `RESTART_RESUME_OMARCHY_P5_236`, `CURRENT_CONTEXT_P5_224`, profile213 (один incoming bubble7646). Аналогичный Mac237 вернул предыдущий context228/profile213 (один incoming7648). Исходные user bubbles7644/7645 исключены из Result counts.

Для native Mac228 фактическая долгая команда наблюдалась в собственной ancestry; runtime trace содержит один task_started/task_complete и тот же native turn `01a12537-ffa8...`, без cancel/interrupt marker. Утверждается применение steering в активной Attempt до её terminal, а не полный90-секундный elapsed sleep. Core не создавал следующий turn для steering229.

## Финальная сборка и живой Web direct input

На immutable `af1290a15bae1b345c20654e26143fc0dab859ec` новый input238 отправлен через обычное поле Message Worker и кнопку Message в production Web observer. Attempt `att_7480e50d50dc1257db461f0ea5594a9a`, Result `res_c77cded3e827cb48292f1931013ea2ba` succeeded.40 assistant_text_delta events9563–9602, включая два whitespace-only deltas; concat точно равен Result156bytes, SHA256 `70beb0114093d6feffa3ee1d762ffa9f82f9595ee8d4b24683ac52ab816079ea`.

AXI DOM: ровно один matching activity block, настоящий h1 `WEB_NATIVE_ACTIVITY_P5_238`, strong `bold words`, paragraph `Native activity preserves spaces and bold words.`, отдельный paragraph предыдущего context224, fenced pre/code `console.log("WEB_CODE_P5_238")` с сохранённым newline. Backslashes0, alerts0, действие Accepted. Это новая native работа и реальный Web renderer, а не только SSR regression. Topic296 получил один Result bubble7650; General — один bubble7651. Исторические потерянные whitespace chunks не реконструировались и durable history не переписывалась.

Оба independent [Spec round2](worker-activity-review-spec-round2-20261010.md) и [Standards round2](worker-activity-review-standards-round2-20261010.md) имеют0 замечаний. Public whitespace wire RED→GREEN и19 Web tests перечислены отдельно от native evidence.

## Обычная конфигурация после удаления fixtures

После закрытия четырёх собственных diagnostic/model-created Workers обычный profile222bytes возвращён, только `codex_acceptance` удалён из обеих Node MCP configs. Auth, Codex pins `gpt-6.1-sol/medium`, mappings и historical snapshots сохранены. Secretary получил новый Telegram General240 после idle restart и сам вызвал list_nodes9638 + два spawn_worker9642/9650; Turn `stn_74fb6e26efc555e45a3bca393a6f8476`, Input `sin_b4ae5b036c7fbbfe17f805c947185347`, succeeded/accepted10:13:22.992Z. Один canonical confirmation в General bubble7659.

- Omarchy Worker `wrk_02a5ecec278da6307e16e2417d9285e1`, Attempt `att_5e3ace06bfcdc298a6759a51c38bcd6a`, Result `res_a6c1184cf0f79d9850b35e2a7303cd9f`, native session `01a1254d-5f42-7190-99ee-b3937dc3b82c`. Один реальный native `pwd`; путь правильный. Topic357: Task prompt7654, один Result7660; General7661.
- Mac Worker `wrk_9982fccac787715bef3b944cd8a568e5`, Attempt `att_525fc0581caa7cf6bce3f8f32202c6f0`, Result `res_ae631cb76cb43d4e4d7af8c1ed72e009`, native session `01a1254d-8bab-7fb1-a8d1-d19e6eb846f7`. Один native `pwd`; путь правильный. Topic359: Task prompt7656, один Result7664; General7665.

Оба по1Attempt/1Result, обычный profile.content222bytes без213; Node MCP lists empty, native auth и medium pins сохранились. Topic titles детерминированы из Task prompt, не выдаются за точное соблюдение запрошенных названий. Ответы содержат headings «Обычный Codex omarchy/Mac». Эти Workers завершаются штатным Close после сохранения evidence, сохраняя темы и историю.
