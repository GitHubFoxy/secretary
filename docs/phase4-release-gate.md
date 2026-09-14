# Phase 4 release gate

Этот документ разделяет два разных доказательства:

1. **Deterministic automated checks** запускаются из корня репозитория одной командой. Они используют тестовые doubles и локальные integration tests, поэтому не доказывают работу настоящих аккаунтов harness.
2. **Manual real-harness proof** выполняется на чистой конфигурации с настоящими `fx`, Claude Code и Codex. OpenCode проверяется только если он отдельно установлен.

Нельзя помечать ручной пункт как пройденный по результату Go-теста. В evidence ledger должна быть команда или скриншот, время, commit и идентификатор запуска. Секреты, pairing tokens и содержимое `user.md` в ledger не записываются.

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

## Latest duplicate lifecycle replay

- `run_id`: `p4-duplicate-real-20260914`
- `commit`: `9582065`
- `command`: `zsh /tmp/p4-duplicate-real.sh`
- `observed_at_utc`: `2026-09-14T06:23:00Z`–`2026-09-14T06:23:50Z`
- `PASS`: на чистом изолированном server/Node с реальным FX две одинаковые `spawn_worker` submissions с одним idempotency key вернули одного Worker. Replay переиспользовал тот же lifecycle command.
- `PASS`: run завершился с `RC=0`, `RESULT_COUNT=1` и проверенным sentinel; Node state содержит один dispatch command (`COMMAND_COUNT=1`), durable store содержит один Attempt и один Result со статусом `succeeded`, Worker перешёл в `idle`, второй процесс не наблюдался.
- `NOT RUN`: этот прогон не проверял отдельную повторную доставку низкоуровневой команды с тем же `command_id`.

Это real-harness evidence дедупликации повторной lifecycle submission, но не low-level `command_id` replay. Evidence оставлено только во временном redacted run directory.

## Latest server restart during active Attempt

- `run_id`: `p4-server-restart-real-r2-20260914`
- `commit`: `22a9224`
- `command`: `zsh /tmp/p4-restart-real-current.sh`
- `observed_at_utc`: `2026-09-14T08:39:31Z`–`2026-09-14T08:40:01Z`
- `PASS`: на свежем изолированном server/Node с реальным FX перед restart Attempt имел `state=starting`, а Node доставил 27 `attempt.activity` событий. Это подтверждает фактический запуск FX, хотя публичный Worker status в этот момент оставался `queued`.
- `PASS`: server был остановлен в `2026-09-14T08:39:58Z` и поднят заново в `2026-09-14T08:40:00Z`; recovery завершил Attempt одним `interrupted` Result с `failure_code=runtime_execution_unknown`, Worker стал `offline`.
- `PASS`: durable store сохранил ровно один Attempt, один AttemptOutcome и один Result; Node state сохранил один accepted dispatch command. Второй dispatch, Attempt или Result не создан.
- `NOT RUN`: этот прогон не доказывает восстановление native session после reconnect, только корректное `interrupted` завершение.
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

## Latest deterministic gate run

- `run_id`: `p4-deterministic-c32d9a8-20260914T052227Z`
- `commit`: `c32d9a8`
- `command`: `./scripts/phase4-release-gate.sh`
- `observed_at_utc`: `2026-09-14T05:22:27Z`–`2026-09-14T05:22:44Z`
- `PASS`: release gate завершился с кодом 0, включая Go tests, race tests, vet, command builds, frontend tests/build, embedded assets, CLI, Node deployment/revoke tests и `git diff --check`.
- `PASS`: `sex-cli-test.sh` изолированно завершает `logs` tail и не останавливает основной Secretary server.

## Deterministic automated checks

Запуск:

```sh
./scripts/phase4-release-gate.sh
```

Gate останавливается на первой ошибке и выполняет:

- shell contract test для самого gate и этой матрицы;
- `gofmt` для Go-файлов в `internal`, `cmd` и `web`;
- `mise exec go@1.27.1 -- go test ./...`;
- `mise exec go@1.27.1 -- go test -race -p 1 ./...`;
- `mise exec go@1.27.1 -- go vet ./...` и `go build ./cmd/...`;
- `npm ci --ignore-scripts --no-audit --no-fund`, `npm test` и `npm run build:all` в `web`;
- проверку всех шести production assets и `go test ./web`, который читает embedded assets;
- профильные isolated tests: `sex-cli-test.sh`, `node-deployment-test.sh`, `node-revoke-test.sh`;
- повторные `go test ./...` и `go vet ./...` после сборки и `git diff --check`.

