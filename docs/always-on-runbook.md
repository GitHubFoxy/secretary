# Always-on server runbook

Runbook для always-on Arch Linux сервера (omarchy), где постоянно работают `secretaryd` и Execution Node под systemd user-юнитами. Команды не содержат секретов: bootstrap token, capability, pairing и admin token задаются только через переменные окружения в `~/.local/share/secretary/environment` (mode 600) и не попадают в unit files, в эту документацию или в Git.

## Topology

- `secretaryd` слушает только loopback: `-listen 127.0.0.1:8081`. Старт с wildcard или LAN-адресом (`0.0.0.0:8081`, `:8081`, `192.168.x.x`) завершается ошибкой: `validateListen` в `cmd/secretaryd` это проверяет.
- Tailscale Serve публикует HTTPS поверх loopback:

  ```sh
  tailscale serve --bg http://127.0.0.1:8081
  tailscale serve status
  ```

- Tailscale ACL ограничивает доступ к HTTPS-порту owner-устройствами. Пример правила в админ-консоли, адаптируйте под свой tailnet:

  ```json
  { "acls": [{ "action": "accept", "src": ["group:owner"], "dst": ["tag:secretary:443"] }] }
  ```

- Serve публикует весь `/v1` и статику User UI (вариант B из `docs/pi-viewer.md`): routes закрыты credential-ами, Control Room и `/v1/control/*` отдаются только при `-debug` и возвращают `404` без него.
- Порт `8081` не открывается в firewall наружу: наружу смотрит только Tailscale.

## Data directory

Основной каталог: `~/.local/share/secretary`.

| Путь | Содержимое |
| --- | --- |
| `secretary.db` | SQLite с durable state: Conversations, Workers, Approvals, Clients |
| `config.toml`, `user.md` | конфигурация моделей и профилей, документ владельца |
| `environment` | bootstrap, pairing и admin token (mode 600, только здесь) |
| `logs/acp` | raw ACP-логи Worker'ов с retention-политикой |
| `node/config.json`, `node/data` | состояние outbound Node |
| `server.pid`, `server.mode` | runtime-файлы launcher (пишутся и под systemd) |

Логи процессов под systemd уходят в journal, а не в `secretaryd.log` / `secretary-node.log` (эти файлы пишет только foreground-запуск через `sex start` / `sex node start`).

## Services

User-юниты: `~/.config/systemd/user/secretaryd.service` и `secretary-node.service`.

- `secretaryd`: буквальный рабочий unit:

  ```ini
  [Unit]
  Description=Secretary server (secretaryd)
  After=network.target

  [Service]
  Type=simple
  ExecStart=/usr/bin/zsh /home/coder/projects/secretary-v2/sex serve
  Restart=on-failure
  RestartSec=3
  Environment=PATH=/home/coder/.local/bin:/home/coder/.local/share/mise/shims:/usr/local/bin:/usr/bin:/bin

  [Install]
  WantedBy=default.target
  ```

  В `ExecStart` только абсолютные пути: systemd не обязан раскрывать `~`.
- Node: `ExecStart=/usr/bin/zsh /home/coder/projects/secretary-v2/sex node serve` с `Wants=` и `After=secretaryd.service`. Жёсткого `Requires=` нет: Node умеет reconnect при рестарте сервера.
- Оба: `Restart=on-failure`, `WantedBy=default.target`. Секретов в юнитах нет.
- Linger включён (`loginctl enable-linger coder`, проверка: `loginctl show-user coder`), поэтому юниты поднимаются после reboot без входа пользователя.

```sh
systemctl --user enable --now secretaryd.service secretary-node.service
systemctl --user is-active secretaryd.service secretary-node.service
```

Первый enrollment Node выполняется один раз вручную с одноразовым pairing token, дальше юнит работает без токена:

```sh
source ~/.local/share/secretary/environment
SECRETARY_NODE_PAIRING_TOKEN="$SECRETARY_NODE_PAIRING_TOKEN" \
  ~/.local/bin/secretary-node --config ~/.local/share/secretary/node/config.json &
# дождаться "paired Node <name>", остановить процесс, затем enable --now secretary-node.service
```

