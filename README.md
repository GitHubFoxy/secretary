<img src="assets/icon.svg" width="64" height="64" alt="">

# Secretary v2

Установка и запуск: `bun install --cwd web`, затем `bun run start`. Сервер слушает `127.0.0.1:3000`. Для удалённого доступа нужен HTTPS через Tailscale Serve. Claude Code и Codex должны быть установлены и авторизованы.

SQLite хранится в `server/secretary.sqlite`. Настройки Secretary Agent создаются в `~/.secretary/` при первом запуске.