Эти проверки покрывают deterministic state machines, idempotency, replay, redaction, UI contracts, embedded files, CLI и Node deployment seams. Они не являются real-harness proof и не подменяют пункты ниже.

## Manual real-harness proof

Для повторяемого сбора evidence можно запустить интерактивный wizard:

```sh
./scripts/phase4-manual-acceptance-wizard.sh
```

Он не принимает credentials, не сохраняет cookies или prompts, не подменяет real harness deterministic doubles и не меняет статус Ticket 15. Wizard создаёт новый ledger с режимами `PASS`, `FAIL`, `BLOCKED` и `NOT RUN`; существующий ledger намеренно не перезаписывается. В конце он проверяет все 38 строк и записывает строку решения со статусом `PASS` только если каждая обязательная строка имеет `PASS`; иначе решение остаётся `BLOCKED`.

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

В реальном прогоне зафиксировать в ledger observed inventory каждого Node: несколько `HarnessInstances`, version, models и capabilities. Для optional OpenCode создать отдельную конфигурацию с `--include-opencode` и записать, установлен ли `opencode`. Не считать отсутствие OpenCode ошибкой, если compatibility target не заявлен установленным. Отсутствие `fx`, Claude Code или Codex блокирует gate.

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
# default Worker policy: без harness override, ожидается fx
# в Web или Telegram отправить actionable request и дождаться Result

# explicit Claude Code: выбрать Claude Code и MacBook в request/Project policy
# explicit Codex: выбрать Codex и home server в request/Project policy

