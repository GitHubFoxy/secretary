# Phase 4 release gate

Этот документ разделяет два разных доказательства:

1. **Deterministic automated checks** запускаются из корня репозитория одной командой. Они используют тестовые doubles и локальные integration tests, поэтому не доказывают работу настоящих аккаунтов harness.
2. **Manual real-harness proof** включает default OpenCode v2 Secretary/Worker и explicit `fx`, Claude Code и Codex coverage. Его нельзя заменить deterministic fixtures.

Нельзя помечать ручной пункт как пройденный по результату Go-теста, `--help`, фикстуре или успешному запуску CLI. В evidence ledger должна быть команда или скриншот, время, commit и идентификатор запуска. Секреты, pairing tokens и содержимое `user.md` в ledger не записываются.

## Актуальный статус локального batch

Этот свод не заменяет датированные результаты ниже и не закрывает ручную acceptance:

- **31**: локальная реализация OpenCode v2 и private native checks подготовлены; default обязателен. Production/full Telegram acceptance не выполнялась.
- **21**: локальная правка progress/Result presentation подготовлена; flaky test приведён к фактической гарантии: watcher FIFO не задаёт порядок чтения Activity/Result каналов. Публичный regression проверяет eventual progress availability, final deltas и turn boundary; -count=20 и race пяти пакетов -count=2 прошли. Последний полный gate `p4-ticket15-domain-drain-459709c-20261005T105601Z` завершился FAIL на Node process reconnect timeout; isolated exact rerun прошёл. Это не real Telegram acceptance.
- **22**: owner одобрил полный inline Result с semantic/grapheme splitting. Локальная реализация и HTTP/adapter acceptance tests есть; реальный Telegram flow не запускался.
- **29**: owner одобрил opt-in addressed-reply v1. Реализованы public Core/MCP reply/origin links и durable authenticated `respond_worker` receipts, точно связанные с Node/command/Turn/Attempt; uncertainty и terminal Turn правила сохранены. Approval intent durable до handoff; explicit retry использует saved decision и прежние identities. В `approvalRoute` отказ второй auth-проверки теперь завершает HTTP request без mutation, owner fallback или второго body; write-only Client responses идут через strict DTO, read scopes не расширены. Owner observer refresh показывает только public Approval ID/state/resolution_state, скрывая saved response/actor/command ID. RED public HTTP regression синхронно отзывает Client через public revoke route между двумя auth checks и подтверждает 401, отсутствие mutation/owner DTO; второй тест проверяет отказ неправильного scope. Refresh regression покрывает saved approve и deny, а UI contract — реальный worker refresh path и retry по `approval.id`. Combined gate после auth/observer fixes прошёл в `p4-ticket15-approval-auth-refresh-459709c-20261005T102708Z`. Более поздний final gate после ACP test correction записан как FAIL ниже: Node process reconnect timeout. Real Telegram General → Worker Topic → следующий user turn остаётся `NOT RUN`; historical `Node is offline` не объявлен гарантированно устранённым.
- **33**: одна выделенная native DB используется Secretary и co-located Node, а remote Node — своей. Strict Go decoder отклоняет duplicate keys, invalid UTF-8, unpaired UTF-16 surrogates и malformed deployment/manifests; raw valid Unicode и paired escapes сохраняют canonical path. Public RED показал successful wrongpath `node-�/data/opencode-native` для raw `0xFF`; GREEN public config/manifest matrix, CLI fail-closed и native OpenCode v2.0.22 unpaid setup/concurrent/Resume прошли. Неблокирующий Middle Man wrapper без production callers удалён узко. Предыдущие FAIL/logs сохранены; полный gate после UTF-8 fix под umask 0022 прошёл все 9 stages (6 Oct 2026, exit 0; отдельный logfile не сохранялся). Owner login, authenticated inventory/Worker run, production rollout и следующий независимый review не выполнялись.
- **30**: safe failure metadata локально реализована, исследование resolved; исторический incident не содержит safe category/code/status, поэтому его причина остаётся неизвестной.
- **34**: подготовительный Codex прогон заблокирован tickets 31/21/22/29; на целевом Node отсутствуют exact pins `openai/gpt-6.1-sol` и `openai/gpt-6-luna`. Не подменять модели, не выполнять login без owner.

Поэтому Ticket 15 остаётся незавершённым, пока каждая обязательная строка настоящей матрицы не подтверждена допустимым real evidence. Сохраняйте `FAIL`, `BLOCKED`, `UNAVAILABLE` и `NOT RUN` как наблюдавшиеся состояния; не исправляйте ожидания или fixture, чтобы получить `PASS`.

## Ticket31: private OpenCode acceptance, 4 октября 2026

Локальная uncommitted реализация на `phase4-implementation`, review и production rollout ещё не выполнены. Native binary на omarchy: `~/.local/bin/opencode`, v2.0.22. Tests выполнялись Linux acceptance binary в `/tmp/secretary-ticket31-acceptance/`, с фактическим provider store, без изменения production Worker/Conversation, services, config или auth.

- `PASS`: `TestOpenCodeNativeProfilePersistence`, Worker и Secretary, два прогона: managed system и deny-first catalog на Start, same-process Follow-up и fresh-process Resume того же ID. Настоящие read/MCP results доходят до synthetic provider; запрещённый shell не исполняется, прежняя history сохраняется.
- `PASS`: `TestOpenCodeNativeInventory`, два прогона: production HarnessDiscovery сообщает обе выбранные модели и `xhigh` из native variant settings; 28 enabled models, 7 reasoning levels.
- `PASS`: opt-in `TestOpenCodeAuthenticatedSecretaryMCP` — `openai/gpt-6.1-sol` / `xhigh`, scoped MCP environment, tools/list и tools/call, terminal OK.
- `PASS`: opt-in `TestOpenCodeAuthenticatedWorkerReadAndResume` — `openai/gpt-6-luna` / `xhigh`, два настоящих чтения изменяемого random nonce file, hidden system marker и первая прочитанная строка после Resume. ID не заменялся.
- `LIMITATION`: ACP v2.0.22 не передаёт explicit tool identity; normalized tool cards не подтверждены и не объявляются. Title/kind не используются для угадывания имени.
- `PASS local`: ticket33 реализует отдельные stable Secretary/Node native stores, exact-store runtime/inventory/Doctor/login paths и visible legacy migration gate; private unpaid native HTTP fixtures и CLI regressions проходят.
- `PENDING`: owner login и authenticated native inventory/Worker acceptance в новых stores; production setup/restart; owner-approved migration decision для старых managed sessions; real existing fx Follow-up/restart и полный Telegram flow. Presentation tickets21/22/29 остаются отдельной работой. Ticket34 не реализован.

Точные команды ticket31 находятся в его comments; private fixtures ticket33 и прежние authenticated/live evidence не закрывают полную acceptance matrix.

### Ticket31: immutable Worker template envelope follow-up, 7 октября 2026

Отдельное локальное изменение в uncommitted worktree `p4/31-worker-profile-envelope-7724b5`, база `8b58ecc12c5c80a2bbb04245841c6fe1fffffb1e`:

- Новый Worker получает один managed Profile snapshot при binding: exact config version/instructions/skills/allow_tools/source hash плюс resolved Harness/model/reasoning/delivery и проверяемый snapshot hash. Snapshot сохраняется в private `workers.profile_snapshot`, исключён из Worker JSON/DTO/events и возвращается из durable idempotency replay.
- Control/Core → Node command → OpenCodeRuntime test исполняет synthetic ACP-compatible external process. Он проверяет сгенерированный native config (точные synthetic instructions, worker identity, selected model/reasoning и deny-first/allowlisted permissions) по фактической границе процесса, не печатая Profile text/hash/session ID. После изменения текущего source Follow-up и Resume продолжают использовать сохранённый v1 Profile; Resume вызывает `session/load` исходного synthetic native ID, а не создаёт новую native session.
- Negative public tests: отсутствующий/недоступный Worker template source и Project permission mismatch не создают Worker; Node отвергает missing, invalid hash, model mismatch и запрещённые permissions до runtime start. OpenCode direct Start guard сохранён. Новый FX Worker без source fail-closed до создания binding. Пустой template разрешён только у уже существующего legacy FX Worker с durable pre-marker binding; additive `worker_template_required` marker сохраняет старый binding, новый Worker получает required=true, общего FX bypass нет. Legacy FX regressions сохраняют исходную HarnessInstance при Dispatch, Follow-up и Resume; managed FX template сохраняет собственные tool names.
- `go test -p 1 -count=1 ./...` прошёл; финальный release gate ниже включает полный Go race suite уже с итоговыми Resume/DTO assertions.
- Сохранённый первый blocker-fix gate `/private/tmp/secretary-worker-profile-final-gate.log`: exit 1 на Stage 3 из-за прежних synthetic FX fixtures в `internal/mcp` и `internal/webapi`, где не был задан обязательный template source; stages 4–9 не запускались. Fixtures получили только synthetic sources, assertions не ослаблялись.
- Финальный gate после последних source/test изменений: `umask 0022; ./scripts/phase4-release-gate.sh`, exit 0; все 9 stages PASS — release-gate contract, gofmt, полный Go suite, полный Go race suite, vet/build, frontend tests (10/10), production/embedded assets, CLI/deployment/revoke tests и `git diff --check`. Лог `/private/tmp/secretary-worker-profile-final-gate-rerun.log` (mode 0600). После gate менялись только docs/ledger/report; финальный `git diff --check` прошёл. Более ранний gate `/private/tmp/secretary-worker-profile-release-gate.log` сохранён отдельно.
- Это deterministic synthetic executable evidence, а не настоящий provider/model, оплачиваемый call, production, Telegram или real FX/OpenCode acceptance. Эти сценарии не запускались; independent review, commit, merge и rollout также не выполнялись. Ticket31 и общая manual acceptance остаются `claimed`/pending.

## Ticket33: локальная изоляция native store

Этот исторический прогон фиксирует прежний дизайн с раздельными Secretary/Node stores и `base_commit 459709c`; он superseded и не описывает реализацию в текущей ветке Ticket 33.

Текущий контракт: одна private native DB для Secretary и co-located Node; remote Node владеет собственным локальным store. Старый `config.toml`/`secretary.db` и старый Node config/state без selection record остаются на прежнем `XDG_DATA_HOME`. Команда `sex opencode select-shared-store` выполняет явный owner transition без переноса auth/history/mappings и отказывается при managed OpenCode session mappings, конфликтующих выборах, частичной записи или непустом target. Fx selection, существующие mappings и runtime session IDs не меняются.

- `PASS local`: public `scripts/sex-cli-test.sh` проверяет единый initialization/login path, повторный setup/restart, Doctor, legacy fx continuity, explicit/idempotent owner selection, fail-closed managed-session/conflict/partial/symlink случаи, личный-store canary и private store автономного remote Node.
- `PASS local`: новый реальный native unpaid HTTP fixture покрывает последовательную работу Secretary/Worker в одном DB и одновременные ACP sessions с role-specific Profiles, MCP/tools, models/reasoning и history. Отдельно проверен native missing-auth без fallback.
- `NOT RUN`: owner provider login в новом store, authenticated inventory/Worker gate и production install/restart. Fixture не использовал credentials и не подтверждает доступ к платному provider.

### Исторические private native fixtures до выбора общего store, 5 октября 2026

- `run_id`: `p4-ticket15-native33-20261005T051548Z`
- `PASS`: на omarchy/OpenCode v2.0.22 исполнены `TestOpenCodeNativeProfilePersistence` (Worker и Secretary: Start, Follow-up, fresh-process Resume того же ID/history), `TestOpenCodeSecretaryMCPNativeHTTPFixture` и `TestOpenCodeNativeInventoryMissingAuthDoesNotFallback`.
- `PASS`: модель/provider endpoints и MCP были local synthetic HTTP fixtures; файлы и stores созданы только в `t.TempDir`, личный native store оставался canary и не подключался. Provider login не выполнялся.
- `command`: Linux amd64 test binary собран из текущего working tree, временно передан в `/tmp` omarchy и запущен с `SECRETARY_OPENCODE_ACP_E2E=1` и regexp `^TestOpenCode(NativeProfilePersistence|SecretaryMCPNativeHTTPFixture|NativeInventoryMissingAuthDoesNotFallback)$`; временный binary удалён после прогона.
- `NOT RUN`: authenticated inventory/Worker acceptance требует owner-provisioned login; production setup/restart, service/config changes и legacy migration approval не выполнялись.

