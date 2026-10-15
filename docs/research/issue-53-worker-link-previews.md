# Issue #53: компактные ссылки Worker в Telegram

Источник задачи: [GitHub issue #53](https://github.com/GitHubFoxy/secretary/issues/53). Дата исследования: 2026-10-14. Проверены официальные материалы Telegram и локальный код проекта. Код и issue не изменялись.

Основа проверки кода — рабочее дерево с HEAD `b856c5db0813eae22bd956a36da25db4ccd6a9fd`, в котором уже были незакоммиченные изменения, включая `internal/telegram/adapter.go`. Ссылки ниже ведут на локальные файлы; для поиска указаны символы, поскольку номера строк могут меняться.

## Рекомендация

Для первого варианта предлагается отключать карточки во всех исходящих сообщениях Worker, сохраняя ссылки и исходный текст. Применять политику также к копии результата Worker в General. Ответы Secretary оставить с прежним поведением. Это предложение исследователя, а не принятое решение: issue прямо оставляет выбор поведения после уточнения предпочтения. Основания: [issue #53](https://github.com/GitHubFoxy/secretary/issues/53), [`Adapter.handleEvent`](../../internal/telegram/adapter.go) и параметры [LinkPreviewOptions](https://core.telegram.org/bots/api#linkpreviewoptions).

Управление карточкой и выбор видимого текста ссылки — два отдельных решения. Уже существующее форматирование `[название](URL)` позволяет сокращать видимый текст; удалять адрес назначения для этого не требуется. Основание: [`telegramInline`](../../internal/telegram/format.go).

## Возможности Telegram Bot API

`sendMessage` и `editMessageText` принимают `link_preview_options` для конкретного сообщения. Источники: [sendMessage](https://core.telegram.org/bots/api#sendmessage), [editMessageText](https://core.telegram.org/bots/api#editmessagetext).

Поля [LinkPreviewOptions](https://core.telegram.org/bots/api#linkpreviewoptions):

- `is_disabled: true` — отключить предпросмотр.
- `url` — выбрать адрес карточки; без него используется первый URL текста.
- `prefer_small_media: true` — уменьшить медиа; нужен явный `url`, поддержка изменения размера обязательна.
- `prefer_large_media: true` — увеличить медиа с теми же ограничениями.
- `show_above_text: true` — карточка над текстом; иначе под ним.

Следовательно, уменьшение медиа не гарантирует компактную карточку arXiv. Перемещение карточки не устраняет занимаемое место. Это вывод из ограничений [LinkPreviewOptions](https://core.telegram.org/bots/api#linkpreviewoptions), а не результат проверки arXiv в клиенте.

Примеры значения параметра:

```json
{"is_disabled": true}
```

```json
{"url": "https://arxiv.org/abs/1706.03762", "prefer_small_media": true}
```

Старый `disable_web_page_preview` заменён на `link_preview_options` в Bot API 7.0, 29 декабря 2023 года. Для новой реализации следует использовать новый параметр; changelog не обещает совместимость старого имени. Источник: [официальная история Bot API, раздел 2023 / December 29](https://core.telegram.org/bots/api-changelog#december-29-2023).

## Что поддерживает проект сейчас

- [`OutgoingMessage`](../../internal/telegram/adapter.go) содержит адрес чата/topic, текст, идентификатор доставки, прогресс разбиения и режим форматирования; поля предпросмотра нет. [`BotAPITransport.SendMessage`](../../internal/telegram/http.go) формирует `url.Values` с `chat_id`, `text`, при необходимости `reply_parameters`, `parse_mode`, `message_thread_id`; ни `link_preview_options`, ни старый параметр не передаются. Поэтому сейчас проект явно не управляет карточкой.
- [`Transport`](../../internal/telegram/adapter.go) имеет `SendMessage`, но не метод редактирования сообщений. В [`BotAPITransport.SendMessage`](../../internal/telegram/http.go) результат API не сохраняется (`t.call(..., nil)`); соответствующего Telegram message ID для дальнейшего редактирования этот путь не возвращает. Удалять карточки у уже доставленной истории потребовало бы отдельной работы, поэтому это не предлагается включать в первую реализацию.
- [`TelegramPolicy`](../../internal/config/config.go) сейчас описывает генерацию названий topics; настройки предпросмотра нет. В [`telegram.Config`](../../internal/telegram/adapter.go) её также нет. Настраиваемый режим потребует явной передачи настройки до адаптера; для фиксированного поведения это необязательно.
- [`Adapter.drainOutbox`](../../internal/telegram/adapter.go) отправляет текст частями, сначала в HTML, а при `ErrFormatting` повторяет часть обычным текстом и сохраняет `PlainFallback`. Очередь `persistedState.Outbox` хранит `OutgoingMessage` для восстановления после перезапуска. Это важные границы будущей настройки: она должна переживать очередь, разбиение и переход к обычному тексту, а не действовать только на первую HTML-отправку.

## Можно ли ограничить только Worker

Да, архитектура позволяет выбирать политику до общего transport. [`telegramEvent`](../../cmd/secretaryd/telegram.go) сохраняет `Kind` и `WorkerRef`; [`Adapter.handleEvent`](../../internal/telegram/adapter.go) отдельно обрабатывает события Secretary, создание Worker и важные события Worker. Ответы Secretary собираются через `handleSecretaryEvent`, `Flush` и `secretaryBatchMessage` в том же файле. Это вывод по существующему коду, не готовая возможность конфигурации.

Точки создания сообщений Worker в [`adapter.go`](../../internal/telegram/adapter.go):

1. `handleEvent`, ветка `worker.created`: исходная задача и узел в topic Worker.
2. `handleEvent`, ветка `importantWorkerEvent`: результаты, запросы пользователя и служебные состояния в topic Worker.
3. `handleEvent`, `mirror`: terminal-результат Worker повторно отправляется в General с `Identity + ":general"`.
4. `deliverTextUpdate`, сообщение `steer-error:`: ошибка передачи пользовательского указания в topic Worker.

Не следует определять происхождение исключительно по `ThreadID != 0`: копия результата Worker в General создаётся без `ThreadID`, как и сообщения Secretary. Надёжнее задавать политику при создании `OutgoingMessage` в ветках Worker. Основание: [`handleEvent` / `mirror`, `secretaryBatchMessage`](../../internal/telegram/adapter.go).

Минимальная предлагаемая схема: поле политики в `OutgoingMessage` → заполнение в выбранных ветках Worker → JSON-значение `link_preview_options` в `BotAPITransport.SendMessage`. У Secretary поле не задаётся. Транспорт уже работает с form-параметрами, поэтому менять HTTP-клиент или добавлять библиотеку не требуется. Основание предложения: [`OutgoingMessage`, `drainOutbox`](../../internal/telegram/adapter.go), [`SendMessage`](../../internal/telegram/http.go).

При расширении структуры важно сохранить поле в сериализуемой очереди. `drainOutbox` копирует целое сообщение (`chunk := message`), поэтому такой способ естественно переносит политику на части и fallback; это предположение для реализации, которое надо подтвердить тестами. Старые записи очереди без нового поля потребуют выбранного поведения по умолчанию. Основание: [`persistedState`, `sendMessage`, `drainOutbox`](../../internal/telegram/adapter.go).

## Настройки клиента Telegram

Официальная публикация Telegram описывает изменение размера медиа, положения карточки и выбранной ссылки **в собственных сообщениях отправителя**. Настройки доступны в процессе подготовки сообщений, ответов и пересылок. Это не документированная настройка получателя для подавления всех карточек входящих сообщений бота. Источник: [Adjustable Link Previews / Reply, Forwarding and Link Settings](https://telegram.org/blog/reply-revolution#adjustable-link-previews).

В рассмотренных официальных материалах не найдено подтверждения глобального переключателя клиента, который гарантированно скрывает карточки всех входящих сообщений Worker. Это ограничение исследования, а не утверждение об отсутствии такого интерфейса во всех версиях и сторонних клиентах. Не следует обещать решение через пользовательскую настройку без проверки конкретного клиента. Основание границы: [официальное описание пользовательских настроек ссылок](https://telegram.org/blog/reply-revolution#reply-forwarding-and-link-settings).

Документация deep links действительно перечисляет страницу настроек `privacy/data-settings/link-previews`, но не определяет её влияние на входящие сообщения бота. Наличие этой ссылки само по себе не подтверждает нужное поведение. Источник: [Settings links](https://core.telegram.org/api/links#settings-links).

Отдельно существует текст настройки предпросмотра для secret chats в официальной платформе переводов Telegram. Bot API не поддерживает secret chats, поэтому переносить эту настройку на обычные сообщения Worker нельзя. Источники: [Privacy.SecretChatsLinkPreviewsHelp](https://translations.telegram.org/en/ios/settings/Privacy.SecretChatsLinkPreviewsHelp), [Bot API dialog IDs / Secret chat IDs](https://core.telegram.org/api/bots/ids#secret-chat-ids).

Практический вывод: контролировать новые сообщения на стороне бота; проверять отображение на используемых клиентах отдельно. Пользовательское управление собственным сообщением или его пересылкой не меняет параметры исходной отправки из [`BotAPITransport.SendMessage`](../../internal/telegram/http.go).

## Компактное форматирование ссылок

Telegram поддерживает HTML inline-ссылки и показывает адрес в предупреждении перед открытием. Источник: [Formatting options / HTML style](https://core.telegram.org/bots/api#formatting-options).

В проекте [`telegramInline`](../../internal/telegram/format.go) уже преобразует `[название](https://...)` в `<a href="...">название</a>`. `safeTelegramLink` допускает HTTP/HTTPS с hostname и отклоняет небезопасные или некорректные адреса. При fallback получается `название (URL)`. Поведение зафиксировано в [`TestTelegramSafeFormattingPreservesLayout`, `TestTelegramFormatterEscapesMarkupAndRejectsUnsafeLinks`](../../internal/telegram/format_test.go).

Варианты для выбора:

- **Сохранить видимый URL и убрать карточку.** Минимальное изменение содержания. Рекомендуемый первый вариант; форматтер не переписывает обычные URL в короткие названия ([`telegramInline`](../../internal/telegram/format.go)).
- **Использовать короткое название с адресом назначения:** `[arXiv: Attention Is All You Need](https://arxiv.org/abs/1706.03762)`. Такой вход уже поддержан; в обычном текстовом fallback адрес снова будет виден ([`telegramInline`](../../internal/telegram/format.go)). Пример условный, Telegram-запрос не выполнялся.
- **Использовать номера источников:** `[1](https://...)` плюс понятное название рядом. Та же поддержанная конструкция; выбор короткого label — редакционное решение, а не новая возможность transport ([`telegramInline`](../../internal/telegram/format.go)).

Замена видимого URL на название сама по себе не задаёт `link_preview_options`: текущий форматтер меняет HTML текста, а transport по-прежнему не передаёт политику. Поэтому форматирование не следует использовать как гарантию отсутствия карточки. Основание: [`telegramInline`](../../internal/telegram/format.go), [`SendMessage`](../../internal/telegram/http.go).

Скрыть длинный URL за label — сохранить переход. Удалить ссылку полностью — убрать адрес назначения; это другое изменение содержания. Для issue предпочтительно сохранить доступ к источникам. Это рекомендация по [постановке #53](https://github.com/GitHubFoxy/secretary/issues/53), а не принятое требование.

## Что выбрать и проверить перед реализацией

Нужно согласовать три решения: отключать карточки или пытаться уменьшать медиа; сохранять исходный текст либо поощрять короткие названия; применять правило ко всем сообщениям Worker или только результатам. Рекомендуемый набор: отключение, сохранение исходного текста, все сообщения Worker вместе с копией результата в General. Основание выбора: [направление issue #53](https://github.com/GitHubFoxy/secretary/issues/53) и перечисленные выше пути [`handleEvent`](../../internal/telegram/adapter.go).

Предлагаемые проверки для будущего изменения, не выполненные в рамках исследования:

- HTTP-запрос Worker содержит корректный JSON политики; запрос Secretary сохраняет прежний набор параметров ([`http_test.go`](../../internal/telegram/http_test.go), [`SendMessage`](../../internal/telegram/http.go)).
- Политика присутствует у задачи Worker, результата в topic и его копии в General; отдельно проверить выбранный scope служебных сообщений ([`adapter_test.go`](../../internal/telegram/adapter_test.go), [`handleEvent`, `deliverTextUpdate`](../../internal/telegram/adapter.go)).
- Все части длинного сообщения, retry после ошибки, восстановление очереди и обычный текстовый fallback сохраняют политику ([`long_message_research_test.go`](../../internal/telegram/long_message_research_test.go), [`TestTelegramParseRejectionFallbackRetryAndDedup`](../../internal/telegram/format_test.go), [`drainOutbox`](../../internal/telegram/adapter.go)).
- Вручную сравнить видимый URL и inline-ссылку на arXiv в используемых клиентах. Поведение уменьшенной карточки и клиентские настройки фактически не проверялись; никаких сообщений в Telegram исследование не отправляло.
