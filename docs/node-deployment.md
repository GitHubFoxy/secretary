# Private Node deployment

`secretary-node` работает только как outbound-клиент. На MacBook и home server не нужно открывать входящий порт или настраивать port forwarding. Server должен быть доступен по приватному адресу, например HTTPS-адресу Tailscale.

## Установка

На Secretary server задайте отдельные Node credentials до запуска `secretaryd`:

```sh
export SECRETARY_NODE_PAIRING_TOKENS='одноразовый-token-macbook,одноразовый-token-home'
export SECRETARY_NODE_ADMIN_TOKEN='отдельный-node-admin-token'
```

Pairing token и admin token должны отличаться от Client bootstrap/credential, Secretary runtime credential и Telegram token. Не сохраняйте эти значения в Git или launchd plist.

На Secretary host выполните setup один раз, затем при необходимости настройте co-located Node:

```sh
./sex setup
sex opencode login
./sex node setup \
  --server https://secretary.example.ts.net \
  --name secretary-host \
  --workspace frontend=/Users/me/src/frontend
```

`sex opencode login` авторизует один native store, общий для Secretary и co-located Node. Не запускайте второй login через `sex node opencode login` на той же машине.

На отдельном home server настройте автономный remote Node; его native store будет локальным и отдельным:

```sh
./sex node setup --standalone \
  --server https://secretary.example.ts.net \
  --name home-server \
  --workspace frontend=/srv/src/frontend
sex node opencode login
```

`--workspace` задаёт mapping `Project ID -> абсолютный путь`. Server проверяет mapping против Project registry и сохраняет его в NodeRecord. При reconnect mapping остаётся durable, а dispatch использует только согласованный путь. Credentials не попадают в Worker envelope. `sex node setup` сохраняет только non-secret JSON-конфигурацию с правами `0600` и по умолчанию включает OpenCode probe. Флаг `--standalone` сохраняется в config; Node runtime и Doctor/login валидируют его через Go selection path и отказываются запускаться, если standalone Node ссылается на shared Secretary store. Для co-located Node setup/login/Doctor `sex` сравнивает проверенные Go records: canonical `data_dir`, scope, mode и store path с Secretary — до native CLI. Deployment config и manifests разбираются одним строгим Go decoder’ом: duplicate/escaped/case aliases и malformed JSON отклоняются, Shell не выбирает значения через awk. Standalone Node остаётся независимым и использует собственный `data_dir`/store. Если OpenCode не нужен, `sex node setup --exclude-opencode` задаёт explicit opt-out; старый config с `include_opencode:false` также сохраняет opt-out до rollout.

Node отмечает OpenCode ready только после version v2, stored credential metadata, native model inventory и успешного ACP `initialize`; версия CLI отдельно readiness не подтверждает. Authentication step использует поддержанный v2.0.22 вызов `opencode auth list --format json --standalone` в точно выбранном `XDG_DATA_HOME`. Он принимает только exit code 0 и корректный JSON с `connections[].type = "credential"`; обычный текст, пустой/невалидный JSON, ошибка и credentials только из environment не дают ready. Standalone запускает private server вместо background service; шаг ограничен timeout. Secretary и Node `sex doctor` используют Go auth helper с тем же выбранным store и выводят только readiness status, не сырой JSON.

Модельный probe запускает частный `serve --stdio --port 0`, ждёт регистрации config plugins и читает только metadata enabled models/variant settings в том же provider store, что ACP. `xhigh` не выводится из plaintext model IDs или имени variant. Probe не делает model call и не доказывает provider entitlement; exact model/effort и profile marker повторно проверяются на Start/Resume. Dispatch использует только observed HarnessInstance capabilities. В v2.0.22 tool execution работает, но ACP не передаёт explicit tool name, поэтому normalized tool call/result не объявляются.

Runtime и inventory используют один binary: `SECRETARY_OPENCODE_COMMAND`, если он задан, иначе `opencode` из service PATH. Отсутствующий explicit binary не подменяется другим из PATH. На omarchy v2.0.22 находится в `~/.local/bin/opencode`; `/usr/bin/opencode` — другая, старая версия. Проверить binary и service PATH перед rollout, не считая интерактивный `opencode --version` доказательством версии Node.

## Native state OpenCode

На чистой установке `sex setup` выбирает один persistent store `$HOME/.local/share/secretary/opencode-native` для Secretary и co-located Worker Node. Он создаёт native DB настоящим `opencode serve --port 0 --stdio` с закрытым stdin, проверяет `opencode.db` и прекращает setup с видимой ошибкой при сбое; auth/login/provider call не выполняются. Локальный `sex node setup` закрепляет тот же path, не создавая вторую native DB. Отдельный Node на другой машине запускается с `--standalone` и выбирает собственный `<NODE_DATA>/opencode-native`. Каждый путь используется как `XDG_DATA_HOME`; native DB/auth/WAL/SHM находятся под `opencode/`. Runtime, model inventory/probes, title generator и Doctor co-located installation используют общий store; remote Node использует свой локальный. Каталоги имеют `0700`, файлы — `0600` под `umask 077`. Store находится вне Workspace и сохраняется при повторном setup, restart, reconnect, upgrade, Attempt и Follow-up.