### Shared native-store unpaid fixtures, 5 октября 2026

- `run_id`: `p4-33-shared-native-20261005-local`; branch `p4/33-shared-opencode-store-b205fa`, base `5fcde24f95551d18ffb3a3832f567530f809e226`, изменения uncommitted.
- `environment`: macOS, настоящий `opencode v2.0.22`, `SECRETARY_OPENCODE_ACP_E2E=1`; все test stores/workspaces были в приватных `t.TempDir`. Provider и MCP endpoints были local synthetic, credentials/login не использовались.
- `command`: `MISE_TRUSTED_CONFIG_PATHS="$PWD/.mise.toml" SECRETARY_OPENCODE_ACP_E2E=1 mise exec -- go test ./internal/node -run '^TestOpenCode(NativeProfilePersistence|NativeSharedStoreConcurrentSecretaryAndWorker|NativeInventoryMissingAuthDoesNotFallback)$' -count=1 -v`.
- `PASS`: Worker и Secretary последовательно использовали один native data home; каждый выполнил Start, same-process Follow-up и fresh-process Resume с тем же session ID/history. Role-specific system Profile, model, reasoning, MCP/tool scope и history не смешались.
- `HISTORICAL, superseded`: первоначальный concurrent fixture делал предварительный `Runtime.Start` для создания DB, поэтому не доказывал поведение public setup. Исправленный native run с public setup описан ниже; там `Runtime.Start` initializer удалён.
- `PASS`: native missing-auth inventory вернул явный unavailable status без fallback к personal store.
- `NOTE`: первая попытка с role-specific model остановилась на test wrapper, который изменял только literal `fixture-model`; wrapper расширен на все configured fixture models. Первая конкурентная попытка стартовала на пустой DB; последующая подготовка через `Runtime.Start` была признана недостаточной reviewer. Текущий fixture теперь использует native DB из настоящего public `sex setup`, см. ledger ниже. Raw ACP, prompts, tool payloads и reasoning в лог/evidence не выводились.
- `NOT RUN`: owner provider login, authenticated inventory/Worker gate, production setup/restart и legacy migration approval.

### Ticket 33 review blocker fixes, 5 октября 2026 (последующие Spec findings)

- `run_id`: `p4-33-duplicate-key-fullgate-20261005T170947Z`; ветка `p4/33-shared-opencode-store-b205fa`, база `5fcde24f95551d18ffb3a3832f567530f809e226`; изменения остаются незакоммиченными.
- **Blocker 1 RED:** реальный `sex setup` в пустом private HOME с OpenCode v2.0.22 вернул exit 0 без `opencode.db`; personal canary не изменился. **Fix/GREEN:** setup вызывает `opencode serve --port 0 --stdio` с EOF на stdin, проверяет DB и aborts с явной ошибкой при отказе/отсутствии файла. CLI fake `auth list` больше не создаёт DB. `sex-cli-test.sh` проверяет как успех, так и видимый native init failure.
- **Blocker 2 RED:** public Secretary login выполнил fake auth для manifest `mode=shared`, указывавшего на personal XDG path с basename `opencode-native`; Doctor также запустил fake `auth list`. **Fix/GREEN:** единый Go JSON selection parser и typed exclusive mode валидируют exact canonical path, malformed modes, symlinks и role conflict; Doctor/login/owner CLI выполняют Go preflight до native CLI. Публичные CLI cases проверяют личный canary и отсутствие fake CLI calls.
- **Blocker 3 RED:** public `secretary-node --config` с `standalone=true` и shared manifest проходил selection и доходил до ошибки pairing. **Fix/GREEN:** Node runtime и Doctor/login применяют `DeploymentConfig.Standalone` до identity/auth; shared Secretary store отвергается до native CLI. Selection и DB canary сохраняются.
- **PASS focused:** `go test -count=1 ./internal/node ./cmd/secretaryd ./cmd/secretary-node`, `./scripts/sex-cli-test.sh`, `./scripts/node-deployment-test.sh`.
- **PASS native unpaid:** `MISE_TRUSTED_CONFIG_PATHS="$PWD/.mise.toml" SECRETARY_OPENCODE_ACP_E2E=1 mise exec -- go test -count=1 -v -run '^(TestOpenCodeNativeSetupCreatesDatabaseWithoutAuth|TestOpenCodeNativeSharedStoreConcurrentSecretaryAndWorker|TestOpenCodeNativeProfilePersistence)$' ./internal/node`. Настоящий v2.0.22 прошёл public setup без auth/provider call, concurrent Secretary/Worker на созданной этим setup DB и sequential Start/Follow-up/fresh-process Resume с прежним ID/history. HTTP provider/MCP — локальные unpaid fixtures, stores/workspaces — private temp directories; personal-store canary unchanged. Логи содержат только safe metadata.
- **FAIL, сохранённый initial attempt:** `p4-33-reviewfix-fullgate-20261005T162922Z` остановился на `[2/9] gofmt`, потому что `mise` отклонил недоверенный `.mise.toml`. Stage 1 прошёл; stages 2–9 не запускались. Лог: `/private/tmp/p4-33-reviewfix-fullgate-20261005T162922Z.log`. Этот FAIL не переобозначается как PASS.
- **PASS, corrected full gate:** `umask 0022 ./scripts/phase4-release-gate.sh`, run `p4-33-reviewfix-fullgate-trusted-20261005T163336Z`, exit 0. Прошли все девять stages: contract, gofmt, `go test -p 1 ./...`, `go test -race -p 1 ./...`, vet/build, frontend tests (10/10), production frontend/embedded assets, CLI/deployment/revoke tests, `git diff --check`. Лог: `/private/tmp/p4-33-reviewfix-fullgate-trusted-20261005T163336Z.log`.
- **Blocker 4 RED:** public CLI regression с первым personal и последним canonical `data_home` прошла Go-проверку с last-wins, затем fake Secretary login был запущен на первом (personal) пути из shell `awk`. Использовались только изолированные временные HOME и fake CLI; реальный personal store не открывался. **Fix/GREEN:** общий Go parser отклоняет дубликаты верхнеуровневых ключей, в том числе эквивалентные escaped keys, malformed/trailing/unknown JSON и control characters; valid escaped canonical path декодируется единожды. Go selector печатает проверенные typed mode/path записи для Secretary+local Node и Node, а `sex` больше не читает selection manifests и не использует eval/fallback. Public regressions покрывают setup, обе Doctor/login ветви, owner transition, malformed/escaped values, canonical path со slash escapes и пробелами, canary и отсутствие native CLI при отказе. Неблокирующее duplicate-code замечание закрыто в той же правке.
- **Диагностированный focused FAIL:** первый `node-revoke-test.sh` остановился, потому что общий `ensure_node_config` вызывал OpenCode selector и требовал Node binary в фикстуре, не связанной с OpenCode. Убрана store selection из generic control/revoke setup; native validation осталась в OpenCode setup/runtime/Doctor/login. Фикстура deployment config также переведена с буквального `$HOME` на абсолютный `data_dir`. Повторный `node-revoke-test.sh` прошёл; внешний curl использовался только как fake.
- **PASS focused:** `go test -count=1 ./internal/node ./cmd/secretaryd ./cmd/secretary-node`, `./scripts/sex-cli-test.sh`, `./scripts/node-deployment-test.sh`, `./scripts/node-revoke-test.sh`.
- **PASS native unpaid:** `MISE_TRUSTED_CONFIG_PATHS="$PWD/.mise.toml" SECRETARY_OPENCODE_ACP_E2E=1 mise exec -- go test -count=1 -v -run '^(TestOpenCodeNativeSetupCreatesDatabaseWithoutAuth|TestOpenCodeNativeSharedStoreConcurrentSecretaryAndWorker|TestOpenCodeNativeProfilePersistence)$' ./internal/node`. OpenCode v2.0.22 подтвердил public setup, concurrent Secretary/Worker и Start/Follow-up/fresh-process Resume в той же DB; локальные unpaid HTTP fixtures, private temporary stores/workspaces, canary неизменен, только safe metadata в логах.
- **FAIL, сохранённый initial attempt:** `p4-33-reviewfix-fullgate-20261005T162922Z` остановился на `[2/9] gofmt`, потому что `mise` отклонил недоверенный `.mise.toml`. Stage 1 прошёл; stages 2–9 не запускались. Лог: `/private/tmp/p4-33-reviewfix-fullgate-20261005T162922Z.log`. Этот FAIL не переобозначается как PASS.
- **PASS, corrected pre-review gate:** `p4-33-reviewfix-fullgate-trusted-20261005T163336Z`, `umask 0022`, stages 1–9, exit 0; лог `/private/tmp/p4-33-reviewfix-fullgate-trusted-20261005T163336Z.log`.
- **PASS, latest code full gate:** `umask 0022 ./scripts/phase4-release-gate.sh`, run `p4-33-duplicate-key-fullgate-20261005T170947Z`, exit 0. Contract, gofmt, all Go tests/race, vet/build, frontend tests (10/10), production frontend/embedded assets, sex CLI, Node deployment/revoke и diff check прошли. Лог: `/private/tmp/p4-33-duplicate-key-fullgate-20261005T170947Z.log`.
- **PASS focused follow-up:** после gate добавлены assertions для co-located и standalone `sex node setup` при duplicate keys; повторный `./scripts/sex-cli-test.sh` прошёл. Между full gate и этой проверкой product code не менялся.

### Follow-up по duplicate deployment `data_dir` и co-located pair — 6 октября 2026

- **RED:** public Node login fixture с первым external и последним canonical `data_dir`, legacy Secretary selection и shared Node selection до fix завершился `FAIL: Node login bypassed the local Secretary pair check through duplicate data_dir`. Команда вернулась успешно и вызвала только fake `auth login`; Go last-wins видел local path, shell first-wins пропускал pair validation. Canary/mappings были в private temp HOME; личный store не открывался. Первая версия fixture содержала noncanonical manifest path с `//` и корректно отклонялась раньше; это записано как fixture diagnostic, не как RED.
- **GREEN:** deployment config и store manifests разбираются общим Go strict decoder’ом: duplicate decoded fields, escaped и case aliases, unknown/malformed/trailing JSON и controls отклоняются до native CLI. Selector records дают shell trusted mode/home/data_dir/standalone; Node record также даёт `include_opencode`. `sex` не разбирает эти поля через awk/eval/fallback. Co-located Node login/Doctor/setup сравнивают selection с Secretary; standalone Node работает независимо, включая отдельный configured data directory. Owner transition дополнительно проверяет deployment config и ambiguous managed-session mappings.
- **PASS public matrix:** `sex-cli-test.sh` покрывает Secretary/Node setup, Doctor/login, owner, duplicate-data_dir RED→GREEN, escaped/case aliases, malformed configs, ordinary pair conflict, standalone independence, личный canary и отсутствие CLI invoke при отказе. Focused `go test -count=1 ./internal/node ./cmd/secretaryd ./cmd/secretary-node`, `./scripts/sex-cli-test.sh`, `./scripts/node-deployment-test.sh`, `./scripts/node-revoke-test.sh`, `git diff --check` прошли. Промежуточный неверный duplicate-mapping test fixture, parser assertion и fake PATH fixture исправлены; их диагностика записана в issue33 и implementation report.
- **PASS native unpaid на финальном code tree:** с настоящим OpenCode v2.0.22 прошли public setup без login, concurrent Secretary/Worker на созданной DB и Start/Follow-up/fresh-process Resume прежнего ID/history. Использовались private temp stores/workspaces и local unpaid provider/MCP fixtures; canary не изменился, в логах только safe metadata.
- **PASS latest full gate:** `p4-33-config-alias-fullgate-20261005T182746Z`, команда `MISE_TRUSTED_CONFIG_PATHS="$PWD/.mise.toml" MISE_YES=1 ./scripts/phase4-release-gate.sh` при `umask 0022`, exit 0. Все 9 stages PASS: contract, gofmt, Go tests/race, vet/build, frontend 10/10, production/embedded assets, CLI/deployment/revoke, diff check. Лог: `/private/tmp/p4-33-config-alias-fullgate-20261005T182746Z.log`. Full-gate FAIL `p4-33-reviewfix-fullgate-20261005T162922Z` и более ранние FAIL остаются отдельно сохранёнными.
- После этого gate менялись только documentation/ledger files; code/tests не трогались. Owner login, authenticated inventory/Worker acceptance, production setup/restart и migration approval остаются pending. Независимый повторный review не запускался; ticket остаётся `claimed`.

