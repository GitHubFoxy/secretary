# 31 Сделать OpenCode v2 harness по умолчанию вместо fx

Type: task
Status: claimed

## Work

Пользователь попросил заменить fx на OpenCode и сделать его default после сравнения Telegram Worker с успешным локальным OpenCode ответом.

Это изменение product decision: Phase 4 map/spec и текущие defaults задают fx; OpenCode был только compatibility target. Не подменять одно значение в template, оставив deployment, Node inventory, Profiles, docs и acceptance в противоречии.

На omarchy уже установлен OpenCode v2.0.22 для генератора названий Topics, но существующие Secretary и Worker defaults по-прежнему fx; Node `include_opencode=false`. Наличие binary для title generator не означает готовность OpenCode Worker harness.

## Утверждённая область

- `secretary.harness = "opencode"` и `worker_policy.default_harness = "opencode"` для новых Workers.
- Существующие Workers сохраняют immutable Node/HarnessInstance binding и native session history. Не выполнять silent migration или создание replacement session.
- fx, Claude Code и Codex остаются selectable adapters. Изменение default не снимает их compatibility/acceptance coverage.
- Title-generation harness/config из tickets17/18 остаётся независимым.

## Acceptance

- Настоящий OpenCode v2 проходит ACP startup/probe и безопасный tool/model/permission round trip.
- Node публикует ready HarnessInstance для v2; dispatch использует observed capabilities, а не факт наличия binary.
- Согласованный default применяется к чистой установке и явной migration существующего deployment.
- Existing external Profiles, model pins и data сохраняются либо изменяются только по утверждённому migration plan.
- Unknown/unsupported model или reasoning возвращает видимую ошибку. Нет silent model/harness fallback.
- Новые Worker и Secretary smoke tests проверяют короткий ответ, tool activity и terminal Result. Progress не смешивается с Result, Secretary не пересказывает Result.
- Docs, product contract, setup/doctor и release gate согласованы с новым default.
- Проверены restart/reconnect и Follow-up существующего fx Worker без migration.

## Investigation, 3 октября 2026

Установленный на omarchy OpenCode `2.0.22` распознаёт `acp`, `models` и `run`. `opencode models` содержит кандидат `openai/gpt-6.1-sol`; наличие модели в каталоге не доказывает credentials, доступ provider или успешное выполнение. Paid model call не запускался.

`internal/node/opencode_runtime.go::prepare` и wiring в `cmd/secretaryd/main.go` существуют. Но Node inventory не содержит OpenCode, deployment оставляет `include_opencode=false`, а default probes его не включают. Нужна явная registration/probe, а не только переключение config.

Wire-level ACP handshake, настоящая Worker session и безопасная runtime model/reasoning reconfiguration в этой read-only диагностике не проверялись. Именно эти проверки являются prerequisite rollout.

## Open decisions

- Пользователь прямо разрешил переключить и Secretary, и Workers на OpenCode. Default меняется для новых Workers; существующие bindings/history не мигрируют.
- Актуальное явное решение пользователя: Secretary — `openai/gpt-6.1-sol` / `xhigh`, новые Workers — `openai/gpt-6-luna` / `xhigh`. Оно заменяет прежнее preserve `gpt-5.6-luna` / `low` и provider-default для новых Workers. Existing Workers не менять.
- Explicit preferences и Project pins имеют приоритет над default Worker model/reasoning; неизвестные или unsupported pins возвращают ошибку без silent model fallback.
- Прямой запрос "Switch ... on both" разрешает применение на omarchy после проверок совместимости, с backup и rollback. Telegram slash commands из ticket32 в эту работу не входят.

## Related

Tickets21/29: Result и отсутствие повторного Secretary ответа. Ticket30: отдельная причина web_fetch failure. Ticket32: Telegram выбор model/reasoning.

## Implementation status

Локальная реализация остаётся claimed для review. Родитель подтвердил automated/native проверки, но повторный authenticated Worker run обнаружил FAIL на attempt0 старого acceptance prompt. Focused диагностика воспроизвела буквальный CURRENT с hidden marker, без текущего file nonce; definitions были неоднозначно размещены после first-answer format. Prompt исправлен без ослабления exact gate и без runtime changes; три заранее запланированных независимых live Worker ReadAndResume прогона прошли, включая marker/history proof. Подробные исходные FAIL и последующие outcomes сохранены ниже. Clean-install defaults, immutable fx bindings, native profile/model gates и inventory сохранены. Production rollout и общий Telegram gate не выполнены.

### История проверок до reset/login и нового model choice

Обновлены clean-install defaults, dispatch fallback, Node OpenCode ACP probe, OpenCode v2 profile schema/permission delivery, изоляция user-global config и относящиеся к default docs. Старые external fx config и immutable Worker bindings не мигрируются.

Родитель исправил synthetic fixture: OpenCode отдельно вызывает tool-free генерацию названия сессии. Fixture ошибочно отдавал tool call этому запросу, а основной turn получал только финальный текст. После разделения запросов настоящий V2 `read` прочитал harmless fixture file; его содержимое появилось в tool message следующего model call.

Остаётся отдельное ограничение normalized tool activity: V2 ACP передаёт title/kind/status/locations/rawInput, но не explicit tool identity fields. Ticket20 намеренно не угадывает name по title; поэтому activity не публикуется. Не выдавать отсутствие normalized activity за отсутствие фактического выполнения tool.

Secretary MCP acceptance пока не подтверждён. Публичные V2 docs `/v2/docs/mcp-servers` объясняют, что MCP по умолчанию `codemode=true`, а `codemode=false` экспортирует tools напрямую. Native ACP MCP mapping/config нужно проверить с этим contract, не открывая Secretary builtin tools.

Первоначальный live isolated model probe на omarchy для текущей `openai/gpt-5.6-luna` завершился с HTTP401. После повторного device flow пользователь подтвердил вход: CLI завершился успешно, native auth store содержит новое активное OpenAI credential. Повторный isolated live model call завершился с exit0 и ожидаемым ответом `OK`; credentials/response payload не выводились.

Исправлены три проблемы delivery:

- `internal/acp/jsonl.go` раньше дополнял очищенный environment исходным `os.Environ()`, возвращая исключённые `SECRETARY_*` переменные. Добавлен точный process environment для OpenCode, прежний overlay contract остальных adapters сохранён. Regression проверяет реальный subprocess.
- Native ACP `session/set_config_option` использует model ID `provider/model/variant`, не CLI `provider/model#variant`. Явный выбор наблюдаемого reasoning variant `low` подтверждён synthetic provider полем `reasoning_effort=low`. Custom variant ID исключён: authenticated OpenAI catalog его не публиковал.
- MCP регистрируется асинхронно после ACP session/new. В runtime добавлена короткая context-cancellable startup stabilization для MCP; fixture больше не добавляет собственного sleep. На чистом native data store direct `secretary_list_workers` реально вызывается, tool result доходит до provider, builtin/extra tools отсутствуют, scoped MCP environment подтверждён. Широкие разрешения и `execute` не добавлены.