Pairing token lifecycle: токен одноразовый и после enrollment durably marked consumed в базе сервера (`EnrollNodeWithPairing`), повторное использование отклоняется, рестарт сервера его не перевооружает (`INSERT OR IGNORE`). Токен остаётся в `environment`, потому что сервер требует pairing tokens при каждом старте: без них при заданном admin token `secretaryd` завершается с `log.Fatal`. Юнит Node этот файл не читает (`node_serve` не делает `load_environment`), поэтому хранение токена не передаёт его Node. При желании spent-токен можно заменить свежим (ротация под будущие Node), но удалять переменную совсем нельзя.

## Управление под systemd

Процессами управляет только systemd. `sex stop` / `sex start` / `sex restart` / `sex node start` под systemd не использовать: они убивают процесс за спиной systemd по pid-файлу, и `Restart=on-failure` может тут же поднять его обратно.

```sh
systemctl --user stop secretaryd.service secretary-node.service
systemctl --user start secretaryd.service secretary-node.service
systemctl --user restart secretaryd.service secretary-node.service
systemctl --user status secretaryd.service secretary-node.service
journalctl --user -u secretaryd -u secretary-node -n 50 --no-pager
```

Для инспекции годятся `sex status`, `sex node status`, `sex doctor` (читают pid-файлы и конфиг, процессами не управляют).

Debug-режим (с Control Room): остановить юнит и запустить foreground вручную:

```sh
systemctl --user stop secretaryd.service
zsh ~/projects/secretary-v2/sex serve --debug   # Ctrl-C по окончании
systemctl --user start secretaryd.service
```

После `git pull` с изменениями `sex` или Go-кода:

```sh
cd ~/projects/secretary-v2 && zsh ./sex setup   # пересборка бинарей, проверка harness
systemctl --user daemon-reload                  # только если менялись сами unit files
systemctl --user restart secretaryd.service secretary-node.service
sex doctor
```

## Viewer pairing (Pi read-only credential)

Ручной flow выдачи read-only credential для Pi viewer на MacBook Air. Bootstrap token берётся только из `environment` и не записывается в ledger, документацию или вывод на Air. Для pairing не нужны SQLite и Node credential.

Все Client mutations требуют idempotency key: в заголовке `Idempotency-Key` или в поле body `idempotency_key` (при заданных обоих значения должны совпадать, иначе `400 idempotency key in body and header must match`). Без ключа сервер отвечает `400 idempotency key is required`. Это штатный mutation contract, а не сбой сервера. `POST /v1/clients/{id}/approve` требует `Idempotency-Key` наравне с остальными mutations.

```sh
source ~/.local/share/secretary/environment
API=http://127.0.0.1:8081

# 1. Pair. Явный scope list обязателен: omitted, null и [] дают 400
pair=$(curl -fsS -X POST -H 'Content-Type: application/json' \
  -d "{\"bootstrap_token\":\"$SECRETARY_BOOTSTRAP_TOKEN\",\"device_id\":\"mba-viewer\",\"display_name\":\"Pi viewer\",\"platform\":\"pi\",\"scopes\":[\"conversation:read\",\"worker:read\",\"approval:read\"],\"idempotency_key\":\"mba-viewer-pair\"}" \
  "$API/v1/clients/pair")
client_id=$(printf '%s' "$pair" | python3 -c 'import json,sys; print(json.load(sys.stdin)["client_id"])')
# pending_token из ответа не сохранять: он держится в памяти Pi до approve/redeem

# 2. Owner web session
curl -fsS -c /tmp/owner.jar -X POST -H 'Content-Type: application/json' \
  -d "{\"bootstrap_token\":\"$SECRETARY_BOOTSTRAP_TOKEN\"}" "$API/v1/web/session"

# 3. Approve. Idempotency-Key обязателен, без него 400 idempotency key is required.
# Ответ уходит в shell-переменную, credential из неё извлекается без stdout:
# в scrollback и в журнал команд он не попадает.
approve=$(curl -fsS -b /tmp/owner.jar -X POST -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: mba-viewer-approve' -d '{}' \
  "$API/v1/clients/$client_id/approve")
credential=$(printf '%s' "$approve" | python3 -c 'import json,sys; print(json.load(sys.stdin)["credential"])')
unset approve pair
rm -f /tmp/owner.jar
```

