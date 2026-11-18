<img src="assets/icon.svg" width="64" height="64" alt="">

# Secretary v2

Установка и запуск: `bun install --cwd web`, затем `bun run start`. Сервер слушает `127.0.0.1:3000`. Для удалённого доступа нужен HTTPS через Tailscale Serve. Claude Code и Codex должны быть установлены и авторизованы.

SQLite хранится в `server/secretary.sqlite`. Настройки Secretary Agent создаются в `~/.secretary/` при первом запуске.

Чтобы команда `secretary` была доступна из терминала, выполните из корня проекта `mkdir -p ~/.local/bin && ln -s "$PWD/secretary" ~/.local/bin/secretary` (каталог `~/.local/bin` должен быть в `PATH`). Команды `secretary start`, `secretary stop` и `secretary status` управляют пользовательской службой systemd `secretary.service`; она должна быть установлена отдельно.

Для подключения устройства выполните `secretary pair` и введите полученный одноразовый код на странице входа. Код действует 10 минут и блокируется после 10 попыток; для нового кода повторите `secretary pair`. Для удалённого доступа используйте HTTPS; не публикуйте сервер напрямую без TLS.

Android-прототип: `cd web && bun install && bun run build && bunx cap sync android`; проект находится в `web/android`. Для сборки нужен Android SDK. Приложение открывает `https://app.scrtrbot.ru`; в настройках Android его можно выбрать приложением-помощником.
