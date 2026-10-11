# Добавить Antigravity harness через официальный ACP server

Type: task
Status: ready-for-agent
Blocked by: None

## Цель

Добавить Google Antigravity как самостоятельный runtime harness в Secretary, используя официальный `agy_acp_server` и существующую ACP реализацию. Решения и исследовательские ссылки: [спецификация](../spec.md), [исследование](../research.md).

## Контекст

- Google публикует `agy_acp_server` как Antigravity ACP agent в официальном ACP Registry. Это отдельный executable от пользовательского `agy` CLI.
- T3 запускает официальный server по stdio ACP и использует общий session runtime плюс Antigravity-specific support/normalization. ACP config catalog приходит в ответах setup/resume и уведомлениях `config_option_update`; отдельного `session/config_options` RPC нет. T3 меняет options через общий `session/set_config_option` путь.
- В Secretary есть Go ACP transport и `ACPRuntime`; его `Resume` вызывает только `session/load`. ACP различает `sessionCapabilities.resume` и `sessionCapabilities.loadSession`; перед выбором проверить initialize response Google server. Не менять Codex/OpenCode поведение.
- CLI auth и ACP auth раздельны. Не принимать успешный `agy` login за ACP authentication и не читать credentials напрямую. `ACPRuntime.connect` сейчас только вызывает `initialize`, не выполняет ACP `authenticate`.
- В T3 Antigravity deployment требует companion `localharness_external` рядом с `agy_acp_server`, включает `ANTIGRAVITY_HARNESS_PATH`, разрешает symlink перед поиском companion и передаёт Linux platform args из ACP Registry entry. В первой версии установка внешняя; путь/пара файлов и args должны быть частью проверенного launch contract.
- Daemon обновляет inventory каждые 5 минут. Текущий discovery сохраняет только `HarnessInstance` и теряет детали ошибки `ProbeResult`; сам instance не содержит diagnostic reason.
- На машине пользователя найден Google CLI `agy` 1.3.3, но не ACP binary/companion. Официальный Registry artifact 1.3.0 был временно запущен из `/tmp` с изолированным home только для `initialize`; ответ подтвердил `sessionCapabilities.resume`, `loadSession`, и четыре auth methods. Реальные auth и session operations ещё не проверены; детали и checksum basis записаны в [research](../research.md).

## Объём