### Ticket 33 review follow-up: invalid Unicode — 6 октября 2026

- **RED из spec re-review:** private public selector получил raw `0xFF` в deployment `data_dir`; до fix он вернул success для `node-�/data/opencode-native` и создал selection manifest по replacement path. Личный store не открывался; auth/native CLI не запускались. Полный результат сохранён в issue 33 и implementation report.
- **GREEN:** общий JSON pre-scan проверяет `utf8.Valid` до `json.Decoder` для deployment config, store manifests и duplicate-key state JSON. Малый escape scan отклоняет unpaired high/low surrogates. Public tests проверяют отсутствие selection/store/native CLI при malformed config/manifest и сохранность raw Unicode/paired escapes.
- **Focused PASS:** Go (`internal/node`, `cmd/secretaryd`, `cmd/secretary-node`), `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`; повторён native unpaid setup/concurrent/resume на OpenCode v2.0.22 без provider login.
- **Сохранённый тестовый FAIL:** первая Unicode acceptance попытка упала на missing parent directory в fixture; fixture исправлен, после чего полный CLI suite прошёл. Это не product regression.
- **Единственный полный gate после code/tests:** ledger label `p4-33-invalid-utf8-fullgate-20261006` (его задал ledger: gate script run ID не печатает); uncommitted worktree на базе `5fcde24f95551d18ffb3a3832f567530f809e226`. При `umask 0022` команда `MISE_TRUSTED_CONFIG_PATHS="$PWD/.mise.toml" MISE_YES=1 ./scripts/phase4-release-gate.sh` завершилась exit 0; все 9 stages PASS, включая Go race, frontend 10/10, production assets и CLI/deployment/revoke. Вывод stages 8–9 был около 02:12 local 6 Oct; точное время завершения не записано, отдельный logfile не сохранялся. Ранее записанные full-gate FAIL и логи остались без изменений.
- После gate изменяется только ledger; дальнейшая независимая review ещё не запускалась. Production/authenticated acceptance остаётся pending.

## Latest partial real-harness run

- `run_id`: `p4-real-local-20260913`
- `commit`: `c4e4c6e`
- `PASS`: чистый локальный server setup/start/restart; настоящий `fx` выполнил Secretary request и вернул ожидаемый ответ.
- `PASS`: два локальных outbound Node подключались и сообщили inventory; Project получил разные path mappings.
- `BLOCKED`: это был один компьютер, без отдельного MacBook и home server; Telegram не был настроен.
- `BLOCKED`: Claude Code отсутствует, Codex в clean Node HOME не аутентифицирован, OpenCode не установлен.
- `FAIL`: реальная попытка создать fx Worker достигла runtime boundary, но завершилась `worker: runtime command delivery is unavailable`; Worker и файлы не были созданы или изменены.
- `NOT RUN`: approval, queue, replay, revoke и network-loss сценарии.

Эта запись фиксирует фактический прогон, но не заменяет полную acceptance matrix ниже. Полный redacted ledger хранится в каталоге запуска и не содержит credentials или raw prompts.

## Latest Telegram pairing and inbound run

- `run_id`: `p4-telegram-inbound-real-20260914`
- `commit`: `494dfcc`
- `command`: `zsh /tmp/p4-telegram-real.sh` в изолированной data directory
- `observed_at_utc`: `2026-09-14T09:30:29Z`–`2026-09-14T10:04:26Z`
- `PASS` для partial flow: Telegram Bot API ответил успешно; одноразовый pairing deep link был redeemed, pairing code удалён, update обработан.
- `PASS` для inbound boundary: после исправления отдельного Telegram server credential API сохранил два `message.saved` и два inbound message без Client credential; второй процесс не создавался.
- `BLOCKED`: изолированный server был поднят без Execution Node, поэтому Secretary не вернул terminal reply и Worker Topic не создавался. General chat, Topics, Worker activity и terminal Result не считаются закрытыми.

Evidence оставлено только во временном redacted run directory. Секреты, pairing code, cookies и runtime/session IDs в документацию не записывались.

## Latest Telegram run with an Execution Node

- `run_id`: `p4-telegram-node-real-20260914-general-chat`
- `commit`: `fa20a0d`
- `command`: `/tmp/p4-telegram-node-real.sh` в изолированной data directory; Node сообщил ready authenticated `fx` inventory.
- `observed_at_utc`: `2026-09-14T14:41:23Z`–`2026-09-14T14:42:41Z`
- `PASS`: одноразовый Telegram pairing был redeemed; inbound General chat message был сохранён в server-owned Personal Conversation, durable Secretary turn стал `succeeded`, а настоящий локальный `fx` ACP вернул terminal response.
- `PASS`: Secretary entry `TELEGRAMOK` был persisted и доставлен в Telegram General chat. UI screenshot и redacted ACP envelope trace сохранены только во временном isolated run directory.
- `NOT RUN`: Worker Topic, Worker activity и terminal Worker Result. Этот flow не создавал Worker.
- `NOT RUN` для real replay/process-count: harness не выполнял отдельный replay и не считал процессы.
- `PASS` для authorization boundary и replay dedup получен отдельно в deterministic `internal/webapi/telegram_test.go`: scoped principal `telegram-adapter` имеет только `conversation:write` и `worker:write`, `/v1/workers` получает `403`, повтор inbound даёт `duplicate=true`. Это не real-harness evidence.

Секреты, pairing code, cookies, chat/thread IDs и runtime/session IDs в документацию не записывались.

## Latest follow-up real-harness run

- `run_id`: `p4-mcp-main.aSTI7d`
- `commit`: `c88d5f1`
- `PASS`: чистый server/node setup; настоящий `fx` Secretary создал Worker через server-owned MCP proxy.
- `PASS`: Worker на `node/fx` выполнил read-only `pwd`, получил terminal success и вернул ожидаемый workspace path.
- `BLOCKED`: прогон выполнен на одном компьютере; отдельные MacBook/home-server, Telegram, Claude Code и Codex не подтверждены.
- `NOT RUN`: approval, queue, multi-Attempt, replay, revoke и network-loss сценарии.

Эта запись подтверждает production MCP routing, но не закрывает полную acceptance matrix ниже.

## Latest two-node real-harness run

- `run_id`: `p4-real-tailscale.XOzeoY`
- `commit`: `977e7ef`
- `PASS`: локальный MacBook Node и отдельный Tailscale home-server Node подключились к чистому Secretary server.
- `PASS`: оба Node сообщили inventory и Project mappings; настоящий `fx` Worker на MacBook выполнил read-only `pwd` и вернул terminal success.
- `BLOCKED`: на home-server отсутствуют авторизованные `fx`, Claude Code и Codex; Telegram не настроен.
- `NOT RUN`: approval, queue, multi-Attempt, replay, revoke и network-loss сценарии.

Эта запись расширяет evidence, но не закрывает полную acceptance matrix ниже.

## Real Codex ACP run

- `run_id`: `p4-real-tailscale.XOzeoY-codex`
- `commit`: `ccd3fbe`
- `PASS`: отдельный Tailscale home-server Node сообщил `home-server/codex` как `ready` с авторизацией ChatGPT после исправления non-TTY Codex probe.
- `PASS`: upstream `@agentclientprotocol/codex-acp@1.10.0` запустил настоящий Codex CLI `0.135.0`; Worker с явным `home-server/codex` выполнил read-only `pwd` и вернул `/tmp/secretary-node-p4-workspace`.
- `PASS`: явный Codex binding не переключился на `fx`; ошибки неподдерживаемых моделей также вернулись видимым terminal Result.
- `BLOCKED`: Claude Code и Telegram по-прежнему недоступны для real acceptance.

Эта запись расширяет evidence, но не закрывает полную acceptance matrix ниже.

## Real Codex Approval attempt

- `run_id`: `p4-real-approval`
- `commit`: `eb53065`
- `NOT RUN`: попытка попросила настоящий Codex создать файл внутри workspace. В upstream `codex-acp@1.10.0` режим `read-only` использует `approval=on-request` вместе с `workspace-write`, поэтому такая запись разрешена и не является permission stimulus.
- `NOT RUN`: внешний путь вне workspace, Client B response и `needs_input`, потому что корректный permission event не был запрошен.
- Этот run не считается Approval evidence и не заменяет real flow deterministic double.

Эта запись расширяет evidence, но не закрывает полную acceptance matrix ниже.

## Latest isolated local real-harness run

- `run_id`: `p4-real-local-20260914`
- `commit`: `f88460d`
- `PASS`: изолированный Secretary server и outbound Node `local-real` подняты на чистом временном data directory; web session и Node pairing прошли.
- `PASS`: Node сообщил реальные `fx` и Codex inventory; оба harness были `ready` и authenticated в момент dispatch.
- `PASS`: Project с mapping для `local-real` был создан, mapping добавлен в Node deployment, а Worker с явным `local-real/fx` завершил read-only `pwd`.
- `PASS`: Worker с явным `local-real/codex` завершил read-only `pwd` в runtime-provided mapped workspace без `fx` fallback.
- `BLOCKED`: это один локальный Node, без отдельного MacBook и home-server; Claude Code и Telegram недоступны.
- `NOT RUN`: Approval/needs_input, queue, multi-Attempt, replay, revoke, network-loss и оставшиеся cross-Node scenarios.

Эта запись добавляет partial evidence для real `fx` и Codex, но не закрывает полную acceptance matrix ниже.

## Latest isolated lifecycle follow-up

- `run_id`: `p4-real-local-20260914-followup`
- `commit`: `f88460d`
- `PASS`: `user.md` изменён с revision 1 на revision 2, а следующий настоящий Secretary turn использовал новую preference в ответе.
- `PASS`: отдельный Client B получил active credential, успешно прочитал Conversation, затем после revoke получил `401` на чтение и запись.
- `PASS`: Node `local-real` был drained и revoked без active Attempts; последующий явный Dispatch получил `core: node revoked`.
- `FAIL`: две входящие сообщения были отправлены во время работы Secretary, но созданные Worker Attempts завершились с invalid workspace и не выполнили команду. Это не evidence успешного queue/parallel flow.
- `NOT RUN`: Approval/needs_input, replay, multi-Attempt, network-loss, Telegram и cross-Node scenarios.

Эта запись расширяет partial evidence и сохраняет неуспешный queue run, но не закрывает полную acceptance matrix ниже.

## Real Codex Approval round-trip

- `run_id`: `p4-real-approval2`
- `commit`: `9debd40`
- `PASS`: свежий изолированный Secretary server и outbound Node запустили настоящий Codex ACP; `fx` probe в этом прогоне был fixture-only из-за недоступности `fx models` и не считается evidence для `fx`.
- `PASS`: настоящий Codex запросил запись во внешний путь вне workspace. Node опубликовал permission activity с capability из inventory, Worker перешёл в `waiting_approval`, а pending Approval появился в API.
- `PASS`: отдельный Client B получил credential, одобрил запрос через API, Worker завершился `succeeded`, а внешний sentinel получил ровно `APPROVED`.
- `NOT RUN`: `needs_input`. Тот же настоящий Codex завершил flow с сообщением, что механизм user input недоступен, и не создавал input request. Это не считается PASS.
- `NOT RUN`: queue, multi-Attempt, replay, revoke, network-loss, Telegram, Claude Code и cross-Node scenarios.

Эта запись доказывает только Codex Approval round-trip после исправления capabilities и не закрывает полную acceptance matrix ниже.

## Latest ACP elicitation probe

