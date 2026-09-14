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
| 1 | Чистый Secretary server запущен | `sex start`, URL, server log, run ID |
| 2 | Подключены MacBook Node и home server Node | два `sex node status` и server inventory |
| 3 | Каждый Node сообщает несколько HarnessInstances | inventory snapshot с version/models/capabilities |
| 4 | Project зарегистрирован с разными path mappings | Project record и mapping `macbook`/`home-server` |
| 5 | Web и Telegram видят одну Personal Conversation | одинаковый conversation reference и два redacted screenshots |
| 6 | Задача без harness override принята | inbound entry, acknowledgement и dispatch record |
| 7 | Без override выбран `fx`, независимо от Secretary harness | Secretary config и Worker HarnessInstance `fx` |
| 8 | Explicit Claude Code направлен на MacBook | request, binding `macbook/claude`, terminal Result |
| 9 | Неизвестный model ID даёт visible error без fx fallback | error entry, отсутствие dispatch к `fx` |
| 10 | Создан один Worker и первый Turn, Task нет в Client API | Client API response и Worker/Turn IDs |
| 11 | Изменённый `user.md` виден следующему Secretary turn | redacted before/after revision и следующий-turn evidence |
| 12 | Acknowledgement приходит до completion | timestamps acknowledgement и Result |
| 13 | Видны `started`, deltas, tool call/result и `finished` | Web replay или export event types |
| 14 | Второе сообщение во время Secretary turn queued, Workers параллельны | ordered queue entries и overlapping Worker timestamps |
| 15 | Activity соответствует capabilities HarnessInstance | inventory capabilities и normalized activity list |
| 16 | Worker запрашивает Approval через Node/harness | request ID, Node event, Approval state |
| 17 | Другой Client отвечает Approval и `needs_input` через `respond_worker` | Client B request/response и terminal state |
| 18 | Несколько Attempts дают diagnostics, но один Result на Turn | Attempts, AttemptOutcomes и count ровно 1 Result |
| 19 | Terminal Result доставлен напрямую без Secretary model turn | event timeline и отсутствие нового Secretary turn |
| 20 | Следующий Secretary context содержит unseen Result | следующий turn diagnostic/context evidence |
| 21 | Follow-up идёт тому же Worker новым Turn | Worker ID прежний, Turn ID новый |
| 22 | Явная задача Codex выполняется на home server | binding `home-server/codex` и terminal Result |
| 23 | Restart server во время active Attempt безопасен | pre/post status и recovery timeline |
| 24 | Network loss после harness completion replay-ит outbox | отключение сети, local outbox, reconnect и AttemptOutcome |
| 25 | Повтор `command_id` не создаёт process/Attempt | две dispatch submissions, один outcome/process/Attempt |
| 26 | После сбоя `interrupted` или доказанное native session recovery | terminal state и Node recovery evidence |
| 27 | `message_worker` выбирает resume или Follow-up | request, выбранная операция и binding |
| 28 | Idle Worker не тратит active Attempt capacity | capacity snapshot до/после idle периода |
| 29 | Offline Node не мигрирует Worker | offline status, прежний binding, новый Worker для другой машины |
| 30 | Internal subagent отображается activity, child Worker отсутствует | activity stream и server state без child Worker |
| 31 | `retry_attempt` только после terminal `retryable`, uncertain не retry-ится | outcome classification и Attempt sequence |
| 32 | Повтор inbound/action/event/Result не дублирует state | idempotency keys и counts до/после replay |
| 33 | Revoked Client не читает и не меняет state | revoked credential получает 401/403 |
| 34 | Revoked Node не принимает Dispatch | revoke response и rejected dispatch |
| 35 | Offline Node оставляет Worker видимым и понятным | Web/Telegram status `offline` и binding |
| 36 | Telegram Topic соответствует Worker без delta/raw-event spam | Topic mapping и агрегированные сообщения |
| 37 | Go, race, vet и frontend checks проходят | ссылка на automated gate log и commit |
| 38 | Реальные `fx`, Claude Code, Codex проходят; OpenCode отдельно conditional | по одному real run ID на harness |

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