1. Добавить `core.HarnessAntigravity` со значением `antigravity`; разрешить его во всех конфигурационных и CLI allowlist/legacy seams.
2. Добавить отдельный `AntigravityRuntime` compatibility adapter по образцу `CodexRuntime`, отдельное поле маршрутизатора `RuntimeRouter.Antigravity` и wiring в обоих node entrypoints. Существующий `internal/acp.Client` остаётся JSON-RPC transport, общий `ACPRuntime`/`acpSession` — переводчиком стандартного ACP в Secretary `Session`/`Activity`/`Result`. Не добавлять второй transport или универсальный proxy. Не использовать Codex `CODEX_CONFIG`, profile environment, parser или Codex kind. Runtime должен иметь отдельные команду, аргументы, env и стабильный auth/profile path для discovery, Start и Resume.
3. Проверить официальную команду и platform args в Registry entry; разрешать установленный `agy_acp_server` через PATH или отдельный override `SECRETARY_ANTIGRAVITY_COMMAND`. Проверить обязательный `localharness_external` companion рядом с реально разрешённым executable, symlink resolution и `ANTIGRAVITY_HARNESS_PATH`. Если пара/аргументы отсутствуют, вернуть явную unavailable причину. Не скачивать бинарники в рамках этой задачи.
4. Определить поддерживаемый ACP auth flow и onboarding для оператора по server-declared auth methods. Для `initialize`, ACP `authenticate`, discovery, Start и Resume использовать одну и ту же выделенную profile directory и environment. Не импортировать `agy`/ambient Google credentials и не читать token stores. Если login URL выводится plain text в stdout/stderr, не ждать общий RPC timeout: безопасно распознать и отобразить только очищенную ссылку/инструкцию, не сохраняя произвольный вывод или secret query params. Если неинтерактивный вход невозможен, вернуть actionable недоступность. Не добавлять multi-account UI/browser callback relay.
5. Обрабатывать config catalog по ACP контракту: читать полный `configOptions` из `session/new`/resume ответа и `session/update` с `config_option_update`; сохранить и проверить точные native IDs/values, категории, варианты и зависимые options. Применять explicit model/mode/reasoning pin через `session/set_config_option`, проверять ответный полный каталог и повторно применять/проверять pins после resume. Не вызывать несуществующий `session/config_options` метод, не угадывать catalog из `agy models`.
6. Выбирать session restoration method по фактическим `sessionCapabilities`: предпочитать `session/resume`, когда он объявлен (Google ACP 1.3.0 объявляет и `resume`, и `loadSession`); использовать `session/load`, если resume не поддерживается. Сохранить существующий способ для Codex/OpenCode; определить поведение при отсутствии обеих возможностей. Проверить живой restart: завершить ACP процесс, восстановить тот же native session, отправить follow-up и проверить, что события старой истории не попали в новую Attempt.
7. Добавить Antigravity-specific native request normalization там, где общей ACP обработки недостаточно. В частности, `interaction_*` в `session/request_permission` являются вопросом с фиксированными option IDs, а не безусловным approval: преобразовать их в user-input activity, при ответе отправить выбранный native option ID и оставить запрос нерешённым при неизвестном выборе. Обычные permission options показывать только как пришли от агента; не включать yolo/skip-permissions автоматически.
8. Определить отдельную проверенную terminal-result политику для Antigravity `session/prompt`: один итоговый Result после drain предшествующих событий; различать завершение, cancel, отказ, пустой ответ и обрыв процесса. Не включать Codex/OpenCode completion flags без подтверждения их native metadata.
9. Проверить передачу `StartRequest.MCPServers` на `session/new` и восстановление; acceptance требует реальный вызов MCP tool в новой и восстановленной сессии. Проверить фактическое применение managed instructions/skills через существующий profile delivery; не включать ACP filesystem capability автоматически.
10. Добавить отдельный ACP observation path для discovery. Не запускать полный Antigravity bundle и временную session на каждом 5-минутном inventory refresh: кешировать дорогие auth/catalog observations с явной TTL/invalidation, оставив дешёвую проверку наличия/пары файлов. Сохранять non-secret diagnostic reason для missing binary/companion, auth required, protocol mismatch и catalog failure в inventory/операторском ответе; не терять `ProbeResult.ErrorCode`.
11. Включить Antigravity в корректное закрытие subprocess/session до записи Outcome в `ExecutionNode.watchSession`; проверить, что close завершает companion/дочерние процессы без утечки.

## Критерии приёмки

- `antigravity` проходит валидацию как отдельный `HarnessKind`; instance с этим kind маршрутизируется только в Antigravity ACP runtime. При отсутствии runtime команда завершается явной ошибкой unavailable.
- `agy_acp_server` можно разрешить из PATH или передать через существующий executable override. Ошибки запуска/версии/handshake видны в inventory; никакие загрузки с непроверенным digest не выполняются.
- Fixture/integration tests подтверждают initialize capabilities, auth-required fast-fail, конфигурационные options и их update, resume selection, вопрос/approval mapping, MCP передачу, terminal outcomes, diagnostics, cache policy и process close. Codex/OpenCode ACP tests сохраняют прежние ожидания.
- Уже проверенный baseline для официального macOS arm64 artifact 1.3.0: ACP initialize согласует protocol 1, сообщает `resume` и `loadSession`, MCP HTTP/SSE, `image`/`audio`, logout и auth methods `oauth-personal`, `oauth-business`, `gemini-api-key`, `agent-platform`. Не запускать повторный initialize по таймеру. При проверке другой версии снова наблюдать её ответ и не переносить значения 1.3.0 автоматически.
- Вручную проверены launch contract и prompt→result на реальном Google ACP server, если доступна авторизованная среда. Зафиксированы точная версия, Registry args, наличие companion, auth method, negotiated protocol/session capabilities и наблюдённые config options. Если auth/quota недоступны, это прямо указано как незавершённая live-проверка, а неподтверждённые возможности выключены.
- Проверен restart реального ACP процесса: Antigravity предпочитает `session/resume` (Google ACP 1.3.0 объявляет `resume` и `loadSession`), восстанавливает ту же беседу, pins и MCP и не повторяет старые activity в новой Attempt. Codex/OpenCode продолжают прежний `session/load` путь.
- MCP проверен реальным вызовом tool в новой и resumed session. Cancel объявляется только при подтверждении. Steering явно не поддерживается без доказанного same-turn injection; очередь следующего ввода не считается steering.
- Модели, reasoning и режимы берутся из `configOptions` и `config_option_update`, с сохранением native ID/value; неизвестный pin отклоняется, auth-required каталог не объявляет instance ready.
- Antigravity questions `interaction_*` не классифицируются как approval и не получают auto-allow; permission options сохраняют native IDs. Auth required показывается как actionable недоступность без запуска Worker и без чтения секретов.
- Пятиминутное discovery не создаёт тяжёлый ACP процесс/session при каждом цикле. При недоступности binary, companion, auth или protocol mismatch inventory сохраняет непустую диагностическую причину.
- Capability list ограничен реально проверенным поведением. Не приписывать `steering`, approvals, subagents, compaction или reasoning уровни по аналогии с T3 без подтверждения контрактом и реализацией. Не включать multimodal: `image`/`audio` server capabilities, наблюдённые в initialize, не входят в этот тикет, и Secretary media input path не меняется.
- Добавлены/обновлены unit tests для kind/config, discovery, router и ACP resume selection; не изменены ожидания существующих harness.