- `run_id`: `p4-real-acp-elicitation-20260914T051704Z`
- `commit`: `be3f80f`
- `command`: `python3 scripts/phase4-real-acp-elicitation-probe.py`
- `observed_at_utc`: `2026-09-14T05:17:04Z`–`2026-09-14T05:17:22Z`
- `PASS`: реальный `codex-acp` согласовал ACP v1 initialize с form capability, вызвал конкретный MCP tool через временный локальный сервер, отправил валидный form `elicitation/create`, получил accept response, вернул downstream tool result и завершил prompt.
- `NOT RUN`: это прямой ACP probe, не Secretary Node/API flow. Durable pending request, Client B response и terminal continuation через `respond_worker` не считаются PASS.

Это подтверждает фактическое поведение установленного Codex ACP и оставляет обязательный full real-harness `needs_input` сценарий открытым.

## Latest isolated real needs_input attempt

- `run_id`: `p4-real-input7-20260914`
- `commit`: `bb3c694`
- `command`: `zsh /tmp/p4-real-input7.sh`
- `observed_at_utc`: `2026-09-14T08:27:16Z`–`2026-09-14T08:30:45Z`
- `PASS`: свежий изолированный Secretary server и outbound Node поднялись; Codex HarnessInstance был `ready` с `capacity=2`; Worker выполнил реальный Codex ACP flow и завершился с одним `succeeded` Result.
- `BLOCKED`: Codex не создал `user_input_request` и не оставил pending input Approval. Поэтому Client B не получил реальный request ID для `respond_worker`, а полный `needs_input` round-trip не запускался. Это не заменяется прямым ACP elicitation probe.
- `PASS` для отрицательной части no-input check: в durable events был один `attempt.started`, один `attempt.outcome_recorded`, один `result.accepted` и не было `attempt.activity` с `user_input_request`.

Evidence оставлено только во временном redacted run directory. Секреты, cookies, prompts, runtime/session IDs и значения credentials в документацию не записывались.

## Latest isolated network-loss simulation

- `run_id`: `p4-network-loss-real-20260914`
- `commit`: `3a0270a`
- `command`: `zsh /tmp/p4-network-loss-real.sh`
- `observed_at_utc`: `2026-09-14T06:11:13Z`–`2026-09-14T06:12:02Z`
- `PASS`: изолированный Secretary server и outbound Node с реальным FX подняты на временных data directories; Web session, Project mapping и dispatch прошли.
- `PASS`: прозрачный тестовый WebSocket proxy симулировал сетевой разрыв и намеренно потерял terminal AttemptOutcome. После reconnect Node outbox доставил событие: `OUTBOX_BEFORE_RECONNECT=1`, `OUTBOX_AFTER_RECONNECT=0`; в durable store остались ровно один `phase4_attempt_outcome` и один `phase4_result`, Attempt завершился `succeeded`, Worker перешёл в `idle`.
- `PASS`: Node state содержит ровно один принятый dispatch command, абсолютный sentinel в mapped workspace содержит ожидаемый маркер, а второй процесс и второй Attempt в этом reconnect run не наблюдались.
- `BLOCKED`: это симуляция сетевого разрыва через тестовый proxy, а не физическое отключение интерфейса. Прогон использовал FX, а не Codex, и не закрывает обязательные Claude Code, Telegram, `needs_input`, multi-Attempt и cross-Node строки.
- `PASS` для Scenario 19: в этом lifecycle run durable event list содержал `result.accepted` и `attempt.outcome_recorded`, но не содержал `secretary.turn.*`; terminal Result был принят напрямую без нового Secretary model turn.

Evidence оставлено только во временном redacted run directory. Секреты, cookies и runtime/session IDs в документацию не записывались.

## Latest duplicate command delivery

- `run_id`: `p4-command-duplicate-real-20260914`
- `commit`: `7aa3781`
- `command`: `zsh /tmp/p4-command-duplicate-real.sh`
- `observed_at_utc`: `2026-09-14T06:45:24Z`–`2026-09-14T06:46:03Z`
- `PASS`: на чистом изолированном server/Node с реальным FX один и тот же server→Node `command.dispatch` был доставлен дважды. Node создал только один command claim и accepted outcome.
- `PASS`: run завершился с `RC=0`, `COMMAND_COUNT=1`, `ATTEMPT_COUNT=1`, `RESULT_COUNT=1` и проверенным sentinel; Worker перешёл в `idle`, второй процесс не наблюдался.

Это прямое real-harness evidence дедупликации повторной доставки одного `command_id`. Evidence оставлено только во временном redacted run directory.

## Latest server restart during active Attempt

- `run_id`: `p4-server-restart-real-20260914`
- `commit`: `7aa3781`
- `command`: `zsh /tmp/p4-restart-proxy-real.sh`
- `observed_at_utc`: `2026-09-14T06:51:29Z`–`2026-09-14T06:51:57Z`
- `PASS` только для частичных фактов: на изолированном server/Node с реальным FX перед restart Attempt имел `state=starting`. После restart Attempt стал `interrupted`, Worker стал `offline`, создан ровно один Result, дубликатов процесса и Attempt нет.
- `BLOCKED` для полного acceptance: Node после restart через proxy не переподключился, outbox остался buffered. Поэтому этот прогон не доказывает полный recovery flow, а только консервативное завершение uncertain Attempt.
- `NOT RUN`: этот прогон не доказывает восстановление native session после reconnect.
- `NOT RUN` для полного Scenario 29: в этом прогоне не было второго Node, поэтому отсутствие миграции на другую машину не доказано.

Evidence оставлено только во временном redacted run directory. Секреты, cookies, prompts и runtime/session IDs в документацию не записывались.

## Latest revoked Node dispatch rejection

- `run_id`: `p4-revoked-node-real-20260914`
- `commit`: `0fcd451`
- `command`: `zsh /tmp/p4-revoked-node-real.sh`
- `observed_at_utc`: `2026-09-14T07:04:15Z`–`2026-09-14T07:04:41Z`
- `PASS`: на чистом изолированном server/Node с реальным FX owner control revoke вернул HTTP 200; admin state после revoke показал `online=false`, `draining=true`, `revoked=true`, `health=revoked`.
- `PASS`: последующий explicit `spawn_worker` с binding на revoked Node получил видимую ошибку `core: node revoked`; Node state сохранил `COMMAND_COUNT=0`, sentinel не записан, Worker/Attempt не созданы. Это rejected dispatch до доставки на отозванный Node.

Evidence оставлено только во временном redacted run directory.

## Latest idempotency replay

- `run_id`: `p4-idempotency-real-20260914`
- `commit`: `efa0919`
- `command`: `zsh /tmp/p4-idempotency-real.sh`
- `observed_at_utc`: `2026-09-14T07:10:07Z`–`2026-09-14T07:10:44Z`
- `PASS`: два одинаковых `spawn_worker` actions с одним idempotency key вернули один Worker (`WORKER_B_MATCH=1`), Node state сохранил один dispatch command (`COMMAND_COUNT=1`), один Attempt и один Result (`ATTEMPT_COUNT=1`, `RESULT_COUNT=1`).
- `PASS`: два inbound requests с одним external message ID, но разными idempotency keys, дали один публичный conversation entry (`INBOUND_ENTRIES=1`), второй ответ имел `duplicate=true`.
- `PASS`: proxy намеренно повторил один terminal `attempt.outcome` event frame (`DUPLICATED_RESULT_FRAME=1`); durable events содержат один `attempt.outcome_recorded` и один `result.accepted`, а реальный FX записал sentinel ровно один раз (`SENTINEL_OK=1`, `RC=0`).

Evidence оставлено только во временном redacted run directory.

## Latest idle Worker capacity check

- `run_id`: `p4-idle-capacity-real-20260914`
- `commit`: `19cc036`
- `command`: `zsh /tmp/p4-idle-capacity-real.sh`
- `observed_at_utc`: `2026-09-14T07:16:20Z`–`2026-09-14T07:17:05Z`
- `PASS`: после завершения реального FX Worker snapshots до и после 8-секундного idle периода показали `capacity=1` и `active_attempts=0` (`BEFORE_CAPACITY=1`, `BEFORE_ACTIVE_ATTEMPTS=0`, `AFTER_CAPACITY=1`, `AFTER_ACTIVE_ATTEMPTS=0`).
- `PASS`: run завершился с `RC=0`, одним dispatch command, одним Attempt и одним Result; mapped workspace sentinel подтверждён (`SENTINEL_OK=1`).

Evidence оставлено только во временном redacted run directory.

## Latest real Worker follow-up

- `run_id`: `p4-followup-real-20260914`
- `commit`: `514e8c7`
- `command`: `zsh /tmp/p4-followup-real.sh`
- `observed_at_utc`: `2026-09-14T07:30:46Z`–`2026-09-14T07:31:34Z`
- `PASS`: после первого успешного FX Turn `message_worker` на idle Worker создал новый Turn на том же Worker. Final details содержали два Turn, два Attempt и два Result; follow-up сохранил `node_id=local-codex` и `harness_instance_id=local-codex/fx`, а sentinel второго запроса подтвердился (`SENTINEL_OK=1`, `RC=0`).
- `PASS` для Scenario 27: lifecycle response сначала показал новый Turn в `queued/starting`, затем Worker вернулся в `idle`; отдельный Worker или rebinding не создавались.

Evidence оставлено только во временном redacted run directory.

## Latest unseen Worker Result context

- `run_id`: `p4-unseen-result-real-20260914`
- `commit`: `a516142`
- `command`: `zsh /tmp/p4-unseen-result-real.sh`
- `observed_at_utc`: `2026-09-14T07:40:27Z`–`2026-09-14T07:41:16Z`
- `PASS`: реальный FX Worker завершился с одним Result и записал sentinel `UNSEEN_RESULT_OK` (`RESULT_COUNT=1`, `SENTINEL_OK=1`, `RC=0`). Следующее inbound message запустило реальный Secretary turn; ответ Secretary сообщил `status: succeeded` и точный sentinel unseen Result (`ANSWER_SENTINEL_MATCH=1`).
- `PASS` для Scenario 20: durable event sequence содержал `secretary.turn.queued`, `secretary.turn.started`, `secretary.turn.finished` после `result.accepted`, что подтверждает передачу unseen Result в следующий Secretary context.

Evidence оставлено только во временном redacted run directory.

## Offline UI and Telegram status

- `BLOCKED` для полного Scenario 35: restart run показал Worker `offline` и сохранённую binding через Web/API, но Telegram adapter не был настроен, поэтому согласованный Web+Telegram offline state не подтверждён.
- `BLOCKED` для Scenario 36: Telegram bot и topic configuration недоступны, поэтому Worker Topic mapping и отсутствие delta/raw-event spam не проверялись.

## Scenario 31 retry status

- `BLOCKED`: real-harness proof не запускался. В доступном Client/Control API нет операции `retry_attempt`, а установленный real FX не даёт управляемого способа завершить Attempt с классификацией `retryable`; network loss/restart дают `interrupted`/`runtime_execution_unknown`. DB и protocol payloads намеренно не подменялись, поэтому retryable и uncertain не объявляются PASS.
- `BLOCKED` для Scenario 18: без такого controllable `retryable` outcome нельзя получить несколько Attempts внутри одного Turn и проверить ровно один финальный Result. Реальный follow-up run создал два Attempts и два Results в разных Turns, это не заменяет acceptance.

## Previous deterministic gate run

- `run_id`: `p4-deterministic-c32d9a8-20260914T052227Z`
- `commit`: `c32d9a8`
- `command`: `./scripts/phase4-release-gate.sh`
- `observed_at_utc`: `2026-09-14T05:22:27Z`–`2026-09-14T05:22:44Z`
- `PASS`: release gate завершился с кодом 0, включая Go tests, race tests, vet, command builds, frontend tests/build, embedded assets, CLI, Node deployment/revoke tests и `git diff --check`.
- `PASS`: `sex-cli-test.sh` изолированно завершает `logs` tail и не останавливает основной Secretary server.

## Previous batch deterministic gate run

