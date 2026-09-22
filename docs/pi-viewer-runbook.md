# Pi viewer runbook (MacBook Air)

Повторяемый setup Pi read-only viewer на MacBook Air. Серверная сторона (pairing, revoke, systemd, backup): `docs/always-on-runbook.md`. Контракт: `docs/pi-viewer.md`.

## Предпосылки

- MacBook Air в том же tailnet, `tailscale status` показывает сервер.
- Клон `phase-1` этого репозитория на Air собран (bundle в `phase-1/packages/coding-agent/dist/bundle/cli.js`). Установленный из mise `pi` не содержит команды `--experimental secretary` и для viewer не подходит, поэтому команды ниже идут через bundle с `PI_EXPERIMENTAL=1`.
- Viewer credential получен по flow из `docs/always-on-runbook.md` (раздел "Viewer pairing"). Bootstrap token, Node credential и доступ к SQLite для viewer не нужны и на Air не хранятся.

## Credential storage

Каталог и файл создаются так:

```sh
mkdir -m 0700 -p ~/.config/secretary
install -m 0600 /dev/null ~/.config/secretary/viewer-credential
```

Содержимое кладёт server-side flow из `docs/always-on-runbook.md`: значение credential идёт пайпом через Tailscale SSH (`printf '%s' "$credential" | ssh <air> 'cat > ...'`) и перезаписывает этот файл, сохраняя режим `0600`. В терминал credential не выводится ни на сервере, ни на Air.

- `~/.config/secretary` — режим `0700`, сам файл `viewer-credential` — `0600`.
- В файле только client credential. Не класть сюда bootstrap token, pending token, Node credential или cookies.
- Ledger и логи содержат имя файла, но не его содержимое.

## Snapshot команда (после reboot)

Одна команда, которая проверяет всё: Tailscale, сервер, credential и read surface:

```sh
REPO=~/projects/secretary-v2   # клон репозитория на Air
env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
  PI_EXPERIMENTAL=1 node "$REPO/phase-1/packages/coding-agent/dist/bundle/cli.js" secretary --once \
  --base-url https://<host>.<tailnet>.ts.net \
  --credential-file ~/.config/secretary/viewer-credential
```

Команда печатает состояние Secretary, хвост Conversation, Workers с результатами и pending Approvals, затем завершается. Ошибки: `401` или `403` означают отсутствующий или отозванный credential, таймаут — недоступность сервера или Tailscale, `fetch failed` — в окружении задан прокси (`HTTPS_PROXY`/`ALL_PROXY`), который bundle-транспорт берёт на себя: снимите эти переменные через `env -u`, как в команде выше.

## Interactive TUI

```sh
REPO=~/projects/secretary-v2
env -u HTTP_PROXY -u HTTPS_PROXY -u ALL_PROXY -u http_proxy -u https_proxy -u all_proxy \
  PI_EXPERIMENTAL=1 node "$REPO/phase-1/packages/coding-agent/dist/bundle/cli.js" secretary \
  --base-url https://<host>.<tailnet>.ts.net \
  --credential-file ~/.config/secretary/viewer-credential
```

Состояния связи, которые обязан показывать TUI:

| State | Условие |
| --- | --- |
| `connected` | snapshot загружен, live подписки открыты |
| `reconnecting` | подписка потеряна, повтор с последним cursor |
| `offline` | сервер или Tailscale недоступны после retries, виден последний snapshot |
| `revoked` | credential отклонён (`401`, `403`, `1008 Client revoked`), reconnect остановлен |

## Recovery

### Server offline

- Viewer symptom: state `offline`, показывается последний snapshot с временем чтения; snapshot команда завершается таймаутом или connection error.
- Server diagnosis (на always-on Laptop): `systemctl --user status secretaryd.service`, `journalctl --user -u secretaryd -n 50 --no-pager`, `curl -fsS http://127.0.0.1:8081/v1/health`.
- Восстановление: `systemctl --user restart secretaryd.service`. Viewer переподключается сам, отдельных действий на Air не нужно.

### Node offline

- Viewer symptom: сервер отвечает, state `connected`, но Worker'ы показывают `offline` или stale status. Control action с viewer невозможен и не нужен: viewer читает состояние, чинить его должен сервер.
- Server diagnosis (на always-on Laptop): `systemctl --user status secretary-node.service`, `journalctl --user -u secretary-node -n 50 --no-pager`, `sex node status`, при необходимости `systemctl --user restart secretary-node.service`.
- Из Pi запрещено пытаться читать `GET /v1/workers/{worker_ref}/diagnostics`: для Client credential этот endpoint возвращает `403` и не входит в viewer surface. Diagnostics — только owner web session на сервере.

### Tailscale unavailable

- Viewer symptom: state `offline`, `--once` не может достучаться до `https://<host>.<tailnet>.ts.net`.
- На Air: `tailscale status`, `tailscale ping --timeout=8s <server-host>`, при выходе из сети `tailscale up`.
- На сервере: `tailscale status`, `tailscale serve status` (пустой serve = `tailscale serve --bg http://127.0.0.1:8081`), `tailscale ping <viewer-host>`.

### Revoked credential

- Viewer symptom: state `revoked`, reconnect остановлен, `--once` получает `401`.
- Сервер: revoke уже выполнен, новых подключений не будет. Повторный viewer pairing по `docs/always-on-runbook.md`, затем положить новый credential в тот же файл `0600`.
- Старый credential не восстанавливается, streams закрыты по `1008 Client revoked`.

## Ограничения viewer

Viewer не является remote control: он только читает состояние. Write-команды (`--message`, `--approve`, `--deny` и worker actions), Node control, `client:manage` и bootstrap token для viewer недоступны. Полный список: `docs/pi-viewer-release-note.md`.