`sex node setup` и `sex setup` не запускают provider login. На Secretary host один явный `sex opencode login` авторизует общий Secretary/local Node store; для автономного remote Node owner запускает `sex node opencode login` на его машине. Обе команды используют точный выбранный `XDG_DATA_HOME` и изолированные HOME/config/state directories. Provider auth не копируется из обычной OpenCode DB и не передаётся через CLI args, prompt, Worker envelope или diagnostics; host API key/token/cloud credential environment очищается перед каждым OpenCode process. Только явно заданные narrow MCP server environment variables передаются конкретному MCP process. Если auth отсутствует, Doctor/inventory показывает это явно; OpenCode не переключается на другую базу или harness.

Старая установка (существует `secretary.db` или прежний `config.toml`) либо Node config/state без `opencode-native-selection.json` сохраняют прежний `XDG_DATA_HOME` (если он не задан — `$HOME/.local/share`) и получают migration requirement. Старый fx-only setup не переключается автоматически. Перед переходом остановите Secretary и co-located Node, убедитесь, что нет managed OpenCode sessions, затем запустите `sex opencode select-shared-store`. Команда проверяет Node session mappings, конфликтующие selections и непустой target, идемпотентно записывает два shared selection manifests и не переносит credentials/history, не меняет session IDs или fx state. При конфликте/частичной записи переход завершается fail-closed; повторите после ручной проверки только если target остаётся пустым. После выбора общего store выполните один `sex opencode login`. Удалённый Node не включайте в этот переход: он сохраняет собственный `<NODE_DATA>/opencode-native`. Историческое удаление DB на omarchy не считается доказательством corruption; managed-mode race v2.0.22 исправлена ticket31.

Local setup/runtime/probe/Doctor/login regressions проверяют duplicate deployment fields, обычное расхождение co-located stores и standalone independence; Go отклоняет неоднозначный config до native CLI. Private unpaid native HTTP fixtures используют DB, созданную реальным public `sex setup`, и реализованы в [ticket 33](../.scratch/phase-4/issues/33-isolate-opencode-native-state.md). Actual provider login, authenticated live acceptance и production rollout не выполнялись.

## Pairing и запуск

На сервере заранее создайте отдельный одноразовый `SECRETARY_NODE_PAIRING_TOKEN` для каждого Node. Передайте token только первой команде запуска:

```sh
export SECRETARY_NODE_PAIRING_TOKEN='извлечённый-локально-token'
./sex node start
unset SECRETARY_NODE_PAIRING_TOKEN
```

После pairing identity и per-Node transport credential сохраняются в `$HOME/.local/share/secretary/node/data/identity.json`. Повторный запуск использует этот credential и не требует старого pairing token.

У Node нет параметра listen и нет callback URL. Reconnect после рестарта выполняется исходящим WebSocket-соединением к server. Старый Attempt не запускается повторно, а Worker остаётся привязан к исходному Node.

## Lifecycle

```sh
sex node status
sex node logs
sex node doctor
sex node stop
sex node drain
sex node revoke
```

`revoke` сначала ставит Node на drain, затем опрашивает remote status до пустого `active_attempts`, и только после этого отзывает identity на server и останавливает локальный процесс. Таймаут bounded. При таймауте revoke отказывается по умолчанию. Явный `sex node revoke --force` разрешает принудительный отзыв и остановку активной работы. Для control-команд нужен отдельный `SECRETARY_NODE_ADMIN_TOKEN`. Client credential, Secretary runtime credential и Telegram token для этого не подходят.

Для запуска после входа в macOS:

```sh
sex node install-service
sex node uninstall-service
```

LaunchAgent не содержит pairing token, Node credential или других секретов. На home server используйте системный supervisor, который запускает `sex node serve`; он также не должен содержать token в unit-файле.

## Credential boundaries

Используются четыре независимых секрета:

- Node transport credential, только `secretary-node` и Node protocol;
- Client credential, только Web, Pi или другой доверенный Client;
- Secretary runtime credential, только `secretaryd`;
- Telegram bot token, только Telegram adapter.

Они не записываются в Worker envelope, Profiles, Conversation, activity, diagnostic export или обычные логи. Bootstrap token нужен только для первоначального Web pairing. Не помещайте его в Git, issue, launchd plist или командный журнал.

`sex setup` явно печатает: `Full-access trusted Node policy: explicit and enabled for local Node only.` Это trusted-local policy, а не sandbox boundary. Для remote Node auto-approval не включается.