# unknown model: указать model ID, отсутствующий в inventory выбранного HarnessInstance
# ожидается видимая ошибка, Worker не стартует и fx не появляется в dispatch
```

Перед каждым новым harness можно сохранить конфигурацию и перезапустить server. Для отсутствующего бинарника сначала выполнить `sex doctor`, затем записать `BLOCKED`, а не заменять его другим harness. OpenCode запускать только отдельным прогоном и помечать `UNAVAILABLE`, если он не установлен. OpenCode не используется как замена Claude Code.

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
| 7 | Без override выбран `fx`, независимо от Secretary harness | Secretary config и Worker HarnessInstance `fx` |
| 8 | Explicit Claude Code направлен на MacBook | request, binding `macbook/claude`, terminal Result |
| 9 | Неизвестный model ID даёт visible error без fx fallback | error entry, отсутствие dispatch к `fx` |
| 10 | Создан один Worker и первый Turn, Task нет в Client API | Client API response и Worker/Turn IDs |
| 11 | Изменённый `user.md` виден следующему Secretary turn | PASS: `p4-real-local-20260914-followup`, revision 1 → 2 и следующий Secretary turn использовал новое предпочтение |
| 12 | Acknowledgement приходит до completion | timestamps acknowledgement и Result |
| 13 | Видны `started`, deltas, tool call/result и `finished` | Web replay или export event types |
| 14 | Второе сообщение во время Secretary turn queued, Workers параллельны | ordered queue entries и overlapping Worker timestamps |
| 15 | Activity соответствует capabilities HarnessInstance | inventory capabilities и normalized activity list |
| 16 | Worker запрашивает Approval через Node/harness | PASS: `p4-real-approval2`, реальный Codex опубликовал permission activity, Worker стал `waiting_approval`, pending Approval появился в API |
| 17 | Другой Client отвечает Approval и `needs_input` через `respond_worker` | BLOCKED: `p4-real-input7-20260914` подтвердил real Codex без `user_input_request`; Client B не получил request ID, полный `needs_input`/`respond_worker` flow не запускался |
| 18 | Несколько Attempts дают diagnostics, но один Result на Turn | BLOCKED: нет controllable real `retryable` outcome; follow-up с двумя Turns/Results не выдаётся за multi-Attempt acceptance |
| 19 | Terminal Result доставлен напрямую без Secretary model turn | PASS: `p4-network-loss-real-20260914`, `result.accepted`/`attempt.outcome_recorded` без `secretary.turn.*` в event list |
| 20 | Следующий Secretary context содержит unseen Result | PASS: `p4-unseen-result-real-20260914`, следующий Secretary turn вернул `status: succeeded` и `UNSEEN_RESULT_OK` из Worker Result |
| 21 | Follow-up идёт тому же Worker новым Turn | PASS: `p4-followup-real-20260914`, тот же Worker получил второй Turn при двух Turn IDs и прежнем binding |
| 22 | Явная задача Codex выполняется на home server | PASS: `p4-real-tailscale.XOzeoY-codex`, binding `home-server/codex` и terminal Result подтверждены |
| 23 | Restart server во время active Attempt безопасен | PASS: `p4-server-restart-real-r2-20260914`, FX прислал 27 activity до restart; Attempt `starting` → `interrupted`, один Attempt/Outcome/Result без дубля |
| 24 | Network loss после harness completion replay-ит outbox | PASS: `p4-network-loss-real-20260914`, proxy-сбой после terminal outcome, outbox `1 → 0`, один AttemptOutcome |
| 25 | Повтор `command_id` не создаёт process/Attempt | NOT RUN: `p4-duplicate-real-20260914` проверил повторную lifecycle submission с одним idempotency key, но не отдельную повторную доставку низкоуровневой команды с тем же `command_id` |
| 26 | После сбоя `interrupted` или доказанное native session recovery | PASS: `p4-server-restart-real-r2-20260914`, explicit `interrupted` Result с `failure_code=runtime_execution_unknown`; native session recovery отдельно не заявляется |
| 27 | `message_worker` выбирает resume или Follow-up | PASS: `p4-followup-real-20260914`, idle Worker получил новый Turn, всего два Turn/Attempt/Result при прежнем binding |
| 28 | Idle Worker не тратит active Attempt capacity | PASS: `p4-idle-capacity-real-20260914`, до/после idle `capacity=1`, `active_attempts=0` |
| 29 | Offline Node не мигрирует Worker | NOT RUN: `p4-server-restart-real-20260914` подтвердил offline status и сохранение binding, но без второго Node не проверил отсутствие миграции |
| 30 | Internal subagent отображается activity, child Worker отсутствует | activity stream и server state без child Worker |
| 31 | `retry_attempt` только после terminal `retryable`, uncertain не retry-ится | BLOCKED: нет публичного `retry_attempt` и контролируемого real `retryable` AttemptOutcome; не подменялось DB/protocol payloads |
| 32 | Повтор inbound/action/event/Result не дублирует state | PASS: `p4-idempotency-real-20260914`, inbound `INBOUND_ENTRIES=1`/`duplicate=true`, один Worker/command/Attempt/Result, duplicate terminal event дал один durable outcome |
| 33 | Revoked Client не читает и не меняет state | PASS: `p4-real-local-20260914-followup`, Client B после revoke получил `401` на чтение и запись |
| 34 | Revoked Node не принимает Dispatch | PASS: `p4-revoked-node-real-20260914`, revoke HTTP 200; следующий dispatch отвергнут как `core: node revoked`, `COMMAND_COUNT=0` |
| 35 | Offline Node оставляет Worker видимым и понятным | BLOCKED: Web/API offline status и binding наблюдались в `p4-server-restart-real-20260914`, но Telegram не настроен |
| 36 | Telegram Topic соответствует Worker без delta/raw-event spam | BLOCKED: Telegram bot и topic configuration недоступны |
| 37 | Go, race, vet и frontend checks проходят | PASS: `p4-deterministic-c32d9a8-20260914T052227Z`, release gate RC=0 и `git diff --check` чист |
| 38 | Реальные `fx`, Claude Code, Codex проходят; OpenCode отдельно conditional | BLOCKED: real `fx` и Codex имеют evidence (`p4-real-local-20260914`, `p4-real-tailscale.XOzeoY-codex`), Claude Code недоступен; OpenCode не установлен и остаётся optional |

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
- `UNAVAILABLE`: допустимо только для optional OpenCode, если compatibility target не заявлен установленным. Для обязательных `fx`, Claude Code и Codex это `BLOCKED`.
- `NOT RUN`: evidence отсутствует. Это не PASS.

## Финальное решение

Phase 4 можно назвать принятой только когда deterministic script завершился успешно, все применимые строки 1-38 имеют `PASS`, а OpenCode имеет либо `PASS`, либо явно обоснованный `UNAVAILABLE`. Любой `FAIL` или `BLOCKED` по обязательному пункту оставляет gate незавершённым. Ручные flows не автоматизированы этим репозиторием и не должны быть представлены как автоматизированные.