- `run_id`: `p4-ticket15-deterministic-459709c-20261004T210801Z`
- `commit`: `459709c` (все implementation changes остаются uncommitted на `phase4-implementation`)
- `command`: `./scripts/phase4-release-gate.sh`
- `observed_at_utc`: `2026-10-04T21:08:01Z`
- `PASS`: `mise exec go@1.27.1 -- go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...` и `go build -p 1 ./...`; все пакеты прошли.
- `PASS`: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, два изолированных Vite production builds, сравнение всех шести embedded assets и `go test ./web`.
- `PASS`: `scripts/sex-cli-test.sh`, `scripts/node-deployment-test.sh`, `scripts/node-revoke-test.sh`, gate contract, `gofmt` и `git diff --check`; итог gate exit 0.
- `NOT RUN`: manual real-harness matrix не менялась этим запуском. Native owner login/migration, обязательные внешние сценарии и incomplete states остаются blockers; automated PASS не закрывает Ticket 15.
- `NOTE`: первый gate attempt остановился на gofmt до запуска suite: `internal/telegram/long_message_research_test.go` имел только alignment diff. Применён gofmt без изменения тестовых ожиданий; повторный полный gate после этой точечной правки прошёл.

## Previous deterministic gate run

- `run_id`: `p4-ticket15-shared-459709c-20261005T052525Z`
- `commit`: `459709c` (current batch uncommitted on `phase4-implementation`)
- `command`: `./scripts/phase4-release-gate.sh` — единственный полный запуск этой approved implementation-фазы
- `observed_at_utc`: `2026-10-05T05:25:25Z`
- `PASS`: `mise exec go@1.27.1 -- go test -p 1 ./...` и `mise exec go@1.27.1 -- go test -race -p 1 ./...`; все пакеты прошли, включая `internal/node`, `internal/telegram`, Core/MCP/Runtime.
- `PASS`: `mise exec go@1.27.1 -- go vet -p 1 ./...`, `mise exec go@1.27.1 -- go build -p 1 ./...`, frontend tests 10/10, оба Vite production builds, все шесть embedded asset comparisons, `go test ./web`, CLI/deployment/revoke scripts, `gofmt`, gate contract и `git diff --check`; gate завершился exit 0.
- `NOT RUN`: real manual matrix не запускалась. Local tests/fixtures и этот gate не закрывают live Telegram acceptance22/29, owner login/migration33 либо оставшиеся real harness scenarios.
- `NOTE`: исторический `Node is offline` timing failure ticket29 остаётся зафиксированным; этот успешный serial suite не доказывает, что такой сбой гарантированно исправлен.

## Previous deterministic gate run

- `run_id`: `p4-ticket15-lateack-459709c-20261005T070605Z`
- `commit`: `459709c` (late-ACK implementation remains uncommitted on `phase4-implementation`)
- `command`: `./scripts/phase4-release-gate.sh` — один полный запуск после late-ACK fix
- `observed_at_utc`: `2026-10-05T07:06:05Z`
- `PASS`: `mise exec go@1.27.1 -- go test -p 1 ./...` и `mise exec go@1.27.1 -- go test -race -p 1 ./...`; все пакеты прошли, включая `internal/core`, `internal/ctl`, `internal/node`, `internal/secretary` и `internal/webapi`.
- `PASS`: `go vet -p 1 ./...`, `go build -p 1 ./...`, frontend tests 10/10, обе Vite-сборки, сравнение всех шести embedded assets, `go test ./web`, CLI/deployment/revoke scripts, gate contract, `gofmt` и `git diff --check`; итоговый gate exit 0.
- `PASS local integration/privacy`: isolated `sex-cli-test.sh` и `node-deployment-test.sh` прошли, включая выбор отдельных Secretary/Worker native stores и legacy-preservation gate. Все native/live opt-in flags были unset; authenticated native acceptance не запускалась.
- `NOT RUN`: real Telegram General → Worker Topic → следующий user turn и оставшаяся manual real-harness matrix. Gate не закрывает Ticket 15.
- `NOTE`: historical `Node is offline` timing failure остаётся в ledger; успешный serial suite не доказывает его гарантированное устранение.

## Previous deterministic gate run: Approval intent/terminal fix

- `run_id`: `p4-ticket15-approvalfix-459709c-20261005T081907Z`
- `commit`: `459709c` (approval-fix implementation uncommitted on `phase4-implementation`)
- `command`: `./scripts/phase4-release-gate.sh` — один полный запуск после двух Approval fixes
- `observed_at_utc`: `2026-10-05T08:19:07Z`
- `PASS`: `mise exec go@1.27.1 -- go test -p 1 ./...`, `mise exec go@1.27.1 -- go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`; все пакеты прошли, включая Core/CTL/Node/WebAPI approval и receipt paths.
- `PASS`: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, обе production Vite-сборки, сравнение всех шести embedded assets и `go test ./web`.
- `PASS`: gate contract, `gofmt`, isolated `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`; общий gate завершился exit 0.
- `PASS local`: durable Approval resolution, receipt, conflict/privacy, no-terminal-revival regressions входят в пройденный Core/CTL/Node/WebAPI suite. Это не подтверждает real provider/Node/Telegram acceptance.
- `NOT RUN`: real Telegram General → Worker Topic → следующий user turn и вся оставшаяся manual real-harness matrix. Все opt-in native/live flags были unset; owner login и production mutation не выполнялись.
- `NOTE`: historical `Node is offline` timing failure остаётся в ledger и не объявляется гарантированно устранённым.

## Latest deterministic gate run: owner retry после durable Approval intent

- `run_id`: `p4-ticket15-approval-retry-459709c-20261005T091712Z`
- `commit`: `459709c` (working tree uncommitted on `phase4-implementation`)
- `command`: `./scripts/phase4-release-gate.sh` — implementer сообщил, что это единственный полный запуск после owner-retry fix
- `observed_at_utc`: `2026-10-05T09:17:12Z`
- `PASS`: `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`; все пакеты прошли, включая Core recovery, CTL saved-intent retry, WebAPI restart/connection-loss/authenticated-ACK и Node protocol tests.
- `PASS`: frontend tests 10/10, обе Vite production builds, все шесть embedded asset comparisons и `go test ./web`.
- `PASS`: release-gate contract, `gofmt`, `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`.
- `PASS local only`: явный owner retry после refresh/reconnect использует сохранённое решение и прежние command/Turn/Attempt IDs; нет auto-retry, новой команды или подмены payload. Evidence и focused regression описаны в Ticket 29; это не real Node/Telegram acceptance.
- `NOT RUN` / `BLOCKED`: row 17 остаётся `BLOCKED` — `p4-real-input7-20260914` не создал `user_input_request`, поэтому live `respond_worker` round-trip не состоялся. Ticket 29 General → Worker Topic → следующий user turn — `NOT RUN`; live Telegram row 22 — `BLOCKED`. Ticket 15 не закрывается этим gate.

## Deterministic automated checks

Запуск:

```sh
./scripts/phase4-release-gate.sh
```

Gate останавливается на первой ошибке и выполняет ровно один полный последовательный Go suite: `mise exec go@1.27.1 -- go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...` и `go build -p 1 ./...`. Кроме того, он запускает gate contract test, `gofmt`-проверку, `npm ci --ignore-scripts --no-audit --no-fund` и `npm test`, изолированные production builds main и Control Room через Vite CLI с временными `--outDir`, обработку assets `clean-vite-assets.mjs`, сравнение шести файлов с embedded assets без записи поверх tracked assets, `go test ./web`, isolated `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh` и `git diff --check`. У frontend нет отдельной команды `typecheck`: исходники JS/Svelte, их production compile проверяет Vite build.

До запуска учитывайте локальные эффекты: `npm ci` заменяет `web/node_modules` и может загрузить зависимости из npm registry (install scripts отключены); Vite пишет в новый временный каталог под `${TMPDIR:-/tmp}`, сравнивает сборку с embedded assets и удаляет только этот каталог, оставляя существующие `web/dist`, `web/dist-control` и tracked bundles нетронутыми. CLI tests используют временный HOME и локальный HTTP server на `127.0.0.1:8081` только после проверки, что порт свободен. Node deploy/revoke tests используют fixtures и временные файлы. Gate не подключается по SSH, не читает credentials, не меняет production config/services/Workers.

Эти проверки покрывают deterministic state machines, idempotency, replay, redaction, UI contracts, embedded files, CLI и Node deployment seams. Они не являются real-harness proof и не подменяют пункты ниже.

## Manual real-harness proof

Для повторяемого сбора evidence можно запустить интерактивный wizard:

```sh
./scripts/phase4-manual-acceptance-wizard.sh
```

Он не принимает credentials, не сохраняет cookies или prompts, не подменяет real harness deterministic doubles и не меняет статус Ticket 15. Wizard создаёт новый ledger с режимами `PASS`, `FAIL`, `BLOCKED`, `UNAVAILABLE` и `NOT RUN`; существующий ledger намеренно не перезаписывается. В конце он проверяет все 38 строк и записывает строку решения со статусом `PASS` только если каждая обязательная строка имеет `PASS`; иначе решение остаётся `BLOCKED`.

### 1. Чистая конфигурация и два Node

Запускать новый прогон нужно в отдельной директории и с отдельным `HOME`. Не использовать существующий `~/.local/share/secretary`.

```sh
export GATE_RUN="$PWD/.scratch/phase4-runs/$(date -u +%Y%m%dT%H%M%SZ)"
mkdir -p "$GATE_RUN/server-home" "$GATE_RUN/macbook-home" "$GATE_RUN/home-server-home"
export SERVER_URL="https://<private-tailscale-name-or-address>"
```

На Secretary server:

```sh
HOME="$GATE_RUN/server-home" ./sex setup
HOME="$GATE_RUN/server-home" ./sex doctor
HOME="$GATE_RUN/server-home" ./sex start
```

`SERVER_URL` должен указывать на этот server. Pairing tokens передаются только через окружение первой команды и сразу удаляются:

```sh
HOME="$GATE_RUN/macbook-home" ./sex node setup \
  --server "$SERVER_URL" --name macbook \
  --workspace frontend=/Users/<owner>/src/frontend
SECRETARY_NODE_PAIRING_TOKEN='<one-time-macbook-token>' \
  HOME="$GATE_RUN/macbook-home" ./sex node start
unset SECRETARY_NODE_PAIRING_TOKEN

HOME="$GATE_RUN/home-server-home" ./sex node setup \
  --server "$SERVER_URL" --name home-server \
  --workspace frontend=/srv/<owner>/src/frontend
SECRETARY_NODE_PAIRING_TOKEN='<one-time-home-token>' \
  HOME="$GATE_RUN/home-server-home" ./sex node start
unset SECRETARY_NODE_PAIRING_TOKEN
```

В реальном прогоне зафиксировать в ledger observed inventory каждого Node: несколько `HarnessInstances`, version, models и capabilities. OpenCode v2 входит в default discovery; ready подтверждается только после version/auth/model observations и успешного ACP initialize. Отсутствующий или неготовый OpenCode блокирует clean-install default сценарий. Отсутствие `fx`, Claude Code или Codex также блокирует их explicit adapter coverage.

Остановить и поднять server заново после первого набора проверок:

```sh
HOME="$GATE_RUN/server-home" ./sex restart
HOME="$GATE_RUN/server-home" ./sex status
```

Ожидается та же Personal Conversation, тот же Worker binding и отсутствие второго выполнения активного Attempt.

### 2. Clients и каналы

Открыть Web только из bootstrap URL, напечатанного `sex start`, и не сохранять fragment в evidence. Проверить одну Personal Conversation в Web и в General chat Telegram. Telegram включается только в отдельном ручном окружении:

```sh
export SECRETARY_TELEGRAM_ENABLED=true
export SECRETARY_TELEGRAM_BOT_TOKEN='<private-bot-token>'
export SECRETARY_TELEGRAM_SERVER_CREDENTIAL='<separate-server-credential>'
export SECRETARY_TELEGRAM_OWNER_CHAT_ID='<owner-chat-id>'
HOME="$GATE_RUN/server-home" ./sex restart
```

Pairing Telegram выполнять через `POST /v1/telegram/pairing` authenticated Client-ом, затем проверить General chat и Worker Topics. В ledger записывать только redacted response metadata. Control Room проверять отдельно после `HOME=... ./sex restart --debug`, а затем убедиться, что тот же URL в normal mode возвращает 404:

