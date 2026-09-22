# Always-on server runbook

Runbook для Laptop, где постоянно работают `secretaryd` и Execution Node. Команды не содержат секретов: bootstrap token, capability, pairing и admin token задаются только через переменные окружения и не попадают в launchd plist, в эту документацию или в Git.

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
| `logs/acp` | raw ACP-логи Worker'ов с retention-политикой |
| `node/config.json`, `node/data` | состояние outbound Node |
| `server.pid`, `secretaryd.log`, `environment` | runtime-файлы локального launcher |

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

Полный restore: `sex stop`, скопируйте бэкап в `secretary.db` (удалите соседние `secretary.db-wal` и `secretary.db-shm`), `sex start`, затем `sex doctor`.

## Services после reboot

```sh
sex install-service        # server: ~/Library/LaunchAgents/dev.secretary.sex.plist
sex node install-service   # node:   ~/Library/LaunchAgents/dev.secretary.sex-node.plist
```

Оба plist содержат `RunAtLoad` и `KeepAlive`, секретов в них нет. LaunchAgent стартует после входа пользователя в систему: чтобы сервер поднимался после reboot без действий, включите auto-login.

Проверка:

```sh
sex status
sex node status
sex doctor
```

## Health checks

```sh
curl -fsS http://127.0.0.1:8081/v1/health                      # {"status":"ok"}
curl -fsS https://<machine>.<tailnet>.ts.net/v1/health          # через Tailscale Serve
```

`GET /v1/health` не требует credential, возвращает только поле `status` и отвечает `503`, если SQLite недоступна. Domain-данных в ответе нет, поэтому его можно публиковать через Serve.

## Restart и восстановление

- Перезапуск server: `sex restart` (normal) или `sex restart --debug` (с Control Room).
- Node не подключился: `sex node status`, затем `sex node stop && sex node start`.
- Состояние durable state проверяется restore-check-запросами выше плюс `sex logs`.

## Negative surface

Автотесты фиксируют, что read-only (Pi) credential получает отказ вне read surface:

```sh
go test ./internal/webapi ./cmd/secretaryd
```

Покрыто: `/v1/nodes/connect` (Node protocol), `/v1/internal/secretary/tools/call`, `/v1/telegram/pairing`, `/v1/control/*`, `/v1/clients/*` с `client:manage` и все write routes. `GET /v1/bootstrap` остаётся доступен по `conversation:read` и в negative tests не входит (см. `docs/pi-viewer.md`).

## Ручные проверки acceptance (после reboot и на реальном железе)

1. Reboot Laptop, вход в систему: `sex status` и `sex node status` показывают запущенные процессы.
2. `curl -fsS https://<machine>.<tailnet>.ts.net/v1/health` с MacBook Air возвращает `{"status":"ok"}`.
3. Реальный Codex или fx HarnessInstance становится ready после restart: `sex doctor`.
4. Restore-check на копии свежего бэкапа возвращает `ok` и ненулевые счётчики.
