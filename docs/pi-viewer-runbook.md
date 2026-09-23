# Pi Secretary extension runbook

Загрузка стандартного TypeScript extension в Pi 0.87.1. Никаких `PI_EXPERIMENTAL` или изменённых Pi CLI не требуется. Серверный pairing/revoke: `docs/always-on-runbook.md`; scope contract: `docs/pi-viewer.md`.

## Setup

Mac должен быть подключён к tailnet. Создайте каталог и credential file с режимами `0700` и `0600`:

```sh
mkdir -m 0700 -p ~/.config/secretary
install -m 0600 /dev/null ~/.config/secretary/viewer-credential
```

Owner должен одобрить Client с точными scopes `conversation:read`, `conversation:write`, `worker:read`, `worker:message`, `approval:read`. Обычный `defaultPairScopes` остаётся read-only. Pairing/revoke procedure описан в `docs/always-on-runbook.md`.

```sh
export SECRETARY_BASE_URL=https://<host>.<tailnet>.ts.net
pi --extension ~/projects/secretary-v2/phase-1/packages/coding-agent/src/extensions/secretary.ts
```

В Pi выполните `/secretary`, выберите Secretary или Worker и введите сообщение. Extension читает `~/.config/secretary/viewer-credential`, проверяет отсутствие group/other permissions и не передаёт сообщение Pi model. Можно задать другой файл через `SECRETARY_CLIENT_CREDENTIAL_FILE`.

## Live view and recovery

Виджет показывает Connection state, Conversation body, allowlisted Worker status и tool name со статусом start/finish. Он не показывает tool arguments/output, raw ACP или reasoning.

- `connected`: snapshot загружен, live subscriptions открыты.
- `reconnecting`: повторное подключение с cursor.
- `offline`: сервер/Tailscale недоступны.
- `revoked`: credential отклонён; выполните новый pairing после проверки и отзыва старого Client.

Server offline: проверить `systemctl --user status secretaryd.service`, логи и `/v1/health` на сервере. Node offline: проверить `secretary-node.service` и `sex node status`. Tailscale offline: `tailscale status`, `tailscale ping <server-host>`.

## Credential revoke и смена scope

Revoke Client выполняет owner, см. команду в `docs/always-on-runbook.md`. Для перехода с read-only grants на messaging grants недостаточно менять локальные scopes: отзовите прежний Client, создайте новый с указанным выше точным списком, одобрите его и безопасно замените файл credential. Не выдавайте `worker:write`.
