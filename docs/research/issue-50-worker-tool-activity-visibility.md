# Issue #50: видимость активности инструментов Worker

Источник: [GitHub issue #50](https://github.com/GitHubFoxy/secretary/issues/50). Исследование просмотрело текущую реализацию; код и issue не менялись.

## Рекомендуемый scope

Флаг управляет только строками вызова и результата инструмента (`tool_call`, `tool_result`) в ленте Worker activity внутри Worker observer Web UI. Остальные строки активности, состояние Worker, approvals, управление и terminal Result остаются видимыми. В issue речь о видимости в UI; API, event storage и доставка активности должны продолжать работать как прежде.

Основание: runtime contract определяет Worker observer как client view состояния и активности одного Worker, а Worker activity — как ephemeral text, tool и status events ([`runtime-contracts.md`](../architecture/runtime-contracts.md), «Worker profile и Dispatch»). В Web UI лента находится в [`App.svelte`](../../web/src/App.svelte): `openWorker` загружает replay, `connectActivity` получает live events, а observer рендерит `<WorkerActivity>`. [`WorkerActivity.svelte`](../../web/src/WorkerActivity.svelte) отображает строки, которые строит `workerActivityRows` в [`ui-model.js`](../../web/src/ui-model.js); там `tool_call` и `tool_result` уже распознаются как отдельный label `Tool` и форматируются отдельно. Это даёт точную границу для presentation-only фильтра.

Область флага — именно лента activity. Секция `Diagnostics` в том же observer отдельно показывает `raw_harness_details`, в том числе поле `tool` и сырой `details`; issue не просит скрывать диагностические данные. Если подразумевается также скрытие этого debug представления, scope нужно расширить отдельно.

## Конфигурация и default

Предлагаемый ключ: `[worker_observer] show_tool_activity = true`.

- В `Config` есть отдельные секции для политик и конкретных поверхностей (`TelegramPolicy`, `ASRConfig`); настройки запуска Worker сгруппированы отдельно в `WorkerPolicy` ([`internal/config/config.go`](../../internal/config/config.go), `Config`, `WorkerPolicy`). Настройка показа относится к observer UI, а не к выбору/исполнению Worker tool, поэтому отдельная секция `worker_observer` точнее `worker_policy`.
- Используется положительный `show_...` ключ: такая форма уже есть у `ASRConfig.ShowASRInChat` (`show_asr_in_chat`, [`internal/config/asr.go`](../../internal/config/asr.go)). Для Worker visibility подходящее название поля — `ShowToolActivity`.
- Default должен быть `true`, чтобы обновление не меняло существующее поведение, при котором UI показывает tool events. Это вывод из текущего поведения, а не зафиксированное ранее решение.
- Важно: отсутствующий TOML bool декодируется как `false`, а `Load` сейчас задаёт начальные defaults только для Telegram (`internal/config/config.go`, `Load`). Чтобы старые config.toml сохраняли видимую ленту, при загрузке нужно инициализировать поле `true` до `toml.Unmarshal` (или эквивалентно различать unset и `false`). Пример из `internal/config/defaults/config.toml` и `docs/config.example.yaml`/документацию следует привести в соответствие; основной формат конфигурации — TOML ([`docs/configuration.md`](../configuration.md)).
- `Config.Diff` вручную собирает изменения по секциям (`internal/config/config.go`, `Diff`); новый раздел нужно включить в diff, чтобы reload/change recording замечал переключение. `Manager.Reload` подхватывает новый Snapshot без автоматической перезаписи существующего файла ([`internal/config/manager.go`](../../internal/config/manager.go)).

## Передача настройки в UI и неизменяемые слои

Сейчас browser получает `/v1/bootstrap`; ответ содержит owner/conversation IDs, выбранную модель Secretary и workers (`bootstrapResponse` в [`internal/webapi/user.go`](../../internal/webapi/user.go)). [`App.svelte`](../../web/src/App.svelte), `loadState`, получает этот ответ, но читает из него только эти поля и не сохраняет config. Полный config Snapshot отдельно доступен control-room endpoints через `ConfigSnapshot` (`internal/webapi/control.go`, `controlConfig`/`controlOverview`), подключённый в [`cmd/secretaryd/main.go`](../../cmd/secretaryd/main.go), но Worker observer не использует control-room API.

Минимальный поток настройки: `config.toml` → `Config`/reload Snapshot → безопасное UI-настройке поле в ответе bootstrap → состояние `App.svelte` → prop `WorkerActivity` или параметр `workerActivityRows`. Не отдавать в bootstrap целиком `config.Snapshot`: текущая граница специально возвращает узкий DTO, а `sanitizePublicJSON` не заменяет явное решение о публичных полях. Булево предпочтение показа не секретно; при необходимости оно одинаково доступно paired Client с `conversation:read`, так как `/v1/bootstrap` авторизует эту scope.

Не фильтровать события в server API: `GET /v1/workers/{ref}/activity` читает replay и санитизирует его, а WebSocket route также передаёт live activity (`internal/webapi/observer.go`, `workerActivityReplayJSON`, `workerActivityReplay`, `workerActivity`). Runtime contract говорит, что Worker activity получают только открывшие Worker observer и что terminal Result не является activity (`runtime-contracts.md`, тот же раздел). Скрытие только в ленте сохраняет cursor, replay/reconnect и прочих API consumers.

## Acceptance criteria

1. Добавить и описать `[worker_observer] show_tool_activity`; новые установки и старые config без ключа сохраняют текущее отображение (`true`). Явное `false` валидно и выключает показ.
2. При `true` Web UI показывает tool call и result как сейчас; при `false` не показывает ни одну из этих строк. Скрытие обеих сторон важно для избежания одинокого результата или вызова.
3. Скрытие не убирает text/status activity, approvals, Worker state/actions или terminal Result.
4. Фильтр применяется одинаково к первоначальному replay, последующим live events и reconnect. Cursor двигается по полученным событиям, даже если их строки не показываются.
5. Сервер сохраняет и отдаёт activity без изменения; настройка presentation не меняет Worker runtime, event storage, retention, санитизацию, client scopes или каналы Telegram.
6. Изменение config через существующий reload путь становится видимым в bootstrap; открытая страница обновляет preference при очередном `refreshState` (сейчас он запускается раз в 5 секунд), а открытый observer применяет обновлённый prop без перезагрузки страницы.

## Edge cases и открытое решение

- `tool_call` может не иметь имени и поэтому не создавать строку; `tool_result` может нести ошибку/статус. При отключённой видимости скрываются оба вида по kind независимо от форматируемого текста.
- Скрытые events всё равно должны участвовать в cursor progression: иначе WebSocket reconnect может повторно получать уже принятые события. Cursor сейчас обновляется в `appendActivity` до рендера.
- При default `true` нужна защита от нулевого значения старого config после reload; запись default только в новый sample не покрывает уже существующий файл.
- В bootstrap сейчас есть paired Client surface, не только браузер владельца. Для этого поля это допустимо, но реализация должна не возвращать полный runtime config.
- Диагностический раскрывающийся блок содержит `detail.tool`/`detail.details`; выбранный scope оставляет его без изменений. Если запрос означал запрет видеть названия tools где-либо в UI, это отдельный scope, который потребует скрывать или редактировать и `raw_harness_details`.
- В repo нет `docs/adr/`; в изученных domain docs уже описан observer contract, но не существует решения о конфигурации видимости. Default и граница feed-only — предлагаемые ticket decisions, выведенные из текущего поведения и формулировки issue.

## Затрагиваемые файлы и слои для реализации

- Конфигурация/default/reload: [`internal/config/config.go`](../../internal/config/config.go), [`internal/config/defaults/config.toml`](../../internal/config/defaults/config.toml), [`docs/configuration.md`](../configuration.md), при необходимости [`internal/config/manager.go`](../../internal/config/manager.go) только если default не решается в `Load`. `docs/config.example.yaml` документирует отдельный ASR sidecar, а не основной config, поэтому новый раздел туда не добавлять.
- Передача безопасного UI preference: [`cmd/secretaryd/main.go`](../../cmd/secretaryd/main.go) при подключении callbacks, [`internal/webapi/user.go`](../../internal/webapi/user.go) для bootstrap DTO.
- Отображение и последовательность событий: [`web/src/App.svelte`](../../web/src/App.svelte), [`web/src/WorkerActivity.svelte`](../../web/src/WorkerActivity.svelte), [`web/src/ui-model.js`](../../web/src/ui-model.js).
- Не менять для этого ticket: activity producer/runtime, durable event storage и маршруты replay/WebSocket в [`internal/webapi/observer.go`](../../internal/webapi/observer.go), если scope остаётся presentation-only.

Проверку/тесты реализации следует добавить вокруг `Load` старого config без ключа и с явным `false`, передачи bootstrap preference, фильтрации обоих tool kinds, сохранения text/status, а также replay/live cursor поведения.