Credential живёт только в shell-переменной `credential`, в stdout не выводится. Передача на Air идёт приватным каналом (Tailscale SSH) пайпом, тоже без вывода в терминал. Файл на Air должен быть создан заранее через `install -m 0600` (см. `docs/pi-viewer-runbook.md`), тогда `cat >` перезапишет содержимое и сохранит режим `0600`:

```sh
# на Air (один раз, до передачи)
mkdir -m 0700 -p ~/.config/secretary
install -m 0600 /dev/null ~/.config/secretary/viewer-credential

# на сервере: credential в переменной, stdout не используется
printf '%s' "$credential" | ssh <air> 'cat > "$HOME/.config/secretary/viewer-credential"'
unset credential
```

После передачи переменная сбрасывается. `credential` не записывается в ledger, логи, документацию или вывод на Air.

Transfer предполагает Tailscale SSH. Если capability выключена (`tailscale status --json | jq '.Self.CapMap.SSH'` пусто) и на Air не слушает порт 22, канала omarchy→Air нет. Тогда весь flow выполняется с Air, transfer не нужен:

```sh
# bootstrap в shell-переменную, в stdout не выводится
bootstrap=$(ssh omarchy 'source ~/.local/share/secretary/environment && printf %s "$SECRETARY_BOOTSTRAP_TOKEN"')
# pair, owner session и approve — на https://<host>.<tailnet>.ts.net/v1 (curl с --noproxy '*', если задан прокси),
# capture в переменные так же, как выше
# запись на Air без вывода в терминал:
mkdir -m 0700 -p ~/.config/secretary
install -m 0600 /dev/null ~/.config/secretary/viewer-credential
printf '%s' "$credential" > ~/.config/secretary/viewer-credential
unset credential bootstrap
```

Запись в уже созданный install'ом файл сохраняет режим `0600`.

Revoke выполняет owner и тоже требует idempotency key:

```sh
curl -fsS -c /tmp/owner.jar -X POST -H 'Content-Type: application/json' \
  -d "{\"bootstrap_token\":\"$SECRETARY_BOOTSTRAP_TOKEN\"}" "$API/v1/web/session"
curl -fsS -b /tmp/owner.jar -X POST -H 'Content-Type: application/json' \
  -H 'Idempotency-Key: mba-viewer-revoke' -d '{}' \
  "$API/v1/clients/$client_id/revoke"
rm -f /tmp/owner.jar
```

После revoke активные streams закрываются (`1008 Client revoked`), новые HTTP и WS запросы получают `401`, повторное подключение требует нового pairing. Viewer credential не даёт доступа к SQLite, Node credential, bootstrap token после pairing, `client:manage`, `user:write` и write routes. Air-сторона flow: `docs/pi-viewer-runbook.md`.

## Backup

`sqlite3 .backup` безопасен при работающем сервере, включая WAL:

```sh
mkdir -p ~/.local/share/secretary/backups
sqlite3 ~/.local/share/secretary/secretary.db \
  ".backup '$HOME/.local/share/secretary/backups/secretary-$(date +%Y%m%d-%H%M%S).db'"
ls -1t ~/.local/share/secretary/backups | head   # ротация: оставляйте последние 7
```

## Restore-check

Проверка, что бэкап читается, выполняется в отдельной temporary directory и не трогает рабочую базу. `secretary-migrate` лежит рядом с `secretaryd` в `~/.local/bin`:

```sh
tmp=$(mktemp -d)
cp ~/.local/share/secretary/backups/<файл>.db "$tmp/source.db"
mkdir -p "$tmp/restored"
secretary-migrate -source "$tmp/source.db" -destination "$tmp/restored/secretary.db"
sqlite3 "$tmp/restored/secretary.db" "PRAGMA integrity_check;"
sqlite3 "$tmp/restored/secretary.db" \
  "SELECT 'conversations', count(*) FROM conversations;
   SELECT 'conversation_entries', count(*) FROM conversation_entries;
   SELECT 'results', count(*) FROM results;
   SELECT 'workers', count(*) FROM workers;
   SELECT 'phase4_results', count(*) FROM phase4_results;"
rm -rf "$tmp"
```

