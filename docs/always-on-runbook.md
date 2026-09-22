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
