<img src="assets/icon.svg" width="64" height="64" alt="">

# Secretary

**Один чат, один помощник.** Даёшь Secretary Agent задачу — с телефона по кнопке Power или из браузера, — и он её делает: сам или поручив Specialists, которые работают параллельно на твоём Host.

Установка и запуск: `bun install --cwd web`, затем `bun run start`. Сервер слушает `127.0.0.1:3000`. Для удалённого доступа нужен HTTPS: на omarchy сервер опубликован через Cloudflare Tunnel (`secretary-tunnel.service`) по адресу `app.scrtrbot.ru`. Устройство архитектуры описано в `docs/architecture.md`. Claude Code и Codex должны быть установлены и авторизованы.

SQLite и настройки Secretary Agent хранятся в `~/.secretary/`.

Чтобы команда `secretary` была доступна из терминала, выполните из корня проекта `mkdir -p ~/.local/bin && ln -s "$PWD/secretary" ~/.local/bin/secretary` (каталог `~/.local/bin` должен быть в `PATH`). Команды `secretary start`, `secretary stop` и `secretary status` управляют пользовательской службой systemd `secretary.service`; она должна быть установлена отдельно.

Для подключения устройства выполните `secretary pair` и введите полученный одноразовый код на странице входа. Код действует 10 минут и блокируется после 10 попыток; для нового кода повторите `secretary pair`. Для удалённого доступа используйте HTTPS; не публикуйте сервер напрямую без TLS.

Android-прототип: `cd web && bun install && bun run build && bunx cap sync android`; проект находится в `web/android`. Для сборки нужен Android SDK. Приложение открывает `https://app.scrtrbot.ru`; в настройках Android его можно выбрать приложением-помощником.

На этом компьютере SDK находится в `~/.local/share/android-sdk`, а JDK 21 в `~/.local/share/secretary-jdk`. Для debug-сборки из корня проекта: `JAVA_HOME="$HOME/.local/share/secretary-jdk" ANDROID_HOME="$HOME/.local/share/android-sdk" ./web/android/gradlew -p web/android :app:assembleDebug`. APK: `web/android/app/build/outputs/apk/debug/app-debug.apk`.

При вызове системного помощника приложение может открыться поверх экрана блокировки. Это раскрывает историю чатов и позволяет отправлять команды любому, у кого есть доступ к телефону. Доступность удержания кнопки питания зависит от модели и настроек Android. Обычный запуск поверх блокировки не разрешён.

Чтобы опубликовать новую веб-версию, выполните `bun run --cwd web build`. Сборка идёт отдельно, после успеха публикуются файлы и версия; неудачная сборка не заменяет текущий интерфейс. Открытые клиенты проверяют версию каждые 5 секунд и после возвращения в приложение, затем перезагружаются. Если в чате есть черновик или вложения, ожидается подтверждение отправки, открыты настройки либо редактируется профиль, обновление откладывается до завершения работы. Сначала установленный старый клиент необходимо обновить вручную один раз. Изменения сервера по-прежнему требуют его перезапуска.

Задачи ведутся в GitHub Issues.

### Голосовые сообщения

На Host с Linux x86_64 выполните `bash ~/.secretary/current/scripts/install-voice.sh` (в копии репозитория — `bash scripts/install-voice.sh`). Скрипт устанавливает sherpa-onnx 2.0.2 и две модели с проверкой SHA-256. Модель можно выбрать в настройках; по умолчанию используется GigaAM v3 e2e RNNT int8 (около 1 ГБ оперативной памяти), также доступна Parakeet TDT 0.6B v3 int8 (около 2 ГБ). Аудио обрабатывается локально. [GigaAM](https://huggingface.co/Smirnov75/GigaAM-v3-sherpa-onnx) — сторонняя конверсия по лицензии MIT; [Parakeet](https://huggingface.co/nvidia/parakeet-tdt-0.6b-v3) распространяется по CC BY 4.0, sherpa-onnx — по Apache 2.0.

Android-приложение с голосовым вводом нужно пересобрать: добавлено разрешение на микрофон.
