# Проверка страниц истории

Стенд работает только с синтетическими данными и очищает localStorage открытой вкладки. Используйте отдельный профиль браузера и свободные порты.

1. В `web/` выполните `bun install --frozen-lockfile` и `./node_modules/.bin/vite build --outDir /tmp/sec101-build --emptyOutDir`.
2. Из корня проекта запустите `HISTORY_TEST_BUILD=/tmp/sec101-build bun web/browser-tests/history-server.js`.
3. Откройте `http://127.0.0.1:5198/` в отдельном профиле Helium с CDP на порту 9782.
4. Выполните `bun web/browser-tests/history-check.js`.

Порты и адреса можно изменить переменными `HISTORY_TEST_PORT`, `HISTORY_TEST_SERVER` и `HISTORY_TEST_CDP`. Тест использует первую вкладку CDP и должен подключаться только к выделенному браузеру.