```sh
HOME="$GATE_RUN/server-home" ./sex restart --debug
open http://127.0.0.1:8081/control-room
HOME="$GATE_RUN/server-home" ./sex stop
HOME="$GATE_RUN/server-home" ./sex start
curl -fsS -o /dev/null -w '%{http_code}\n' http://127.0.0.1:8081/control-room  # 404
```

### 3. Harness runs

Для каждой обязательной комбинации использовать новый Project workspace и записать фактический Node, HarnessInstance, model ID, terminal outcome и timestamp. Не менять Claude Code на `fx` для удобства.

```sh
# default Worker policy: без harness override, ожидается OpenCode v2
# проверить openai/gpt-6-luna / xhigh по observed inventory и дождаться Result

# explicit fx: выбрать fx в request/Project policy; binding остаётся fx при Follow-up/reconnect
# explicit Claude Code: выбрать Claude Code и MacBook в request/Project policy
# explicit Codex: выбрать Codex и home server в request/Project policy

# unknown model: указать model ID, отсутствующий в inventory выбранного HarnessInstance
# ожидается видимая ошибка и отсутствие dispatch на другой harness
```

Перед каждым новым harness можно сохранить конфигурацию и перезапустить server. Для отсутствующего бинарника сначала выполнить `sex doctor`, затем записать `BLOCKED`, а не заменять его другим harness. OpenCode v2 обязателен для default-сценария: отсутствие бинарника или ready inventory даёт `BLOCKED`, не `UNAVAILABLE`. OpenCode не используется как замена Claude Code.

### Security evidence beyond scenario 24

Отдельно проверить redaction с уникальным тестовым sentinel, который не является настоящим секретом: поместить его в каждый credential role, diagnostic export, Worker envelope, Profile и обычный log path, затем убедиться, что наружу выходит только redacted marker. В ledger сохранить только имя sentinel и список проверенных поверхностей, не его значение. Повторить после revoke Client и Node. Для no-silent-fallback сохранить visible error для unknown model и отсутствующего harness, а также отсутствие dispatch и process в server/Node evidence.

## Scenario 24 proof matrix

Статус до реального прогона: `NOT RUN`. Поле evidence заполняется по шаблону ниже.

| # | Что доказать вручную | Минимальное evidence |
|---:|---|---|
| 1 | Чистый Secretary server запущен | PASS: `p4-real-tailscale.XOzeoY`, чистый Secretary server поднят и принял подключения Node |
| 2 | Подключены MacBook Node и home server Node | PASS: `p4-real-tailscale.XOzeoY`, локальный MacBook Node и отдельный Tailscale home-server Node подключились |
| 3 | Каждый Node сообщает несколько HarnessInstances | inventory snapshot с version/models/capabilities |
| 4 | Project зарегистрирован с разными path mappings | PASS: `p4-real-tailscale.XOzeoY`, Project mappings для `macbook` и `home-server` различались |
| 5 | Web и Telegram видят одну Personal Conversation | одинаковый conversation reference и два redacted screenshots |
| 6 | Задача без harness override принята | inbound entry, acknowledgement и dispatch record |
| 7 | Без override выбран OpenCode v2 с утверждёнными Worker model/reasoning defaults, независимо от Secretary harness | Secretary config и Worker HarnessInstance `opencode`, observed `openai/gpt-6-luna` / `xhigh` |
| 8 | Explicit Claude Code направлен на MacBook | request, binding `macbook/claude`, terminal Result |
| 9 | Неизвестный model ID даёт visible error без fx fallback | error entry, отсутствие dispatch к `fx` |
| 10 | Создан один Worker и первый Turn, Task нет в Client API | Client API response и Worker/Turn IDs |
| 11 | Изменённый `user.md` виден следующему Secretary turn | PASS: `p4-real-local-20260914-followup`, revision 1 → 2 и следующий Secretary turn использовал новое предпочтение |
| 12 | Acknowledgement приходит до completion | timestamps acknowledgement и Result |
| 13 | Видны `started`, deltas, tool call/result и `finished` | Web replay или export event types |
| 14 | Второе сообщение во время Secretary turn queued, Workers параллельны | ordered queue entries и overlapping Worker timestamps |
| 15 | Activity соответствует capabilities HarnessInstance | inventory capabilities и normalized activity list |
| 16 | Worker запрашивает Approval через Node/harness | PASS: `p4-real-approval2`, реальный Codex опубликовал permission activity, Worker стал `waiting_approval`, pending Approval появился в API |
| 17 | Другой Client отвечает Approval и `needs_input` через `respond_worker` | BLOCKED: `p4-real-input7-20260914` не создал `user_input_request`, поэтому Client B не получил request ID и live flow не запускался. Local `internal/webapi/approval_retry_test.go` покрывает owner retry сохранённого intent после restart/reconnect с прежними IDs; это не real acceptance |
| 18 | Несколько Attempts дают diagnostics, но один Result на Turn | BLOCKED: нет controllable real `retryable` outcome; follow-up с двумя Turns/Results не выдаётся за multi-Attempt acceptance |
| 19 | Terminal Result доставлен напрямую без Secretary model turn | PASS для исторической прямой доставки: `p4-network-loss-real-20260914`, без `secretary.turn.*`. Локальные public tests проверяют join до receipt, late authenticated receipt после waiter removal/reopen, exact Node/command/Turn/Attempt, authoritative denial и запрет оживления terminal Turn (`TestLateWorkerCommandReceiptReconcilesExactResultAfterRestart`, `TestLateAcceptedRespondOutcomeReconcilesAfterWaiterRemoval`, `TestAuthoritativeWorkerCommandDenialIsNotSupersededByLateAcceptance`, `TestRespondTimeoutResultThenLateAcceptedReceiptDoesNotRetryOrReviveTurn`). Real Telegram echo acceptance — NOT RUN |
| 20 | Следующий Secretary context содержит unseen Result | PASS: `p4-unseen-result-real-20260914`, следующий Secretary turn вернул `status: succeeded` и `UNSEEN_RESULT_OK` из Worker Result |
| 21 | Follow-up идёт тому же Worker новым Turn | PASS: `p4-followup-real-20260914`, тот же Worker получил второй Turn при двух Turn IDs и прежнем binding |
| 22 | Явная задача Codex выполняется на home server | PASS: `p4-real-tailscale.XOzeoY-codex`, binding `home-server/codex` и terminal Result подтверждены |
| 23 | Restart server во время active Attempt безопасен | BLOCKED: `p4-server-restart-real-20260914` подтвердил `starting` → `interrupted`, offline Worker, один Result и отсутствие дублей, но Node после restart не переподключился и outbox остался buffered |
| 24 | Network loss после harness completion replay-ит outbox | PASS: `p4-network-loss-real-20260914`, proxy-сбой после terminal outcome, outbox `1 → 0`, один AttemptOutcome |
| 25 | Повтор `command_id` не создаёт process/Attempt | PASS: `p4-command-duplicate-real-20260914`, один `command.dispatch` доставлен дважды; Node создал один claim/outcome, один Attempt и один Result, Worker вернулся в `idle` |
| 26 | После сбоя `interrupted` или доказанное native session recovery | BLOCKED: `p4-server-restart-real-20260914` подтвердил explicit `interrupted` Result и отсутствие дублей, но Node не переподключился после restart; native session recovery не доказано |
| 27 | `message_worker` выбирает resume или Follow-up | PASS: `p4-followup-real-20260914`, idle Worker получил новый Turn, всего два Turn/Attempt/Result при прежнем binding |
| 28 | Idle Worker не тратит active Attempt capacity | PASS: `p4-idle-capacity-real-20260914`, до/после idle `capacity=1`, `active_attempts=0` |
| 29 | Offline Node не мигрирует Worker | NOT RUN: `p4-server-restart-real-20260914` подтвердил offline status и сохранение binding, но без второго Node не проверил отсутствие миграции |
| 30 | Internal subagent отображается activity, child Worker отсутствует | activity stream и server state без child Worker |
| 31 | `retry_attempt` только после terminal `retryable`, uncertain не retry-ится | BLOCKED: нет публичного `retry_attempt` и контролируемого real `retryable` AttemptOutcome; не подменялось DB/protocol payloads |
| 32 | Повтор inbound/action/event/Result не дублирует state | PASS: `p4-idempotency-real-20260914`, inbound `INBOUND_ENTRIES=1`/`duplicate=true`, один Worker/command/Attempt/Result, duplicate terminal event дал один durable outcome |
| 33 | Revoked Client не читает и не меняет state | PASS: `p4-real-local-20260914-followup`, Client B после revoke получил `401` на чтение и запись |
| 34 | Revoked Node не принимает Dispatch | PASS: `p4-revoked-node-real-20260914`, revoke HTTP 200; следующий dispatch отвергнут как `core: node revoked`, `COMMAND_COUNT=0` |
| 35 | Offline Node оставляет Worker видимым и понятным | BLOCKED: Web/API offline status и binding наблюдались в `p4-server-restart-real-20260914`, но Telegram не настроен |
| 36 | Telegram Topic соответствует Worker без delta/raw-event spam | BLOCKED для real acceptance: Telegram bot/topic недоступны. Approved ticket22 semantic/grapheme splitting и retry/dedup покрыты local HTTP/Adapter tests; live Topic/General delivery не запускалась |
| 37 | Go, race, vet и frontend checks проходят | PASS latest: `p4-ticket15-opencode-probe-scope-459709c-20261005T113150Z`; предыдущий authenticated reconnect timeout расследован как host OpenCode probe в process test |
| 38 | OpenCode v2 проходит default Secretary/Worker acceptance; `fx`, Claude Code и Codex проходят как explicit adapters | BLOCKED overall: real `fx` и Codex имеют evidence (`p4-real-local-20260914`, `p4-real-tailscale.XOzeoY-codex`), Claude Code недоступен; native selected-store auth/catalog/ACP readiness теперь PASS, но owner login в новых stores, authenticated Worker acceptance, legacy migration approval и default end-to-end Telegram flow остаются NOT RUN |

## Evidence ledger

Один ledger создаётся на `GATE_RUN`. В него не попадают токены, cookies, bootstrap fragments, полные пути с приватными именами и raw model prompts.

```text
run_id: <UTC id>
commit: <git rev-parse HEAD>
operator: <name>
server_host: <redacted private host>
started_at_utc: <timestamp>

evidence_id | scenario | state | command_or_artifact | observed_at_utc | notes
-------------|----------|-------|--------------------|------------------|------
<id>         | 1        | PASS/FAIL/BLOCKED/UNAVAILABLE/NOT RUN | <command or redacted path> | <timestamp> | <short fact>
```

Допустимые состояния:

- `PASS`: выполнено на чистой конфигурации, evidence воспроизводимо и привязано к commit/run ID.
- `FAIL`: проверка выполнялась и получила неправильный результат. Это не заменяется пояснением в notes.
- `BLOCKED`: обязательное внешнее условие отсутствует, например нет бинарника, credentials, второй машины, private network или Telegram bot. Acceptance не пройден.
- `UNAVAILABLE`: сохраняйте, когда требуемую native проверку нельзя выполнить из-за отсутствия owner-controlled доступа или авторизации. Это не `PASS`: обязательный harness остаётся незакрытым, а итог Ticket 15 — `BLOCKED`. Отсутствие нужной машины, bot или другого внешнего prerequisite можно отмечать `BLOCKED`; отсутствие попытки — `NOT RUN`.
- `NOT RUN`: evidence отсутствует. Это не PASS.

## Финальное решение

Phase 4 можно назвать принятой только когда deterministic script завершился успешно и все применимые строки 1-38 имеют `PASS`, включая default OpenCode v2 и explicit adapter coverage. Любой `FAIL` или `BLOCKED` по обязательному пункту оставляет gate незавершённым. Ручные flows не автоматизированы этим репозиторием и не должны быть представлены как автоматизированные.

## Previous deterministic gate: owner retry после Approval restart/recovery