`go test -race ./...`, `go vet ./...`, `git diff --check` и повторные native HTTP/MCP fixtures проходят. Linux native fixtures на omarchy также проходят с чистым data store.

**Live blocker:** с существующим `~/.local/share/opencode/opencode.db` native mode catalog публикует только `build`/`plan`, а управляемый `secretary-managed-<hash>` отсутствует. Без явного mode selection модель могла успешно ответить `OK`, не вызвав MCP. Теперь runtime явно выбирает managed mode и завершается ошибкой вместо silent profile fallback. Проблема воспроизводится без платных вызовов: тот же synthetic MCP fixture проходит на чистом data store и падает при подключении существующего native data store. Изоляция cache, удаление virtual config override и private data directory с symlink на существующую DB ситуацию не исправляют. Это указывает на влияние существующего native DB state; точная причина ещё не установлена.

Добавлен opt-in `TestOpenCodeAuthenticatedSecretaryMCP`: существующий provider auth, текущие model/low, private synthetic lifecycle server; live Workers и Conversation не создаёт. Он остаётся красным на текущем shared native DB, поэтому rollout запрещён.

Предлагаемый следующий вариант — отдельный native data store для Secretary/Node с собственной авторизацией, без копирования credentials и без изменения обычной OpenCode DB пользователя. До этого нужно согласовать дополнительный вход с пользователем либо продолжить исследование существующего DB state.

Переключение deployed Secretary/Node не выполнено, работающая fx configuration сохранена. Ticket остаётся claimed; не объявлять live lifecycle acceptance или migration завершёнными.

## Comments

4 октября 2026: пользователь явно разрешил удалить старую OpenCode DB на omarchy и создать новую. Сделан SQLite online backup с integrity_check=ok в `~/.local/share/secretary/backups/opencode-reset-20261004-105257/` (directory0700, DB/auth snapshot0600). Штатный OpenCode background service остановлен; перед удалением проверено отсутствие открытых DB/WAL/SHM descriptors. Удалена только активная OpenCode DB; Secretary DB, конфиги сервисов и fx Workers не менялись. OpenCode создал новую DB при следующем native test. Тот же shared-directory Secretary MCP fixture теперь проходит, включая три повторных запуска. Это подтверждает пользу reset в данном deployment, не устанавливая точную внутреннюю причину.

Повторный device login подтверждён: Connected to OpenAI, active openai credential. Authenticated Secretary MCP acceptance прошёл в новой native DB с `openai/gpt-5.6-luna` / `low`: scoped environment, tools/list, настоящий tools/call и ожидаемый terminal Result подтверждены. Это private synthetic lifecycle server, не live Workers/Conversations.

Расширенная opt-in acceptance `TestOpenCodeAuthenticatedWorkerReadAndResume` обнаружила два оставшихся блокера:
- Без model pin native provider-default Worker завершает Attempt с ошибкой provider authentication, несмотря на действующий OpenAI login. Не выбирать другую модель или harness молча; требуется согласованная default-model policy.
- С `TEST_OPENCODE_WORKER_MODEL=openai/gpt-5.6-luna` / low Worker действительно читает приватный файл с неизвестным модели random nonce. После Close/Resume исходный custom managed mode не найден. Reset DB исправил первоначальный MCP round-trip, но не доказал совместимость persistent Worker sessions.

Проверенные, но отменённые эксперименты: native `session/resume` вместо legacy `session/load` не исправил custom mode; explicit managed override builtin build с deny-first policy позволил read/Resume и MCP, однако ACP не подтвердил ожидаемый profile-hash description marker. Не оставлять этот workaround как silent fallback на build/plan. `opencode acp --standalone` не поддерживается v2.0.22 (API/auth CLI поддерживает этот flag). Runtime сохранён с custom managed-mode selection и fail-closed поведением.

Обычные `go test -race ./...`, vet, build, native synthetic fixtures и CLI/deployment scripts проходят. Opt-in live Worker resume/default-model acceptance остаётся красным. Deploy, commit, restart Secretary/Node и изменение bindings не выполнены. Server/Node остаются на fx.

Для новых пользователей документировано требование автоматически создавать отдельный постоянный native store один раз при setup, сохранять его при restart/Follow-up и не трогать личную OpenCode DB. Реализация вынесена в ticket33; её нельзя считать уже выполненной.

Пользователь явно выбрал Worker `gpt-6 luna xhigh`, затем Secretary `6.1-sol xhigh`. Оба exact qualified ID наблюдаются в `opencode models` на omarchy. Live Secretary MCP с новой моделью/effort PASS; live Worker attempt0 read с новой моделью/effort PASS, Resume всё ещё FAIL (managed mode not found). Новые defaults реализованы локально; production config и существующие fx Workers не менялись. Regression покрывает model/reasoning priority, unknown observed pin rejection, независимые profile hashes/config diff и reload/new-binding versus immutable replay.

Финальные проверки этого изменения: `go test ./...`, `go test -race -p 1 ./...`, `go vet ./...`, `go build ./...`, `git diff --check`, sex CLI и Node deployment scripts PASS. Первый параллельный race run поймал heartbeat/offline timing failures в существующих daemon/Approval интеграционных тестах; последовательный полный race run прошёл, unrelated timing logic не менялась. Live Worker Resume остаётся отдельным красным acceptance gate. Перед rollout дополнительно подтвердить публикацию новых model/reasoning pins в реальном Node inventory, не объявляя xhigh observed только на основании обычного plaintext `opencode models`.

Пользователь подтвердил приоритет: закончить31 с OpenCode и остаться на нём, если он работает правильно; потом21/22/29 и human review. Создан отдельный ticket34 для будущей проверки полного пути OpenCode → Codex. Реализацию31 продолжает субагент `openai-codex/gpt-6.1-sol` / `xhigh`; родитель делает review. Субагенту не разрешены commit/push, production deployment, restart, reset auth/DB или изменения существующих Workers. Эти действия остаются за родителем после проверки acceptance и согласованного rollout.

### 4 октября 2026 — implementation + acceptance для review

**Доказанная причина текущего Resume failure:** race регистрации config plugins в OpenCode v2.0.22, не доказанная DB corruption и не migration fx → OpenCode. Прочитан исходный tag `v2.0.22`, commit `527f0b931d1f9b3ebd34e106c51b31ce5db5b075`:

- [`packages/core/src/plugin/supervisor.ts`](https://github.com/anomalyco/opencode/blob/527f0b931d1f9b3ebd34e106c51b31ce5db5b075/packages/core/src/plugin/supervisor.ts): initial activation запускается асинхронно через `Effect.forkScoped`; plugin inventory публикуется после activation.
- [`packages/cli/src/acp/catalog.ts`](https://github.com/anomalyco/opencode/blob/527f0b931d1f9b3ebd34e106c51b31ce5db5b075/packages/cli/src/acp/catalog.ts): наличие хотя бы одного enabled model и primary agent уже считается ready, даже когда доступны только builtin build/plan, а managed config ещё не зарегистрирован.
- [`packages/cli/src/acp/service.ts`](https://github.com/anomalyco/opencode/blob/527f0b931d1f9b3ebd34e106c51b31ce5db5b075/packages/cli/src/acp/service.ts): loadSession сначала получает сохранённую session и attach/replay, затем читает catalog; `withReload` делает лишь один немедленный retry. session/resume использует тот же attach/catalog, поэтому смена метода не устраняла race.
- Native synthetic read + Resume в **фактическом текущем provider store** сначала воспроизвёл `-32602 mode not found`. Ограниченный повтор только exact mode устранил эту ошибку, но обнаружил следующую `model not found`; ожидание обеих регистраций и подтверждение echoed config исправило весь flow без изменений native binary/DB/auth.

**Дополнительная ошибка адаптера:** watch запускался после session/load. Большая history могла заполнить ACP buffer и заблокировать load; небольшая history могла асинхронно добавить старые text chunks в новый Result. Теперь watch работает при load с replay flag, FIFO event fence дожидается обработки всех предшествующих notifications, replay не публикуется как новая Attempt. OpenCode prompt Result также ждёт обработку финальных deltas; explicit-summary contract других adapters сохранён. User history echoes не выдаются за assistant text.

**Изменения этого продолжения** (не весь большой dirty tree):

- `internal/node/{opencode_runtime,profile,acp_runtime,harness_probe}.go`: bounded exact selection, native profile marker по фактическому prompt/ordered permission policy, echoed mode/model/effort gate, replay separation, правдивые activity capabilities.
- `internal/node/opencode_inventory.go`: private native model API observer. В environment нет request-supplied variants; model IDs/efforts берутся из enabled native records и `variants[].settings.reasoningEffort`. Общий domain reasoning union не выдаётся за per-model evidence: acceptance дополнительно проверяет settings отдельно для обеих выбранных моделей.
- `internal/acp/jsonl.go`, `internal/acp/barrier_test.go`: local-only FIFO response/event barrier; не wire extension и не fake activity. Exact environment из предыдущей работы сохранён; OpenCode connect не добавляет CODEX_CONFIG.
- `internal/node/opencode_{configuration,persistence_e2e,replay}_test.go`, existing `opencode_{runtime_e2e,live_e2e}_test.go`, `profile_test.go`, `harness_probe_test.go`: delayed/absent catalogs, wrong marker/effort, unsupported method, missing session без replacement, 256-frame replay, native read/MCP + Follow-up + Resume, authenticated system/history proof.
- `cmd/secretary-node/main{,_test}.go`: explicit `SECRETARY_OPENCODE_COMMAND` одинаково применяется runtime и inventory; missing override не подменяется PATH binary. На omarchy `/usr/bin/opencode` оказался v1.18.29, а утверждённый v2.0.22 — `~/.local/bin/opencode`.
- `cmd/fake-codex-acp/main.go`, `opencode_fixture{,_test}.go`: packaging protocol double подтверждает лишь choices, реально присутствующие в generated managed config. Первый CLI run падал, потому что старый double отвечал `{}` новому fail-closed gate; fixture исправлен, gate не ослаблен. Это mock evidence только для CLI assembly, не native acceptance.
- `docs/{configuration,node-deployment,phase4-release-gate}.md`, `.scratch/phase-4/spec.md`, этот ticket: current contract/evidence и pending gates. Остальные предыдущие изменения сохранены.

**Финальные локальные проверки — все PASS:**

```sh
go test ./...
go test -race -p 1 ./...
go vet ./...
go build ./...
git diff --check
zsh scripts/sex-cli-test.sh
zsh scripts/node-deployment-test.sh
```

В этом продолжении serial race suite проходил; исторические unrelated parallel heartbeat/offline failures выше не скрыты и timing logic не менялась. Реальные existing fx Workers не трогались: их immutable routing/policy/replay coverage проходит в suite, но production fx Follow-up/restart не запускался в рамках разрешённой private acceptance.

**Финальная Linux/native acceptance — PASS:** base repo HEAD `459709c384bd1b064781429b40c97746c7405524`, uncommitted working tree. Сборка и доставка только test artifact в `/tmp`:

```sh
GOOS=linux GOARCH=amd64 go test -c ./internal/node -o /tmp/secretary-ticket31-node.test
scp -q /tmp/secretary-ticket31-node.test omarchy:/tmp/secretary-ticket31-acceptance/node.test
ssh omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_ACP_E2E=1 TEST_OPENCODE_SHARED_DATA_HOME="$HOME/.local/share" /tmp/secretary-ticket31-acceptance/node.test -test.run="^TestOpenCode(ACPNativeHTTPFixture|SecretaryMCPNativeHTTPFixture|NativeProfilePersistence|NativeInventory)$" -test.count=2 -test.v'
ssh omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_LIVE_E2E=1 /tmp/secretary-ticket31-acceptance/node.test -test.run="^TestOpenCodeAuthenticated(SecretaryMCP|WorkerReadAndResume)$" -test.count=1 -test.v'
```

SHA256 финального Linux artifact: `2fa352d9fe19e7a7055a5119f4b7b17fcbc1f3c11b59825c0735b8b090dae3f5`. Native fixtures/inventory прошли дважды; Worker/Secretary phases0/1/2 подтвердили managed system, allowed tool, отказ unadvertised shell без side effect, прежнюю history и same ID. Inventory: 28 enabled models, 7 observed reasoning levels; обе выбранные модели имеют native xhigh settings. Final authenticated Secretary MCP PASS (8.02s); Worker ReadAndResume PASS (14.97s), новый random file content и hidden profile marker на обоих turns, original history после restart. Никакие nonce/credentials/raw ACP/tool payloads/reasoning не печатались.

**Ограничения и pending:**

- [`packages/cli/src/acp/tool.ts`](https://github.com/anomalyco/opencode/blob/527f0b931d1f9b3ebd34e106c51b31ce5db5b075/packages/cli/src/acp/tool.ts) публикует title/kind/status/locations/rawInput, но не explicit tool identity. Настоящие tools работают; normalized tool_call/tool_result не объявляются и name не выводится из title. Для native tool cards потребуется upstream metadata support; это не устраняется расширением Secretary permissions.
- MCP startup stabilization остаётся bounded 500ms перед первым prompt; это не доказательство готовности произвольного медленного MCP server. Проверенные narrow private Secretary MCP fixtures/live round-trip проходят без fixture sleep.
- Автоматическое отдельное persistent native store первого setup остаётся ticket33, не реализовано здесь. Этот product setup gate нельзя объявлять закрытым.
- Production server/Node после acceptance остаются active; config/services, fx bindings/history не изменялись. Deployment, commit/push, DB reset, provider login/credential copy не выполнялись. Native upgrade/patch для данного fix не требуется.
- Production rollout, human review и полный Telegram acceptance остаются pending. Tickets21/22/29 — последующая presentation работа; ticket34 — будущий Codex full-path test, не реализован.

**План rollout/rollback только для review, не выполнен:**

1. Родитель независимо проверяет diff, regressions и Linux artifact/evidence; ticket31 остаётся claimed до этого.
2. Перед согласованным rollout сохранить server config/external profiles, Node non-secret config, server/Node durable state и локальные session mappings. Не сбрасывать native store и не копировать provider credentials. Отдельно решить setup requirement ticket33 для новых установок.
3. Явно выбрать один v2 binary для Secretary/Node service environment; включить OpenCode probe, проверить live inventory и narrow managed MCP/read+Resume в том же store. Изменить только согласованные Secretary/новые Worker defaults; title generation независима, existing fx bindings/model/history остаются прежними.
4. Только родитель после owner-approved service procedure применяет новую assembly и проверяет Telegram/new Worker и production existing fx Follow-up. Не считать private tests full Telegram acceptance.
5. При failure восстановить backup config/binaries и прежние defaults, сохранив все durable data/native sessions. Existing OpenCode bindings не мигрировать обратно в fx и не заменять native ID; unavailable session остаётся явной ошибкой. Новые deployments не принимают dispatch до восстановления readiness.

### Parent review follow-up — диагностика неоднозначного live prompt

Исходный parent failure **сохранён, не заменён позднейшим PASS**. Parent подтвердил `go test ./...`, `go test -race -p 1 ./...`, vet/build, CLI/deployment scripts и repeated native persistence обоих roles/inventory/fixtures в shared provider store. Artifact SHA256 совпал с предыдущим implementer artifact: `2fa352d9fe19e7a7055a5119f4b7b17fcbc1f3c11b59825c0735b8b090dae3f5`. Но authenticated run этого artifact дал SecretaryMCP PASS (6.24s), WorkerReadAndResume **FAIL (11.47s)** на attempt0: `status=succeeded`, `summary_bytes=40`, `expected_content=false` вместо ожидаемых65 bytes.

Parent command:

```sh
ssh omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_LIVE_E2E=1 /tmp/secretary31-parent-review.test -test.run="^TestOpenCodeAuthenticated(SecretaryMCP|WorkerReadAndResume)$" -test.count=1 -test.v'
```

**Доказательство причины:** сначала сохранён прежний system prompt и добавлены только boolean/length diagnostics. Definitions CURRENT/FIRST находились в предложении о later requests, после требования first answer `exactly CURRENT|marker`. Единственный заранее запланированный диагностический run вновь дал **FAIL (10.85s)** на attempt0:

```text
summary_bytes=40 expected_bytes=65 expected_content=false
actual_current_present=false original_first_present=false profile_marker_present=true
literal_CURRENT=true literal_FIRST=false separator_count=1
```

Это прямое evidence буквальной подстановки CURRENT при доставленном hidden marker, а не предположение только по длине. Сам failed run не доказывает чтение файла: не выдавать его за read PASS. Raw Summary, nonce, private system prompt, tool arguments/output и reasoning не попадали в вывод diagnostics. Native private logs/system не инспектировались; классификация Summary происходила только внутри теста.

Diagnostic command (старый prompt, без retries):

```sh
GOOS=linux GOARCH=amd64 go test -c ./internal/node -o /tmp/secretary31-worker-diagnostic.test
scp -q /tmp/secretary31-worker-diagnostic.test omarchy:/tmp/secretary31-worker-diagnostic.test
ssh omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_LIVE_E2E=1 /tmp/secretary31-worker-diagnostic.test -test.run="^TestOpenCodeAuthenticatedWorkerReadAndResume$" -test.count=1 -test.v'
```

**Focused fix (только tests/comments, runtime без изменений):**

- `internal/node/opencode_live_e2e_test.go`: definitions обоих placeholders перенесены ДО обоих форматов, явно запрещён буквальный вывод CURRENT/FIRST. Реальный read на каждом запросе, random current content, hidden profile marker и original FIRST history остаются обязательными. Gate сравнивает `result.Summary == expected` (без contains и без TrimSpace в самом acceptance). Diagnostic contains служит только boolean evidence и не разрешает success.
- `internal/node/opencode_live_diagnostics_test.go`: безопасный classifier полей `actual_current_present`, `original_first_present`, `profile_marker_present`, `literal_CURRENT`, `literal_FIRST`, `separator_count`, `SummaryBytes`; unit regression literal_CURRENT40 против correct65/correct98; regression definitions-before-format, запрета literals и сохранения read/marker/history requirements. Никакого mock/fake success для native/live gate.
- Этот ticket: сохранены оба FAIL, точные commands/outcomes и ограничения. Focused code diff для родителя: `/tmp/secretary31-parent-followup.patch` (два test files; отдельно от прежнего большого dirty tree).

**Новые проверки:**

```sh
go test -race -p 1 ./internal/node ./internal/acp -run '^Test(OpenCode(WorkerAnswerSafeDiagnostics|LiveWorkerPromptDefinesSubstitutionsBeforeFormats|ConfigurationRegistrationAndFailClosed|DeliveryMarker.*|InventoryUsesSettingsNotVariantNames|ModelConfirmationRequiresExactEcho|ResumeDrainsHistoryBeforeFreshResult|RuntimeWritesManagedConfigBeforeACPStart)|ExactEnvironmentDoesNotRestoreExcludedVariables|RequestDrainingEventsWaitsForFIFOConsumer)$' -count=1
GOOS=linux GOARCH=amd64 go test -c ./internal/node -o /tmp/secretary31-worker-fixed.test
scp -q /tmp/secretary31-worker-fixed.test omarchy:/tmp/secretary31-worker-fixed.test
ssh omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_LIVE_E2E=1 /tmp/secretary31-worker-fixed.test -test.run="^TestOpenCodeAuthenticatedWorkerReadAndResume$" -test.count=3 -test.v'
ssh omarchy 'PATH="$HOME/.local/bin:$PATH" SECRETARY_OPENCODE_ACP_E2E=1 TEST_OPENCODE_SHARED_DATA_HOME="$HOME/.local/share" /tmp/secretary31-worker-fixed.test -test.run="^TestOpenCode(NativeProfilePersistence|NativeInventory|ACPNativeHTTPFixture|SecretaryMCPNativeHTTPFixture)$" -test.count=2 -test.v'
go test ./...
go vet ./...
go build ./...
git diff --check
```

Все перечисленные новые checks **PASS**. Targeted race: node2.544s, acp1.259s. Три заранее запланированных независимых authenticated Worker lifecycles: **PASS 15.44s / 13.82s / 16.26s**. В каждом initial Result65 bytes, resumed Result98 bytes, `expected_content=true`, current/first/marker flags true, обоих literal flags false, separator_count1/2; проверяется тот же native ID после Close/Resume. Это три finite samples, не гарантия отсутствия любой будущей model variability. Acceptance не повторяет failed turn/test внутри себя до случайного PASS.

SHA256 нового fixed Linux artifact: `7aab973663cffe2cb8dab35e682e1ab28f4a56880453370b2539027541d074a9`. Native worker/secretary persistence, inventory и read/MCP fixtures в фактическом shared provider store — два независимых прогона PASS. Runtime gates, allow/deny policy, model/effort и native session/history не изменены focused fix. Authenticated Secretary MCP не вызывался платно заново без необходимости: parent PASS6.24s сохранён, native MCP fixture повторно PASS.

**Границы:** никакого deploy/commit, production restart/config edits, реальных Worker/Conversation mutations, DB/auth reset или credential copy. Ticket остаётся claimed. Explicit tool identity limitation и fixed500ms MCP barrier остаются review concerns, а не доказанным полным Telegram acceptance. Production rollout, human review и последующие tickets21/22/29/34 не выполнены.

### Независимый parent review после исправления

Родитель прочитал native исходники `acp/catalog.ts`, `acp/translate.ts`, `acp/tool.ts` версии2.0.22 и проверил adapter/config/permission/inventory/replay изменения. Bounded exact selection не переключается на builtin mode, marker подтверждается native config echo; live failure не скрыт и устранён только уточнением acceptance prompt, не ослаблением runtime или прав.

Повторный полный `go test -race -p 1 ./...` и `git diff --check` — PASS. Предшествующий независимый полный обычный suite, vet/build и CLI/deployment scripts — PASS; новые focused tests также проверены implementer. Родитель заново собрал artifact `/private/tmp/secretary31-parent-review-fixed.test`, SHA256 совпал с corrected implementer artifact `7aab973663cffe2cb8dab35e682e1ab28f4a56880453370b2539027541d074a9`; доставка в `/tmp/secretary31-parent-review-fixed.test` только для private tests. Независимый authenticated WorkerReadAndResume — PASS19.29s: exact65/98 bytes, actual read, hidden managed marker, исходная history и тот же native session ID. Независимые native fixtures/persistence обоих roles и inventory ранее PASS; authenticated Secretary MCP PASS6.24s.

Локальная runtime/default implementation принята для следующего шага21/22/29, но **ticket31 не resolved**: production switch, существующий fx Follow-up/restart и полный Telegram acceptance остаются gates. Native ACP не передаёт explicit tool identity, поэтому реальные tools работают, но named tool cards не доказаны; требуется отдельный проверенный metadata путь, а не guesses. Fixed500ms MCP startup — ограничение readiness, не гарантия любого медленного server. Production остаётся fx; никакого deploy/commit/restart/auth/DB reset родитель не выполнял.

### Итог сверки текущего состояния

Исправлены доступные локально расхождения документации:

- `docs/quickstart.md` теперь указывает точные defaults Secretary `openai/gpt-6.1-sol` / `xhigh` и новых Workers `openai/gpt-6-luna` / `xhigh`; model/reasoning pins по-прежнему требуют observed support и не подменяются.
- `docs/phase4-release-gate.md` больше не требует `fx` как default и не помечает OpenCode optional. Default OpenCode v2 — обязательный manual acceptance; `fx`, Claude Code и Codex остаются explicit adapters.
- `.scratch/phase-4/map.md` больше не показывает уже устранённый managed-mode Resume race как текущий blocker.

Текущие focused checks прошли:

```sh
go test -race -p 1 ./cmd/secretary-node ./cmd/secretaryd ./internal/config ./internal/core ./internal/node ./internal/acp -run '^(TestCleanInstallDefaultsSecretaryAndWorkersToOpenCodeV2|TestProductionWorkerPolicyKeepsModelAndReasoningSeparateFromSecretary|TestOpenCodeProbeAndRuntimeUseSameExplicitBinary|TestConfiguredNodeRuntimeDefaultsOpenCodeAndRetainsSelectableAdapters|TestEffectiveDispatchDefaultsYieldToProjectAndExplicitPreferences|TestWorkerDefaultModelMustBeObservedWithoutFallback|TestOpenCodeConfigurationRegistrationAndFailClosed|TestOpenCodeDeliveryMarker.*|TestOpenCodeInventoryUsesSettingsNotVariantNames|TestOpenCodeModelConfirmationRequiresExactEcho|TestOpenCodeResumeDrainsHistoryBeforeFreshResult|TestRuntimeRouterUsesImmutableHarnessBindingAndPins|TestRuntimeRouterUsesProfileHarness|TestSharedHarnessCompatibilitySuite|TestFXRuntimeInterruptAndContinue|TestACPLegacyFXWireShapeQueuedAndSecondAttempts|TestDaemonPairHeartbeatDrainReconnectReplayDeduplicateAndRevoke|TestExactEnvironmentDoesNotRestoreExcludedVariables)$' -count=1
go build ./...
git diff --check
```

Race-selected tests PASS для шести пакетов (`secretary-node`, `secretaryd`, `config`, `core`, `node`, `acp`); `go build ./...` и `git diff --check` PASS. Full suite этого batch повторно не запускался. Дополнительные public-contract regressions для default OpenCode без fallback, сохранённого fx binding при idempotent replay/reopen, fx runtime compatibility и Node reconnect прошли:

```sh
go test -race -p 1 ./internal/core ./internal/node -run '^(TestResolveAndCreateWorkerReplaysBeforeCanonicalReads|TestDispatchResolverDefaultsEmptyWorkerPolicyToOpenCode|TestDispatchResolverDoesNotSilentlyFallbackWhenOpenCodeIsUnavailable|TestRuntimeRouterUsesImmutableHarnessBindingAndPins|TestRuntimeRouterUsesProfileHarness|TestSharedHarnessCompatibilitySuite|TestFXRuntimeInterruptAndContinue|TestDaemonPairHeartbeatDrainReconnectReplayDeduplicateAndRevoke)$' -count=1
```

Эти локальные checks не заменяют записанные выше native/live acceptance и не доказывают production Telegram или реальный fx reconnect/Follow-up после restart.

**Оставшиеся блокеры, не закрываемые безопасным локальным diff:**

1. Новый default нельзя выкатывать на production по текущему поручению. Для acceptance нужны отдельное owner-approved изменение service/config на Secretary и Node, реальный новый Worker и проверка действующего fx Worker после restart/Follow-up; production сейчас остаётся fx и не менялся.
2. OpenCode v2.0.22 ACP не публикует explicit tool identity. Capability намеренно не объявляет normalized named tool call/result; title/kind догадками не дополняются. Чтобы закрыть named activity/Telegram card acceptance, нужен native upstream metadata support или отдельное owner-решение об альтернативном протоколе/UX.
3. Автоматическое отдельное persistent OpenCode data directory не реализовано в ticket31 и остаётся scope/prerequisite ticket33. Текущие native evidence не закрывают clean setup без подключения обычного OpenCode state.
4. Readiness Secretary MCP дополнительно опирается на bounded 500ms stabilization после регистрации ACP session; проверенные synthetic/native round-trips проходят, но этот таймаут не доказывает готовность произвольно медленного MCP. Не выдавать его за универсальный readiness guarantee; для такого обещания нужен наблюдаемый native readiness signal или отдельное утверждённое ограничение contract.
5. End-to-end Telegram default Worker → activity → terminal Result остаётся отдельным product acceptance gate; в ticket31 такой real path не запускался и не объявляется принятым. Проверка существующего live fx Worker с Follow-up/restart потребовала бы production mutation и не выполнялась по прямому запрету пользователя. Related acceptance15/21/22/29/33 не объявляются выполненными этим тикетом.

Поэтому `Status: claimed` сохранён: локальные defaults/assembly и доступные проверки выполнены, но оставшиеся gates требуют production authorization, ticket33 и решения по metadata/readiness contract. Commit/push/deploy не выполнялись.

### Production auth probe follow-up — 6 октября 2026

**Публичный RED:** default Node probe всё ещё вызывал `auth list` без флагов. `go test ./internal/node -run '^(TestDefaultProbeCommandsAreExplicitContracts|TestOpenCodeProbeRequiresStoredCredentialMetadata|TestOpenCodeProbeRejectsUnsupportedVersionBeforeReadiness)$' -count=1` упал на старом argv и stored-credential case. `./scripts/node-deployment-test.sh` отдельно воспроизвёл ложный Doctor failure: fake background-service auth probe завершался timeout, а `sex node doctor` вернул `FAIL: node doctor`.

**GREEN:** `DefaultOpenCodeProbeSpec` вызывает только `auth list --format json --standalone`. OpenCode readiness теперь требует exit 0 и строго корректный JSON-массив `{id,name,connections}`, где есть `connections[].type == "credential"`; вывод из stderr, текст, пустой список, malformed schema/JSON, failed command и OAuth-shaped connection не принимаются. Общая Go-проверка изолирует HOME/XDG/config, использует только выбранный data home и 10s timeout. `secretaryd` и `secretary-node` предоставляют safe auth-check modes, через которые `sex doctor` проверяет Secretary и Node store. Setup по-прежнему инициализирует DB через `serve --stdio` и не запускает auth/login. Native models/reasoning и ACP flow не менялись; FX/Claude/Codex coverage и immutable mappings не менялись.

**Публичные regressions:** stored credential ready; only-env/empty/malformed/non-credential/stderr/nonzero/unsupported-version not ready; process-boundary checks подтверждают выбранный XDG store, отсутствие ambient `OPENAI_API_KEY`, cleanup temp HOME и остановку timed-out subprocess. `sex-cli-test.sh` и `node-deployment-test.sh` проверяют Doctor, exact flags, shared selected path, personal-store canary и отсутствие ambient credentials.

**Native evidence:** Linux artifact собран из этого source tree и запускался на omarchy с OpenCode v2.0.22. `TestOpenCodeSelectedStoreAuthCatalogAndACPReadiness` в фактическом selected store: `status=ready`, `authenticated=true`, `model_count=28`, `reasoning_count=7`, model IDs `openai/gpt-6.1-sol` и `openai/gpt-6-luna`, `xhigh`; helper повторяет безопасную auth metadata проверку, затем Node inventory проходит native model API и ACP `initialize`. `session/new` и paid model call не запускались, auth JSON/account values не выводились. `TestOpenCodeNativeInventoryMissingAuthDoesNotFallback` на private `/tmp` store прошёл: отсутствующий credential не заимствуется из canary/personal data home. Native CLI help и фактический v2.0.22 подтвердили `--format json` и `--standalone`.

**Gate failures сохранены:** первый полный `phase4-release-gate.sh` завершился FAIL на Stage 3 в `TestOpenCodeConfigurationRegistrationAndFailClosed/missing-mode/resume=true`. У synthetic helper-test deadline был 300ms; он иногда истекал во время старта fixture process. Deadline увеличен до 1s, fail-closed deadline assertion сохранён; subtest прошёл `-count=5` и `-race -count=3`. Финальный полный gate после этого изменения прошёл stages 1–9: Go tests/race, vet/build, frontend/assets, public CLI/deployment/revoke и diff check — PASS. Gate logs сохранены в `/tmp/secretary-ticket31-auth-probe-fullgate.log` (initial FAIL) и `/tmp/secretary-ticket31-auth-probe-finalgate.log` (final PASS). Documentation/ledger обновлены после gate; отдельный `git diff --check` после docs — PASS.

**Auth evidence и pending gates:** Owner headless OpenAI OAuth в production shared store уже был завершён до этой read-only acceptance. Источник факта — handoff от 6 октября 2026 (`/private/tmp/secretary-auth-probe-task.md`); закрытая диагностика selected store — `/private/tmp/secretary-opencode-probe-diagnosis.md`. Native standalone JSON и subsequent `TestOpenCodeSelectedStoreAuthCatalogAndACPReadiness` подтвердили stored credential (`authenticated=true`). Во время этих read-only checks новый `auth login` не запускался: «login не выполнялся во время acceptance» не означает «auth отсутствует».

Production config/Profiles/services не менялись; production switch/restart, Worker execution, paid call, Telegram test и existing fx Worker mutation не выполнялись. Manual rollout, Telegram Result/Follow-up, existing-fx restart/Follow-up, tool identity limitation, ticket33 migration gates и independent review остаются открытыми. Статус ticket31 остаётся `claimed`; commit/merge не выполнялись.

### Scope note: Secretary addressed-reply-v1 completion

Новый opt-in Secretary completion опирается на существующий проверенный OpenCode v2 ACP adapter и не меняет harness defaults, model/reasoning pins, Node inventory, native store, provider auth или Worker runtime contract. Worker остаётся на final-text/status gate; legacy FX/unaddressed profiles не приобретают reply-only completion. Native terminal evidence внутренний и не расширяет публичный Result DTO. Private unpaid profile/permissions/Resume и synthetic MCP fixtures прошли в этой ветке; production rollout, live Telegram и worker migration из этого follow-up не запускались. Прежняя попытка Ticket29 full gate (`umask 0022 ./scripts/phase4-release-gate.sh`) не запускала скрипт и сама была NOT RUN. Корректный отдельный запуск `p4-29-addressed-reply-gate-20261006T090424Z` завершился exit 1 на Stage 3 (`TestOpenCodeConfigurationRegistrationAndFailClosed/missing-mode/resume=true`, safe protocol counts unavailable); stages 4–9 не запускались. После него устранена только test-fixture startup/counters race и error-variable shadowing; full matrix, exact `-count=5` и `-race -count=3` прошли. Финальный Ticket29 gate `p4-29-addressed-reply-finalgate-20261006T094440Z` завершился exit 0, stages 1–9 PASS; точная команда, private logfile и сохранённый промежуточный FAIL записаны в `docs/phase4-release-gate.md`. Дополнительный deduplicated review blocker по Ticket29 устранён: verified OpenCode v2 response.Summary больше не считается final evidence; assistant final требует parent chunks, reply-only путь — exact durable reply. Дублированная Start/Resume contract validation вынесена в общий validator. Private native fixtures и focused/race tests прошли; после одного сохранённого intermediate Stage3 FAIL финальный gate `p4-29-summary-evidence-corrected-finalgate-20261006T102309Z` exit0, stages1–9 PASS. Parent re-review обоих отчётов pending. Это не меняет результаты самостоятельных исторических Ticket31 gates и не закрывает их manual review/rollout.

### Managed Worker template envelope follow-up

В этой локальной uncommitted работе Worker сохраняет точный managed Profile в private `workers.profile_snapshot`: версия/имя, инструкции, skills, allow_tools, source hash и resolved Harness/model/reasoning/delivery binding. Snapshot не попадает в Worker JSON/DTO/events. Control/Core dispatch, Follow-up, Resume и idempotent replay используют сохранённый snapshot; текущая config source после binding не перечитывается. Missing/invalid/hash mismatch, Harness/model/reasoning mismatch и запрещённая Project execution capability приводят к fail-closed до runtime start. Прямой OpenCodeRuntime Profile guard не ослаблялся; только прежние FX Workers с durable pre-marker binding и пустым snapshot сохраняют legacy совместимость, включая FX-specific managed tool names.

`TestPhase4DispatchPassesAuthoritativeWorkerTemplateToRuntime` проходит public WorkerService → Core → NodeRuntime → OpenCodeRuntime → synthetic external ACP executable: процесс проверяет exact test instructions/allowlist/model/reasoning, скрытые profile metadata и deny-first shell policy; source v2 после Follow-up не заменяет frozen v1. Публичный Node Resume проходит через `session/load` с исходным synthetic native ID и без нового `session/new`. Отдельные public negative tests проверяют missing/unavailable source, malformed hash, model/policy mismatch и отсутствие созданного Worker; idempotent Core replay возвращает исходный snapshot. Synthetic executable — deterministic boundary fixture, не настоящий OpenCode/provider call.

Final release gate `p4-31-worker-profile-envelope-gate-20261007-local`: точная команда `umask 0022; ./scripts/phase4-release-gate.sh`, все 9 stages PASS, exit 0; приватный лог `/private/tmp/secretary-worker-profile-release-gate.log`. Независимый review, commit/merge, paid/live provider, Telegram, production change/restart и existing Worker mutation не выполнялись. Эти automated checks не закрывают manual/native/production acceptance; ticket31 остаётся `claimed`.

### Blocker fix follow-up — legacy FX binding и Worker template source

- **RED:** новый FX Worker без `WorkerProfileSource` прошёл прежнее creation path, а Node принимал FX envelope с пустым template. Публичные regressions `TestNewFXWorkerTemplateWithoutSourceRejectsBeforeBinding` и `TestPhase4NodeCommandFailsClosedForInvalidWorkerTemplate` теперь требуют отказ до создания Worker/runtime; partial template и чужой harness с legacy marker тоже отклоняются.
- **GREEN:** каждому новому Worker нужен authoritative source. Приватный additive `workers.worker_template_required` marker устанавливается новым Worker в `true`; старые строки и phase3 migration-created legacy bindings получают `false`. Только существующий FX Worker с этим durable marker, пустым snapshot и неизменённым FX Project/Harness binding может получить explicit Node legacy marker. Никакого обхода только по `harness == fx`, default template или переписывания старых bindings/history нет. Дублированные snapshot reads используют shared `loadWorkerTemplateSnapshot`.
- `TestExistingFXWorkerTemplateBindingSurvivesDispatchFollowUpAndResume` моделирует pre-marker SQLite schema и проверяет после reopen исходные Project snapshot, Node/HarnessInstance и FX policy для public WorkerService Dispatch, Follow-up и Resume; отдельный public `NodeRuntime.Dispatch` подтверждает отсутствие синтезированного template. Новый FX без source проверен на отсутствие Worker state. Test names используют доменный термин Worker template; glossary/CONTEXT не менялись.
- Первый gate после blocker fixes сохранён без переименования: `/private/tmp/secretary-worker-profile-final-gate.log`, exit 1 на Stage 3 из-за прежних synthetic FX fixtures в `internal/mcp` и `internal/webapi` без template source; stages 4–9 не запускались. В тесты добавлены synthetic sources. Повторный полный gate после последних source/tests: `umask 0022; ./scripts/phase4-release-gate.sh`, exit 0, все 9 stages PASS; log `/private/tmp/secretary-worker-profile-final-gate-rerun.log` (mode 0600). После него менялись только docs/ledger/report; final diff check PASS.
- Стоп до parent rerun обоих review axes. Ticket остаётся `claimed`. Native OpenCode/provider, production, Telegram, real FX acceptance, commit/merge и rollout не запускались.

### Latest rollout evidence — actual turn и MCP fixture

- Reviewed Worker Profile-envelope source fix присутствует в target `893`; это source-level acceptance evidence, не доказательство live Worker lifecycle.
- До единственного live General turn обе роли были проверены на target, health/auth-selected-store/catalog/model readiness прошли без повторного login. После него оба процесса остались healthy на FX defaults; прежние 30 FX bindings и 31 Node mappings не менялись. Telegram оставался paused на время owner rotation.
- **Functional FAIL, отдельно от operational health:** реальный Sol General turn завершился `succeeded`, но `spawn_worker=0`, `reply_to_user=0`, обычный reply=0; Worker/Attempt и Result не появились. Нет WorkerResult, Follow-up/Resume, дополнительной FX acceptance или Telegram acceptance.
- **Synthetic fixture PASS only:** isolated native OpenCode v2.0.22 fixture показал MCP startup/`initialize`/`tools/list`, девять tool schemas и `spawn_worker` + `reply_to_user` у localhost mock provider. Это не live acceptance и не исправление real provider/tool choice. В production turn `tools/list` не аудировался; startup availability и выбор инструмента провайдером остаются неразличимы.
- Закрытый parser последнего General input и выбранных versioned instructions: `mismatch=false`, `category=none`. Обязательного literal `create_worker` или tool args с неверной schema не найдено; фактическое имя — `spawn_worker`. Alias не добавлен.

Хронологию сохранять раздельно: (1) прежний mixed-runtime provenance был исправлен до live этапа; обе роли теперь на target; (2) General turn был operationally succeeded, но Worker dispatch функционально FAIL из-за нулевых tool calls; (3) остальные acceptance steps — NOT RUN; (4) прежний Pi operator завершился scopes mismatch, который не доказывает production auth failure и не имеет установленного источника. Не переименовывать эти outcomes в PASS/auth FAIL/provider fix.

Ticket31 остаётся `claimed` и acceptance-blocked; tickets15/22/29 не закрывать. Следующий bounded diagnostic — наблюдение фактических MCP process/startup/`tools/list` и addressed-reply пути с allowlisted counts/booleans и provider tool-schema/tool-call categories, без input, Profile text, arguments, payload, credentials или session IDs. Не повторять random live turns.

### 7 октября 2026 — Sol/high: mandatory reply и narrow MCP observer

На branch `p4/31-opencode-default-sol-7f3adc`, base `e1efb919f81b5edc4221e0febbe5f39b99ce0239`, выполнен parent-approved local scope, без commit/deploy. Кодовый субагент — Sol6.1/high; production pins остаются Secretary `openai/gpt-6.1-sol/xhigh`, новые Workers `openai/gpt-6-luna/xhigh`.

- Opt-in v1 assistant-final bypass воспроизведён public RED и закрыт atomic Core required-reply completion. Это намеренное ужесточение прежней v1 совместимости, не изменение legacy/Worker gate. Missing reply сохраняет безопасный failed code; provider/native failure не скрывается persisted reply. Не добавляется ordinary fallback/echo.
- `POST /v1/internal/secretary/mcp/observe` принимает только safe startup/initialize/list evidence через отдельную launch-scoped capability, которая не вызывает lifecycle tools. Реальный child сообщает initialize/list после stdio write/flush. Count/expected-tool booleans берутся из returned registry. Core связывает current generation и originating turn приватно; same-pins restart получает новое generation, stale/revoked observation отклоняется. Lazy discovery не получает deadlocking barrier; 500ms heuristic не выдана за readiness.
- Reload не обновляет canonical Profile snapshot до explicit native restart. Finished event показывает only safe committed completion/discovery DTO, без private links/native IDs/args/output/credentials/reasoning.
- Public Runtime/Core/stdin/HTTP negative/reopen/privacy tests и focused race PASS. Actual unpaid OpenCode2.0.22 с новым observer и настоящим built MCP binary PASS: 9 schemas, reply и одна entry; loopback provider/private HOME/native store/canary. Existing native Worker/Secretary profile/Follow-up/Resume fixtures PASS. Full gate `p4-31-sol-required-reply-observer-e1efb91-20261007`: один запуск, exit0, stages1–9 PASS; `/private/tmp/secretary-sol-release-gate.log` (0600). Commands/initial FAIL/final outcomes сохранены в release ledger и `/private/tmp/secretary-opencode-sol-implementation.md`.

Actual893 `succeeded` без ordinary entry полностью не объяснён и live не повторялся: unsuppressed verified final должен был сохранить entry. Новый observer даёт будущую bounded evidence о broker response, не о live provider schemas/tool choice. Production FX defaults/health, shared native store, auth, существующие Workers/Profiles/history не менялись. Independent review, production switch, real Worker/Follow-up/Resume, existing FX и Telegram acceptance pending; ticket остаётся `claimed`.

### 7 октября 2026 — исправления после Spec/Standards review

Parent передал Spec 1 blocker / Standards 0 blockers+2 nits. Все исходные23 working files совпадают с candidate по parent checksum; source reset не выполнялся. Public reviewer RED воспроизведён на текущих исходниках: canceled Stop → valid Stop retry оставлял старую session и блокировал explicit Start. `Runtime.Stop` теперь сохраняет repeatable cleanup, независимо от caller context выполняет bounded revoke и subprocess cleanup, возвращает исходные ошибки и блокирует Start при pending revoke/close. Public executable ACP/Store reopen tests подтверждают old subprocess exit, valid retry/restart, новую generation и отказ stale observation authority. Никакой error masking/revoke bypass.

Standards nits исправлены узко: один transaction discovery snapshot для проверки и finished payload; именованные phase type/constants/validator с неизменным wire и fail-unknown. Focused public/race и actual private unpaid OpenCode2.0.22 observer/Worker-Secretary Profile/Follow-up/Resume fixtures PASS. Один финальный gate этого fix cycle: `p4-31-sol-stop-revoke-review-fix-e1efb91-20261007`, `umask 0022; ./scripts/phase4-release-gate.sh`, exit0/stages1–9 PASS; `/private/tmp/secretary-sol-review-fix-release-gate.log` (0600). Полные команды, RED и предыдущие outcomes сохранены в `docs/phase4-release-gate.md`; после gate только docs/diffcheck.

Parent final rerun двух review axes pending; собственный/nested review не выполнялся. Production/defaults/pins/native state/auth/history/bindings не менялись; paid calls/provider login/commit/merge отсутствуют. Ticket `claimed`, actual893 functional FAIL и manual NOT RUN/Telegram pause остаются прежними.