## Предполагаемые точки кода

- `internal/core/harness_contract.go`
- `internal/config/config.go`
- `internal/node/runtime_router.go`
- `internal/node/codex_runtime.go` как пример отдельного harness adapter
- `internal/node/harness_probe.go` и места сборки `HarnessDiscovery`
- `internal/node/acp_runtime.go` и при необходимости `internal/acp`
- `internal/node/execution_node.go` для subprocess/session close lifecycle
- `cmd/secretary-node/main.go`, `cmd/secretaryd/main.go`
- `internal/migration/migration.go`, `internal/ctl/worker_profile.go`
- Связанные тесты в `internal/core`, `internal/config`, `internal/node`

Найти все exhaustive switch/allowlist и deployment config seams до завершения работы; список выше не исчерпывающий.

## Вне объёма

- Скачивание, обновление и целостность управляемой установки; ACP Registry manifest в исследованной версии не содержал digest.
- Управление несколькими Google accounts, browser callback relay, UI sign-in и T3-specific provider profiles.
- Миграция/импорт `agy` credentials в ACP profile.
- Поддержка IDE extensions, прямого `agy -p` NDJSON runtime или стороннего ACP bridge.
- Объявление steering или других возможностей только на основании поведения T3 Code.

## References

- [Google ACP Registry manifest](https://github.com/agentclientprotocol/registry/blob/main/antigravity-acp/agent.json)
- [T3 Code provider guide](https://github.com/pingdotgg/t3code/blob/main/docs/user/providers-antigravity.md)
- [T3 Code AntigravityDriver](https://github.com/pingdotgg/t3code/blob/f23d0084b6e023ffb1af9e332d8666affc99d542/apps/server/src/provider/Drivers/AntigravityDriver.ts)
- [T3 Code AntigravityAdapterV2](https://github.com/pingdotgg/t3code/blob/f23d0084b6e023ffb1af9e332d8666affc99d542/apps/server/src/orchestration-v2/Adapters/AntigravityAdapterV2.ts)
- [T3 Code AntigravityAcpSupport](https://github.com/pingdotgg/t3code/blob/f23d0084b6e023ffb1af9e332d8666affc99d542/apps/server/src/provider/acp/AntigravityAcpSupport.ts)
- [ACP Session Config Options](https://agentclientprotocol.com/protocol/v1/session-config-options)
- [ACP session/resume capability](https://agentclientprotocol.com/rfds/session-resume)
- [T3 Code pinned Antigravity release catalog and artifact SHA-256 values](https://github.com/pingdotgg/t3code/blob/f23d0084b6e023ffb1af9e332d8666affc99d542/apps/server/src/provider/antigravityRelease.ts)