- `run_id`: `p4-ticket15-approval-retry-459709c-20261005T091712Z`; base `459709c`, branch `phase4-implementation`, рабочее дерево uncommitted.
- `observed_at_utc`: `2026-10-05T09:17:12Z`; финальный запуск после owner-retry fix и recovery regression: `./scripts/phase4-release-gate.sh`, exit 0.
- PASS: `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`; все Go пакеты прошли, включая end-to-end recovery/retry WebAPI test и Node protocol ACK path.
- PASS: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, обе Vite production builds, все шесть embedded asset comparisons и `go test ./web`.
- PASS: gate contract, `gofmt`, `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh` и `git diff --check`.
- Local-only evidence: retry после restart/connection loss повторяет saved decision с прежними command/Turn/Attempt identities; authenticated receipt сохраняет canonical Result и отдельную queued Turn. Тестовые protocol peers не являются real Node/Telegram acceptance.
- `NOT RUN`: real Telegram General → Worker Topic → следующий user turn и остальная обязательная real-harness matrix. Ни production state, ни credentials, ни внешние profiles не менялись; Ticket 15 остаётся `claimed`.

## Previous deterministic gate: approval retry strict DTO privacy

- `run_id`: `p4-ticket15-approval-retry-privacy-459709c-20261005T095131Z`; base `459709c`, branch `phase4-implementation`, рабочее дерево uncommitted.
- `observed_at_utc`: `2026-10-05T09:51:31Z`; один полный запуск после последнего API/UI patch: `./scripts/phase4-release-gate.sh`, exit 0.
- PASS: `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`; включая narrow approval-write-only public HTTP regressions.
- PASS: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, обе Vite production builds, все 6 embedded assets и `go test ./web`.
- PASS: gate contract, `gofmt`, `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`.
- Public credential responses для retry/idempotency/resolved no-handoff и adjacent approve/deny возвращают strict allowlisted DTO. Write-only Client по-прежнему получает 403 на worker/approval read. Это local deterministic evidence, не live Node/Telegram acceptance.
- `NOT RUN`: real Telegram General → Worker Topic → следующий user turn и остальная real-harness matrix. Owner login/production/auth mutation не выполнялись; Ticket 15 остаётся `claimed`.

## Previous deterministic gate: approval auth rejection and owner refresh DTO

- `run_id`: `p4-ticket15-approval-auth-refresh-459709c-20261005T102708Z`; base `459709c`, branch `phase4-implementation`, working tree uncommitted.
- `observed_at_utc`: `2026-10-05T10:27:08Z`; один полный запуск после последних auth/observer/UI-test patches: `./scripts/phase4-release-gate.sh`, exit 0.
- PASS: `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet -p 1 ./...`, `go build -p 1 ./...`; все packages прошли, включая deterministic public HTTP revoke-between-checks test и Owner observer refresh regressions.
- PASS: `npm ci --ignore-scripts --no-audit --no-fund`, frontend tests 10/10, main и Control Room Vite production builds, все 6 embedded-asset comparisons и `go test ./web`.
- PASS: release-gate contract, `gofmt`, `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, `git diff --check`.
- RED→GREEN auth evidence: при временном воспроизведении старого ignored-auth-failure handler тест получил status 401, продолженную mutation, appended owner DTO и non-single body; после guard повторная auth ошибка завершает обработку без mutation/DTO/второй записи. Revoke выполняется настоящим public `/v1/clients/{id}/revoke` HTTP call через barrier, без sleep. Invalid approval scope остаётся 403 без mutation.
- Owner `GET /v1/workers/{ref}` refresh показывает saved `approved` или `denied` через `resolution_state`; owner observer DTO не передаёт RequestID/actor, saved response или private command identity. Credential Client allowlist и `approval.id` retry не изменены. Это deterministic evidence, не live acceptance.
- Pending matrix: Ticket 29 General → Worker Topic → следующий user turn — `NOT RUN`; Ticket 22 live Topic/General — `BLOCKED`; остальные `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` сохранены; Ticket 15 остаётся `claimed`. Production, external credentials и auth state не менялись.

## Previous final gate: ACP output drain test correction — FAIL

- `run_id`: `p4-ticket15-domain-drain-459709c-20261005T105601Z`; base `459709c`, branch `phase4-implementation`, рабочее дерево uncommitted; запись зафиксирована `2026-10-05T10:56:01Z`.
- `./scripts/phase4-release-gate.sh` был запущен один раз после последних vocabulary/test changes. Stages 1 (`phase4-release-gate-test.sh`) и 2 (`gofmt`) прошли. Stage 3 `go test -p 1 ./...` завершился FAIL: `internal/node` → `TestTwoSecretaryNodeProcessesPairInventoryAndReconnect`, `process_integration_test.go:89`, timed out after 30.03s waiting for first Node authenticated reconnect. Оба Node были paired; logs showed process-a reconnect attempt before timeout. На момент этой записи full gate ещё не повторяли; последующий результат записан ниже.
- Последующие stages 4–9 (full-repo race, vet/build, frontend/builds/assets, CLI/deployment/revoke, final gate diff-check) этим неуспешным запуском не выполнялись; их здесь не объявлять PASS.
- Диагностика ровно один раз: `go test ./internal/node -run '^TestTwoSecretaryNodeProcessesPairInventoryAndReconnect$' -count=1 -v` — PASS, 21.38s. Isolated PASS не отменяет failure полного gate и не доказывает отсутствие intermittent reconnect timing failure.
- Flaky ACP regression: `go test ./internal/node -run '^TestACPRuntimeDrainKeepsProgressAvailableAndTurnScoped$' -count=20` — PASS (0.400s). Cross-package race repeat `go test -race -p 1 ./internal/node ./internal/acp ./internal/core ./internal/ctl ./internal/webapi -count=2` — PASS (node59.647s, acp1.361s, core41.682s, ctl13.741s, webapi36.245s). Это не заменяет failed full gate.
- Pending real-harness matrix не менялась: Ticket 29 General → Worker Topic → следующий turn `NOT RUN`; Ticket 22 Topic/General `BLOCKED`; остальные `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` сохранены. Ticket 15 остаётся `claimed`; paid/prod/auth mutations не было.
- После добавления evidence entries выполнен отдельный `git diff --check` — PASS. Это отдельная проверка, не Stage 9 failed gate run.

## Latest final gate: isolate OpenCode probe from process acceptance — PASS

- `run_id`: `p4-ticket15-opencode-probe-scope-459709c-20261005T113150Z`; base `459709c`, branch `phase4-implementation`, рабочее дерево uncommitted; `observed_at_utc`: `2026-10-05T11:31:50Z`; inherited `umask=0022`; exit 0.
- Root cause: `secretary-node` включает OpenCode inventory probe по умолчанию, а `Daemon.runConnection` выполняет `Inventory.Discover` до `DialProtocol`. Process acceptance стартовал Node с fake `fx`/Claude/Codex fixtures, но без opt-out от host `opencode`; поэтому каждый старт/reconnect синхронно вызывал native CLI до authenticated handshake. Изолированный `env -i` probe в private temporary HOME/XDG store: `opencode --version` завершился за 0.601s, `opencode auth list` не завершился за 45s и был остановлен diagnostic command timeout. Product probe ограничивает каждый такой CLI step 10s. Строка `connecting outbound` логируется до `daemon.Run` и не доказывает, что WebSocket handshake завершился.
- TDD: без `--include-opencode=false` process-level tripwire дал ожидаемый RED: `go test ./internal/node -run '^TestTwoSecretaryNodeProcessesPairInventoryAndReconnect$' -count=1 -v` завершился на assertion `transport integration unexpectedly ran the OpenCode inventory probe`. Tripwire только фиксирует вызов; он не реализует и не подтверждает native behavior. После явного opt-out GREEN: `go test ./internal/node -run '^TestTwoSecretaryNodeProcessesPairInventoryAndReconnect$' -count=3 -v` — PASS, три полных повтора; оба настоящих Node процесса передали FX inventory, а первый Node переподключился по сохранённой identity без pairing token. Ожидание reconnect и 30s deadline не ослаблялись.
- Изменён только scope `internal/node/process_integration_test.go`: native OpenCode probe отключён в transport/pairing test, его host invocation проверяется tripwire-маркером; production default и native acceptance expectations не менялись. Синтетический tripwire не засчитывается как OpenCode acceptance.
- Combined-load check `go test -race -p 1 ./internal/node ./internal/acp ./internal/core ./internal/ctl ./internal/webapi -count=2` — PASS: node 18.787s, acp 1.363s, core 41.128s, ctl 13.721s, webapi 36.451s.
- Один промежуточный gate wrapper был некорректен: run `p4-ticket15-opencode-probe-scope-459709c-20261005T112859Z` выставил `umask 077` ради временного лога. Stages 1–2 PASS, Stage 3 остановился на `TestOpenCodeNativeStorePreservesLegacySessionsUntilApprovedMigration`, потому что fixture создаёт каталог mode `0755`, а такой umask даёт `0700`; stages 4–9 не запускались. Это изменение окружения внесено wrapper-ом, не product code. Не считать этот запуск результатом acceptance.
- Корректный запуск `./scripts/phase4-release-gate.sh` при исходном `umask=0022` прошёл все stages: **1/9** release-gate contract — PASS; **2/9** gofmt — PASS; **3/9** `go test -p 1 ./...` — PASS (все пакеты, включая internal/node 5.449s); **4/9** `go test -race -p 1 ./...` — PASS (все пакеты, включая internal/node 9.658s); **5/9** `go vet -p 1 ./...` и `go build -p 1 ./...` — PASS; **6/9** `npm ci --ignore-scripts --no-audit --no-fund` и `npm test` — PASS; **7/9** обе production Vite builds, очистка и сравнение всех 6 embedded assets, `go test ./web` — PASS; **8/9** `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh` — PASS; **9/9** `git diff --check` — PASS.
- Manual real-harness matrix не менялась: Ticket 29 General → Worker Topic → следующий turn — `NOT RUN`; Ticket 22 live Topic/General — `BLOCKED`; owner login, authenticated inventory/Worker run и native-store migration approval — `NOT RUN`; остальные `FAIL`/`BLOCKED`/`UNAVAILABLE`/`NOT RUN` сохранены. Diagnostic CLI timeout не является native acceptance. Ticket 15 остаётся `claimed`; production/auth/paid mutations не выполнялись.

## Ticket 33 branch release gate — PASS

- `run_id`: `p4-33-shared-native-fullgate-final2-20261005`; branch `p4/33-shared-opencode-store-b205fa`; base/HEAD `5fcde24f95551d18ffb3a3832f567530f809e226`; рабочее дерево uncommitted; `observed_at_utc`: `2026-10-05T15:25:37Z`.
- Final `./scripts/phase4-release-gate.sh`: stages **1–9 PASS** — release contract, gofmt, `go test -p 1 ./...`, `go test -race -p 1 ./...`, `go vet`, all-package build, frontend tests/builds/assets, `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, final `git diff --check`. Frontend tests: 10/10.
- Первый полный запуск завершился FAIL на Stage 8: `node-deployment-test.sh` всё ещё ожидал прежний второй Node DB. Это была устаревшая проверка, а не product failure. Проверку обновили на общий путь; дополнительно Node LaunchAgent setup больше не запускает Secretary setup, чтобы standalone remote Node не создавал локальную Secretary installation. Public tests проверяют эту границу и config-only FX transition; финальный повтор прошёл 1–9.
- Настоящие native unpaid fixtures указаны выше и запускались отдельно с opt-in flag; обычный release gate использовал default deterministic configuration.
- Не выполнялись provider login в новом store, authenticated provider calls, production/service rollout или legacy data migration. Этот gate не закрывает manual real-harness matrix и не является независимым review.

## Ticket 31 production auth probe — final source/test gate PASS

