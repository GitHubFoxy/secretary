# Убрать название Worker topic перед Result в Telegram General

Status: ready-for-agent

## Запрос пользователя

На [скриншоте](evidence/reported-prefix.png) перед ответом «„Пачкать“ по-английски — по-разному…» стоит отдельная строка «как будет пачкать по английски ? чтобы было разнве...:». Пользователь указал, что эта строка должна исчезнуть (`SHOULD BE GONE`).

Трактовка: убрать автоматически добавленное название Worker topic из сообщения с Result в General. Сам ответ остаётся. Форматирование Markdown-таблицы на том же скриншоте относится к [отдельной задаче](https://github.com/GitHubFoxy/secretary/issues/5).

## Исследованный источник

`internal/telegram/adapter.go:903` формирует General mirror как `topicName("", mapping.Title) + ":\n" + body`. Название topic может происходить из Task prompt, обрезанного до 60 символов с многоточием (`internal/telegram/topic_title.go`). Поэтому сокращённый вопрос появляется перед Result независимо от текста ответа модели.

Существующий `TestTerminalResultMirrorsToGeneralWithWorkerLabel` прямо закрепляет прежний вывод `Погода:\npong`. Подробные первичные источники и проверка доставки — в [research.md](research.md).

## Требуемое поведение

- Для событий с непустой `TerminalIdentity` отправлять в General тот же подготовленный `body`, что и в Worker topic, без добавления названия topic, двоеточия и перевода строки.
- Пример: `Title = "как будет пачкать…"`, `Text = "«Пачкать» по-английски — по-разному…"` → General начинается с `«Пачкать» по-английски`, без строки `как будет пачкать…:`.
- Правило распространяется на все terminal Results, включая failed/canceled и Follow-up существующего Worker. Пустой Result сохраняет существующий fallback `workerEventLabel`.
- Не удалять первую строку самого Result, даже если она совпадает с названием topic или заканчивается двоеточием. Исправление касается только сборки General mirror.

## Границы изменения

Меняется только `OutgoingMessage.Text` при создании terminal General mirror. Сохраняются:

- создание и названия Worker topics, Task prompt в topic и маршрутизация ввода;
- доставка одного Result в topic и одного в General;
- identities `terminal:<TerminalIdentity>` и `terminal:<TerminalIdentity>:general`, outbox, cursors и дедупликация;
- body sanitization, Telegram HTML, разбиение длинных сообщений и plain fallback;
- canonical Result, Personal Conversation, Web и Secretary replies;
- notifications без `TerminalIdentity`.

Новые зеркала создаются без префикса. Уже сохранённые OutgoingMessage в outbox доставляются с прежним текстом: миграция очереди и редактирование старых Telegram-сообщений не входят в задачу. При живой проверке использовать новый Result после обновления.

## Реализация и проверка

Один [implementation ticket](https://github.com/GitHubFoxy/secretary/issues/28). Новые profiles, regex для удаления строк и изменения formatter не нужны. Проверки должны наблюдать отправленные сообщения, включая Bot API seam, а не только строку helper.
