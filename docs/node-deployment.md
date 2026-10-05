# Private Node deployment

`secretary-node` работает только как outbound-клиент. На MacBook и home server не нужно открывать входящий порт или настраивать port forwarding. Server должен быть доступен по приватному адресу, например HTTPS-адресу Tailscale.

## Установка

На Secretary server задайте отдельные Node credentials до запуска `secretaryd`:

```sh
export SECRETARY_NODE_PAIRING_TOKENS='одноразовый-token-macbook,одноразовый-token-home'
export SECRETARY_NODE_ADMIN_TOKEN='отдельный-node-admin-token'
```

Pairing token и admin token должны отличаться от Client bootstrap/credential, Secretary runtime credential и Telegram token. Не сохраняйте эти значения в Git или launchd plist.

На каждой машине выполните в корне репозитория:

```sh
./sex setup
./sex node setup \
  --server https://secretary.example.ts.net \
  --name macbook \
  --workspace frontend=/Users/me/src/frontend
```

На home server используйте другое имя и локальный путь:

```sh
./sex node setup \
  --server https://secretary.example.ts.net \
  --name home-server \
  --workspace frontend=/srv/src/frontend
```

`--workspace` задаёт mapping `Project ID -> абсолютный путь`. Server проверяет mapping против Project registry и сохраняет его в NodeRecord. При reconnect mapping остаётся durable, а dispatch использует только согласованный путь. Credentials не попадают в Worker envelope. `sex node setup` сохраняет только non-secret JSON-конфигурацию с правами `0600` и по умолчанию включает OpenCode probe. Если OpenCode не нужен, `sex node setup --exclude-opencode` задаёт explicit opt-out; старый config с `include_opencode:false` также сохраняет opt-out до rollout.

Node отмечает OpenCode ready только после version v2, auth status, native model metadata и успешного ACP `initialize`; версия CLI отдельно readiness не подтверждает. Probe запускает частный `serve --stdio --port 0`, ждёт регистрации config plugins и читает только metadata enabled models/variant settings в том же provider store, что ACP. `xhigh` не выводится из plaintext model IDs или имени variant. Probe не делает model call и не доказывает provider entitlement; exact model/effort и profile marker повторно проверяются на Start/Resume. Dispatch использует только observed HarnessInstance capabilities. В v2.0.22 tool execution работает, но ACP не передаёт explicit tool name, поэтому normalized tool call/result не объявляются.

Runtime и inventory используют один binary: `SECRETARY_OPENCODE_COMMAND`, если он задан, иначе `opencode` из service PATH. Отсутствующий explicit binary не подменяется другим из PATH. На omarchy v2.0.22 находится в `~/.local/bin/opencode`; `/usr/bin/opencode` — другая, старая версия. Проверить binary и service PATH перед rollout, не считая интерактивный `opencode --version` доказательством версии Node.

## Native state OpenCode

На чистой установке `sex setup` закрепляет разные persistent stores Secretary и local Worker Node; `sex node setup` закрепляет `<NODE_DATA>/opencode-native` для выбранного Node и инициализирует только новые private native DB без входа в provider. Это `XDG_DATA_HOME`; native DB/auth/WAL/SHM находятся под `opencode/`. Worker ACP runtime, actual Node model inventory/probes и `sex node doctor` используют именно этот путь. Каталоги имеют режим `0700`, файлы — `0600` под `umask 077`. Store находится вне Workspace и сохраняется при повторном setup, restart, reconnect, upgrade, Attempt и Follow-up. Другой Node использует собственный data directory. Secretary runtime на Secretary host имеет отдельный store `$HOME/.local/share/secretary/opencode-native`.

`sex node setup` и `sex setup` не запускают provider login. Owner может отдельно выполнить `sex node opencode login` на целевом Node; для Secretary server используется `sex opencode login`. Оба запускают OpenCode auth flow с точным выбранным `XDG_DATA_HOME` и изолированными HOME/config/state directories. Provider auth не копируется из обычной OpenCode DB и не передаётся через CLI args, prompt, Worker envelope или diagnostics; host API key/token/cloud credential environment очищается перед каждым OpenCode process. Только явно заданные narrow MCP server environment variables передаются конкретному MCP process. Если auth отсутствует, Doctor/inventory показывает это явно; OpenCode не переключается на другую базу или harness.

Для старого Secretary installation (существует `secretary.db`) либо старого Node state без `opencode-native-selection.json` setup/runtime закрепляет прежний `XDG_DATA_HOME` (если он не задан — `$HOME/.local/share`) и сохраняет legacy sessions. Старый server DB также переводит local Worker Node в legacy selection, если у неё ещё нет собственного record. Startup сообщает о migration requirement, `sex node doctor` блокирует readiness, а `sex node opencode login` отказывается перенаправлять owner в новую пустую базу. До отдельного owner approval нет backup/restore, копирования DB/credentials, сброса базы или изменения native session IDs. В том числе историческое удаление DB на omarchy не считается доказательством её corruption. Managed-mode race OpenCode v2.0.22 исправлена ticket31; этот старый диагноз не является текущим blocker.

Local setup/runtime/probe/Doctor/login regressions и private unpaid native HTTP fixtures реализованы в [ticket 33](../.scratch/phase-4/issues/33-isolate-opencode-native-state.md). Actual provider login, authenticated live acceptance и production rollout не выполнялись.

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
