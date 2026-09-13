# Private Node deployment

`secretary-node` работает только как outbound-клиент. На MacBook и home server не нужно открывать входящий порт или настраивать port forwarding. Server должен быть доступен по приватному адресу, например HTTPS-адресу Tailscale.

## Установка

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

`--workspace` задаёт mapping `Project ID -> абсолютный путь`. Mapping отправляется в authenticated handshake и не меняется Worker envelope. `sex node setup` сохраняет только non-secret JSON-конфигурацию с правами `0600`.

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

`revoke` сначала ставит Node на drain, затем отзывает identity на server и останавливает локальный процесс. Для control-команд нужен отдельный `SECRETARY_NODE_ADMIN_TOKEN`. Client credential, Secretary runtime credential и Telegram token для этого не подходят.

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
