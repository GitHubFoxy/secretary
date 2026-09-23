# Pi Secretary client

Pi 0.87.1 загружает клиент как обычное TypeScript extension: `pi --extension <secretary.ts>` или из configured extensions. Команда `/secretary` позволяет выбрать Secretary либо существующего Worker и отправить сообщение напрямую серверу через Tailscale. Текст сообщения не поступает в Pi model.

## Documented grants

Обычный pairing default остаётся read-only: `conversation:read`, `worker:read`, `approval:read`. Для messaging extension owner должен явно создать и одобрить credential с ровно этими scopes:

- `conversation:read`
- `conversation:write`, только `POST /v1/messages`
- `worker:read`
- `worker:message`, только `POST /v1/workers/{worker_ref}/message`
- `approval:read`

`worker:write` не выдаётся. Cancel/stop/steer/respond/approve/close, Node, Client management, diagnostics и approval decisions не доступны extension credential.

Credential читается из `~/.config/secretary/viewer-credential` с режимом `0600`, без передачи секрета через аргументы команды. Для миграции старого Client отзовите его и выполните новый explicit-scope pairing по `docs/always-on-runbook.md`; затем безопасно замените файл credential.

## Live data boundary

Extension отображает Conversation body, ограниченные Worker status и имя инструмента с отметкой start/finish. Оно не отображает tool arguments, tool output, raw ACP, credentials, session IDs или reasoning.

## Документация

- Scope и API contract: `docs/pi-viewer.md`
- Pair/revoke procedure: `docs/always-on-runbook.md`
- Установка и восстановление: `docs/pi-viewer-runbook.md`