- Worktree `p4/31-opencode-auth-probe-3934bd`, base `204c466a4a74956331153f2136858d691b7fac19`. Первичный полный gate сохранил FAIL на Stage 3: `TestOpenCodeConfigurationRegistrationAndFailClosed/missing-mode/resume=true`. После исправления нестабильного synthetic helper deadline (300ms → 1s; assertions не ослаблены) повторный финальный gate прошёл stages 1–9: deterministic tests, race, vet/build, frontend tests/build/assets, `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`, diff check — PASS. Initial и final outputs сохранены в `/tmp/secretary-ticket31-auth-probe-fullgate.log` и `/tmp/secretary-ticket31-auth-probe-finalgate.log`.
- RED→GREEN: старый `auth list` без флагов воспроизводил service timeout/ложный `sex node doctor` failure. Probe и обе public Doctor теперь используют `auth list --format json --standalone` в Go-validated selected store. Readiness требует exit 0 и корректный auth JSON с stored `connections[].type=credential`; пустой/ошибочный JSON, only-environment credentials, stderr-only, failed command, OAuth-shape и unsupported version не ready. Environment/personal-store fallback исключён; setup не выполняет auth/login.
- Focused PASS: stored/empty/malformed/non-credential/failed-output/version/context cases; subprocess boundary проверяет exact flags, selected store, отсутствие ambient credentials, private HOME cleanup и kill при timeout. `./scripts/sex-cli-test.sh` и `./scripts/node-deployment-test.sh` PASS.
- Native selected-store PASS на настоящем OpenCode v2.0.22: `status=ready`, stored credential present, 28 models, 7 reasoning levels, обе exact model IDs (`openai/gpt-6.1-sol`, `openai/gpt-6-luna`) и `xhigh`; inventory использует native metadata и ACP `initialize`. Никакой `session/new`, login, paid model call, raw auth output или account value не использовались/выводились. Native private-store missing-auth test также PASS и personal-store canary неизменён.
- Ручные gates остаются pending: owner login в новом store, production config/service rollout/restart, Telegram, paid Worker acceptance, existing fx Worker Follow-up/restart, migration approval и independent reviewer pass. Config/Profiles, existing bindings и production не менялись; ticket31 остаётся `claimed`.

## Ticket 29 addressed-reply-v1 terminal completion — local follow-up

- Opt-in Secretary may finish without assistant final text only when verified OpenCode v2 ACP evidence contains successful terminal RPC, exact native `end_turn`, completed FIFO drain and zero parent assistant chunks, and Core atomically finds exactly one durable addressed reply for that server-issued turn/input. Missing/foreign reply or any incomplete condition stays fail-closed. Typed evidence is internal and excluded from public Result JSON; Worker and legacy/unaddressed Secretary gates remain unchanged.
- Public ACP fixture and Secretary/MCP/Core integration verified RED before the fix, then GREEN/negative matrix for exact/no/foreign replies, missing/malformed/other stop reasons, max tokens, refusal, progress-only, RPC error, legacy profile and ordinary assistant final text. Focused package Go tests and race checks passed. Private unpaid OpenCode v2 HTTP fixtures passed for profile permissions/Resume, terminal metadata and native Secretary MCP; all used temporary stores and local synthetic HTTP/MCP endpoints, not paid provider calls or personal native data.
- **Отозвана ошибочная запись:** прежний вызов `umask 0022 ./scripts/phase4-release-gate.sh` не запускал скрипт; его имя было лишним аргументом `umask`. Тот вызов — **NOT RUN**, без gate exit code/stages/log/ID; отсутствие tool error не было evidence.
- **Настоящий запуск:** run label `p4-29-addressed-reply-gate-20261006T090424Z`; команда `umask 0022; ./scripts/phase4-release-gate.sh > /tmp/p4-29-addressed-reply-gate-20261006T090424Z.log 2>&1`; exit **1**, logfile mode `0600`. Stage 1 release-gate contract — PASS; Stage 2 gofmt — PASS; Stage 3 `go test -p 1 ./...` — **FAIL** в `internal/node`, subtest `TestOpenCodeConfigurationRegistrationAndFailClosed/missing-mode/resume=true`, `opencode_configuration_test.go:294: safe protocol counts unavailable`. Stages 4–9 не запускались. Log просмотрен только по stage markers и этой безопасной failure line. Этот FAIL не скрывается последующими focused checks.
- Public RED до fixture fix: exact subtest `-count=5` дал 1 FAIL / 4 PASS из-за отсутствующего counters file. Helper писал counters только после чтения config и первого RPC; outer test context `1s` включал старт test child, initialize, Resume load и конфигурационные retries, поэтому под нагрузкой cancellation могла произойти до первой записи. Это не доказывало какие-либо запрещённые RPC.
- Fixture-only fix: loopback HTTP ready/release barrier подтверждает, что helper стартовал и загрузил managed config, до того как тест разрешает ACP RPC; counters file создаётся до scanner, каждое обновление записывается до ответа. Тестовый outer watchdog теперь `10s`, а production config-ready deadline остаётся `5s`. Public assertions проверяют exact initialize/new/load/mode counts, отсутствие prompt и отсутствие replacement session при Resume; load завершается через FIFO drain до config selection.
- Первая synchronized matrix выявила затенение: `data, err := os.ReadFile(record)` заменял runtime error при проверке deadline. Safe counts в той FAIL-попытке были Start: initialize1/new1/mode189/config RPC189/prompt0; Resume: initialize1/load1/new0/mode188/config RPC188/prompt0. Тест теперь сохраняет `runtimeErr` отдельно от `readErr` и подтверждает внутренний 5s deadline при ещё активном outer watchdog. Intermediate failure сохранён; runtime/production timeout не менялся.
- Focused GREEN: весь `TestOpenCodeConfigurationRegistrationAndFailClosed -count=1` прошёл 11 применимых scenario/Resume cases; exact `missing-mode/resume=true -count=5` и `-race -count=3` прошли.
- **Финальный корректный full gate после fixture fix:** run label `p4-29-addressed-reply-finalgate-20261006T094440Z`; команда `umask 0022; ./scripts/phase4-release-gate.sh > /tmp/p4-29-addressed-reply-finalgate-20261006T094440Z.log 2>&1`; exit **0**, private logfile mode `0600`. Все stages PASS: 1 release-gate contract; 2 gofmt; 3 `go test -p 1 ./...`; 4 `go test -race -p 1 ./...`; 5 `go vet -p 1 ./...` и `go build -p 1 ./...`; 6 `npm ci` и frontend tests; 7 production Vite builds, cleanup, embedded assets comparisons и web tests; 8 sex CLI, Node deployment и revoke integration; 9 `git diff --check`. Скрипт сообщил `Phase 4 deterministic release gate passed.` Прежний gate Stage3 FAIL и no-op umask invocation выше сохранены. После этого ledger update будет выполнен отдельный `git diff --check`.
- The direct executable overflow attempt did not isolate the terminal-response/incomplete-drain seam and was removed; it is not counted as PASS. ACP FIFO barrier tests still verify that a request does not complete before event acknowledgement and that cancellation reports drain failure. At that earlier point no live Telegram, paid prompt, production/config/auth mutation, commit/deploy or independent review had occurred. Current review outcome and later gates are recorded below.

## Ticket 29 review follow-up: `response.Summary` is not final evidence

- Both independent reviews (actual report directory `/private/tmp/secretary-reply-completion-review/`, `spec.md` and `standards.md`) found one deduplicated blocker: `TerminalSummary` allowed `session/prompt` `response.Summary` to pass `AllowsAssistantFinalText()` with zero parent assistant chunks and no exact durable reply. Verified OpenCode v2.0.22 does not advertise this summary field; it is not authoritative final text. Standards also noted duplicated ReplyContractVersion/TerminalMessageGrouping checks in `Start` and `Resume`.
- Public RED: new Secretary/MCP/Core `summary-only` fixture returned `end_turn` plus only an unverified summary, no assistant chunks, and no durable addressed reply. Before the fix it ended `succeeded`; the assertion failed with `state=succeeded, want=failed`.
- Fix: removed `TerminalSummary` from internal evidence; verified OpenCode v2 ignores `response.Summary` for final/cancel text. Assistant final now comes only from assembled parent assistant messageId chunks finalized by `end_turn`; `AllowsAssistantFinalText()` requires a positive chunk count. Zero-chunk completion still goes only through Core's exact durable turn/input reply check. A shared small `validateReplyContract` is used by both `Start` and `Resume`. Legacy/non-grouped ACP summary behavior is unchanged.
- Tests after fix: ACP `summary-only` fails with the generic unavailable summary; public Secretary no-reply summary case remains failed; exact durable MCP-only and actual assistant messageId/end_turn cases pass. Malformed stop/RPC/provider error, max tokens, refusal and progress-only negatives pass. `TestACPLegacyStopReasonsErrorsAndSummaryExtension` passes both FX and non-FX summary extensions. Focused race regressions pass. Private unpaid OpenCode v2.0.22 HTTP fixtures for profile persistence/permissions/Resume, actual assistant final progress separation, and Secretary MCP pass on temporary stores/local synthetic providers.
- Intermediate gate is preserved: `p4-29-summary-evidence-finalgate-20261006T101820Z`, command `umask 0022; ./scripts/phase4-release-gate.sh > /tmp/p4-29-summary-evidence-finalgate-20261006T101820Z.log 2>&1`, exit **1**, logfile mode `0600`. Stages 1–2 PASS; Stage 3 failed only because `TestACPRuntimeDrainKeepsProgressAvailableAndTurnScoped` expected an injected response Summary rather than actual final message chunks. Stages 4–9 did not run. Fixture expectation was corrected to actual assistant chunks; the injected summary now tests that it is ignored.
- **Final full gate after all source/tests:** run `p4-29-summary-evidence-corrected-finalgate-20261006T102309Z`; command `umask 0022; ./scripts/phase4-release-gate.sh > /tmp/p4-29-summary-evidence-corrected-finalgate-20261006T102309Z.log 2>&1`; exit **0**, private logfile mode `0600`. Stages **1–9 PASS**: release-gate contract, gofmt, all Go tests, all-package race, vet/build, frontend install/tests, production frontend/embedded assets, CLI/deployment/revoke integration, final diff check. Final script output: `Phase 4 deterministic release gate passed.` Earlier NOT RUN, Stage 3 failures and previous PASS runs remain preserved above.
- No paid prompts, auth/provider login, production mutation, Telegram, commit, merge or deploy occurred. Ticket 29 remains `claimed`; parent must rerun both independent reviews. Live Telegram acceptance remains `NOT RUN`.

## Latest target893 rollout evidence — manual gate remains blocked

| Evidence | State | Bounded conclusion |
|---|---|---|
| Current role provenance/health | PASS | Secretary and Node processes match target `893`; health is 200. FX defaults are active; auth, selected stores and model/catalog readiness were checked before the live turn without repeating login. Existing 30 FX bindings and 31 Node mappings remain unchanged. Telegram is paused during owner rotation. |
| Actual Sol General turn | **FAIL — functional** | The turn itself succeeded, but `spawn_worker=0`, `reply_to_user=0`, ordinary reply=0; no Worker, Attempt or Result was created. This is not an auth failure. |
| Native synthetic MCP fixture | PASS — fixture only | Isolated OpenCode v2.0.22 completed MCP startup/`initialize`/`tools/list`; nine schemas, including `spawn_worker` and `reply_to_user`, reached a localhost mock provider. This is not live acceptance or evidence of actual production provider choice. |
| Literal-name/schema mismatch | `false / none` | Closed parser found neither a required literal `create_worker` call nor literal arguments incompatible with the actual tool schemas. The canonical creation tool is `spawn_worker`. |
| Remaining acceptance | NOT RUN | Worker Result, Follow-up/Resume, further FX acceptance and Telegram acceptance did not run. No random retry was made. |

Historical facts remain separate: an earlier mixed-runtime provenance discrepancy was corrected before the live turn; the live functional dispatch failure above; later acceptance steps remain NOT RUN; and a prior Pi operator reported a scopes mismatch. That operator-session failure has no established source and does not prove a production authentication failure. Do not relabel it as provider/auth failure or claim a real-provider fix.

**Remaining evidence gap:** the production turn has no audited native `tools/list`; MCP startup availability versus actual provider tool choice remains unknown. The source fix and synthetic schema fixture do not close this gap. Ticket31 and ticket33 remain `claimed`/acceptance-blocked; tickets15, 22 and 29 remain open. Bounded next step: observe actual MCP process startup, `initialize`, `tools/list`, provider schema booleans and addressed-reply/tool-call categories, recording counts only and no prompts, Profile text, arguments, payload, credentials or session IDs. Do not repeat a random live turn.