JSON-отчёт `secretary-migrate` содержит счётчики `Workers`, `Turns`, `Attempts`, `Results`. Restore-check пройден, когда `integrity_check` возвращает `ok`, счётчики выше нуля на рабочей базе, а таблицы `conversations`, `results` и `phase4_results` читаются. Миграция в destination заодно создаёт таблицы текущей схемы (`workers`, `phase4_*`), которых может не быть в копии, открывавшейся старой версией кода.

Полный restore: `systemctl --user stop secretaryd.service secretary-node.service`, скопируйте бэкап в `secretary.db` (удалите соседние `secretary.db-wal` и `secretary.db-shm`), `systemctl --user start secretaryd.service secretary-node.service`, затем `sex doctor`.

## Health checks

```sh
curl -fsS http://127.0.0.1:8081/v1/health                      # {"status":"ok"}
curl -fsS https://<machine>.<tailnet>.ts.net/v1/health          # через Tailscale Serve
```

`GET /v1/health` не требует credential, возвращает только поле `status` и отвечает `503`, если SQLite недоступна. Domain-данных в ответе нет, поэтому его можно публиковать через Serve.

## Tailscale diagnosis

```sh
tailscale status                                  # пиринг, IP и активность
tailscale serve status                            # HTTPS proxy должен вести на 127.0.0.1:8081
tailscale ping --timeout=8s <viewer-host>         # до MacBook Air
curl -fsS https://<host>.<tailnet>.ts.net/v1/health
```

Если `tailscale serve status` пуст, публикация не поднята:

```sh
tailscale serve --bg http://127.0.0.1:8081
```

ACL в админ-консоли Tailscale должен разрешать owner-устройствам доступ к HTTPS-порту (см. Topology). Viewer-машина должна быть в том же tailnet и видима по `tailscale status`.

## Restart и восстановление

- Перезапуск server: `systemctl --user restart secretaryd.service`.
- Node не подключился: `systemctl --user restart secretary-node.service`, затем `journalctl --user -u secretary-node`.
- Состояние durable state проверяется restore-check-запросами выше плюс journal.

## Negative surface

Автотесты фиксируют, что read-only (Pi) credential получает отказ вне read surface:

```sh
go test ./internal/webapi ./cmd/secretaryd
```

Покрыто: `/v1/nodes/connect` (Node protocol), `/v1/internal/secretary/tools/call`, `/v1/telegram/pairing`, `/v1/control/*`, `/v1/clients/*` с `client:manage` и все write routes. `GET /v1/bootstrap` остаётся доступен по `conversation:read` и в negative tests не входит (см. `docs/pi-viewer.md`).

## Ручные проверки acceptance (после reboot и на реальном железе)

1. Reboot сервера без входа пользователя (linger включён): `systemctl --user is-active secretaryd.service secretary-node.service` показывает `active`.
2. `curl -fsS https://<machine>.<tailnet>.ts.net/v1/health` с MacBook Air возвращает `{"status":"ok"}`.
3. Реальный fx HarnessInstance становится ready после restart: `sex doctor` без проблем.
4. Restore-check на копии свежего бэкапа возвращает `ok` и ненулевые счётчики.

## macOS-вариант (не основной)

На macOS вместо systemd используются launchd-сервисы:

```sh
sex install-service        # server: ~/Library/LaunchAgents/dev.secretary.sex.plist
sex node install-service   # node:   ~/Library/LaunchAgents/dev.secretary.sex-node.plist
```

Оба plist содержат `RunAtLoad` и `KeepAlive`, секретов в них нет. LaunchAgent стартует после входа пользователя в систему: чтобы сервер поднимался после reboot без действий, включите auto-login. Логи: `sex logs`, управление: `sex start/stop/restart`.
