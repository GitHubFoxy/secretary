# Исследование: форматирование таблиц и структурированных данных в Telegram

Ticket: [01 — Читаемое форматирование таблиц в Telegram](https://github.com/GitHubFoxy/secretary/issues/5). Спецификация: [spec.md](spec.md).

## Краткий вывод

Проблема сломанного форматирования вызвана фундаментальным несоответствием между форматом вывода LLM (Markdown-таблицы GFM) и возможностями отображения в Telegram:
1. **Telegram Bot API не поддерживает таблицы:** Ни в режиме HTML, ни в MarkdownV2 нет тегов или сущностей для таблиц (`<table>`, `<tr>`, `<td>` отсутствуют в спецификации Bot API). Попытка отправить HTML с тегом `<table>` возвращает ошибку `400 Bad Request: can't parse entities`.
2. **Форматтер `internal/telegram/format.go` игнорирует таблицы:** В текущей реализации `formatTelegramChunk` строки с пайпами `|` и дефисами `|---|` воспринимаются как обычный текст, экранируются и отправляются «как есть».
3. **Мобильный лейаут Telegram ломает ASCII-таблицы:** В мобильном приложении Telegram сообщения рендерятся пропорциональным шрифтом в узких пузырях (300–400px). Строка таблицы длиной 120–150 символов неизбежно переносится на 3–5 строк, из-за чего колонки не выравниваются, пайпы `|` разбросаны по тексту, а читать результат становится физически неудобно.
4. **В промптах Worker/Secretary отсутствует запрет таблиц:** Ни `internal/config/defaults/worker.md`, ни `internal/config/defaults/secretary.md` не содержат инструкций по запрету таблиц для мессенджера, поэтому модели по умолчанию используют Markdown-таблицы при сравнительных ответах.

Рекомендуемое решение — **эшелонированная защита (Defense-in-depth)**:
- **Уровень 1 (Промпты):** Запретить использование Markdown-таблиц в профилях `worker.md` и `secretary.md`, предписав использовать структурированные списки с выделением заголовков/ключей.
- **Уровень 2 (Адаптер Telegram):** В `internal/telegram/format.go` реализовать автоматическое преобразование Markdown-таблиц в компактные читаемые списки/карточки («definition list»), если модель всё-таки выдаст таблицу.

---

## Подтверждённые факты с источниками

### 1. Поддержка форматирования в Telegram Bot API
- Согласно [официальной документации Telegram Bot API (Formatting options)](https://core.telegram.org/bots/api#formatting-options), поддерживаемый HTML-подмножество включает только: `<b>`, `<strong>`, `<i>`, `<em>`, `<u>`, `<ins>`, `<s>`, `<strike>`, `<del>`, `<span class="tg-spoiler">`, `<tg-spoiler>`, `<a>`, `<code>`, `<pre>`, `<blockquote>`, `<blockquote expandable>`.
- Теги `<table>`, `thead`, `tbody`, `tr`, `th`, `td` **не поддерживаются**. Отправка любого неподдерживаемого тега приводит к ошибке `400 Bad Request: can't parse entities: Unsupported start tag`.
- Режим `MarkdownV2` также не имеет синтаксиса для таблиц; пайпы `|` в MarkdownV2 зарезервированы как специальные символы (для спойлеров `||spoiler||`) и требуют обязательного экранирования `\|`.

### 2. Текущее поведение форматтера в репозитории
- В [`internal/telegram/format.go:17-57`](../../internal/telegram/format.go#L17):
  ```go
  func formatTelegramChunk(text string) (formatted, plain string) {
      ...
      lines := strings.SplitAfter(text, "\n")
      for i := 0; i < len(lines); i++ {
          line := lines[i]
          if strings.HasPrefix(line, "```") { ... }
          content := strings.TrimSuffix(line, "\n")
          // проверяются только заголовки '# '
          ...
          body, safePlain := telegramInline(content)
          out.WriteString(body)
          fallback.WriteString(safePlain)
      }
      return out.String(), fallback.String()
  }
  ```
  Строки таблицы (например, `| Слово / выражение | Когда использовать | Пример |`) не попадают под ветку кода ```` ``` ```` и не являются заголовками `# `. Они передаются построчно в `telegramInline`.
- В [`internal/telegram/format.go:94-118`](../../internal/telegram/format.go#L94):
  `telegramInline` ищет ссылки `[...]` и маркеры ```` ` ````, `**`, `__`. Символы `|` и дефисы экранируются как обычный текст через `html.EscapeString`.
  Одиночные звёздочки `*` (курсив) также не поддерживаются токенами `telegramInline` (обрабатываются только `**` и `__`), поэтому `*Don’t get your clothes dirty.*` остаётся сырыми звёздочками.

### 3. Цепочка доставки сообщений в Telegram
- Worker завершает выполнение, результат сохраняется в `attempt.outcome` -> `result.accepted` -> `worker.completed` в `internal/core/phase4_store.go`.
- В [`internal/telegram/adapter.go:790-845`](../../internal/telegram/adapter.go#L790):
  - При `importantWorkerEvent(event.Kind)` результат отправляется в Worker Topic:
    `a.sendMessage(ctx, OutgoingMessage{ChatID: mapping.ChatID, ThreadID: mapping.ThreadID, Text: body, Identity: identity})`
  - И дублируется в General чат:
    `mirror := OutgoingMessage{ChatID: owner, Text: topicName("", mapping.Title) + ":\n" + body, Identity: identity + ":general"}`
- В [`internal/telegram/adapter.go:1188-1320`](../../internal/telegram/adapter.go#L1188):
  `sendMessage` вызывает `prepareTelegramChunk(message.Text, message.DeliveredBytes, fences)`, который затем вызывает `formatTelegramChunk(raw)`.
  Нарезанные чанки уходят в Bot API через `a.transport.SendMessage(ctx, msg.ChatID, msg.ThreadID, prepared.formatted, "HTML")`.
- Скриншот пользователя демонстрирует именно сообщение из General топика:
  `topicName("", mapping.Title) + ":\n" + body`
  Заголовок топика: `как будет пачкать по английски ? чтобы было разнве...:`
  Тело: сырой текст с пайпами `|`.

### 4. Конфигурация промптов Worker и Secretary
- [`internal/config/defaults/worker.md`](../../internal/config/defaults/worker.md):
  ```markdown
  You are a persistent Worker for one Task. Complete the assigned work using native harness tools, skills, and internal subagents when appropriate. Return a minimal useful report in the user's language: the result and any essential evidence or source. If blocked, briefly state the blocker and what could not be completed. Do not invent facts or claim success without evidence. For current information, check a current source. Do not repeat the task, add an internal status heading, or describe automatic delivery. Preserve the user's important constraints. Avoid unrelated file changes or package installation.
  ```
  Нет никаких инструкций по стилю форматирования списков или запрету таблиц.
- [`internal/config/defaults/secretary.md`](../../internal/config/defaults/secretary.md):
  Имеет блок `## Language and response style` (запрет тире, использование straight quotes, запрет emoji headings), но не имеет инструкций относительно таблиц Markdown.

### 5. Web UI поведение
- В [`web/src/Markdown.svelte:2-4`](../../web/src/Markdown.svelte#L2):
  Рендеринг выполняется через `markdown-it`:
  `const markdown = new MarkdownIt({ html: false, breaks: true, linkify: true });`
- В [`web/src/app.css:17-18`](../../web/src/app.css#L17):
  ```css
  .markdown table { display: block; overflow-x: auto; border-collapse: collapse; }
  .markdown th, .markdown td { border: 1px solid #334155; padding: .4em .7em; }
  ```
  В веб-интерфейсе таблицы полноценно рендерятся и поддерживают горизонтальный скролл. Любые изменения в формате не должны ломать веб-интерфейс, если в веб приходят исходные данные.

---

## Варианты решения и их оценка

### Вариант 1: Только промпты (Model Profile instructions)
Добавить в `worker.md` и `secretary.md` строгое правило:
> «Do not use Markdown tables (`| ... |`). Present multi-column comparisons and tabular data as structured bullet lists or definition blocks (e.g. `• **Key**: description — example`).»

- **Плюсы:**
  - Модель сама понимает семантику колонок и строит естественные, красиво звучащие списки.
  - Нулевой риск сломать парсер `formatTelegramChunk`.
  - Минимальные изменения в коде.
- **Минусы:**
  - Модели (особенно при сложных запросах или внешних цитатах) могут иногда игнорировать инструкцию и сгенерировать таблицу.
  - Не защищает от вывода внешних инструментов или скопированного текста.

### Вариант 2: Преобразование таблиц в `<pre>` (Monospace code block)
В `format.go` определять таблицу и оборачивать её в `<pre>` с выравниванием пробелами.
- **Плюсы:**
  - Сохраняется сеточная форма таблицы.
- **Минусы:**
  - На мобильных устройствах таблица с текстовыми описаниями (80–120 символов в ширину) потребует горизонтального скролла внутри блока кода.
  - Внутри `<pre>` Telegram отключает весь HTML-парсинг: жирный шрифт, ссылки и курсив становятся неактивными.
  - Читать длинные предложения в блоке кода на телефоне крайне неудобно.

### Вариант 3: Автоматическая трансформация таблиц в списки в `formatTelegramChunk`
В `internal/telegram/format.go` добавить детектор блоков Markdown-таблиц:
Таблица определяется как последовательность:
- Строка заголовков: `| H1 | H2 | H3 |`
- Строка разделителей: `|---|---|---|` (или `|:---|:---:|---:|`)
- 1 или более строк данных: `| C1 | C2 | C3 |`

Трансформация для каждой строки данных:
Если 2 колонки:
`• <b>Col1:</b> Col2`
Если 3+ колонки:
`• <b>Col1:</b> Col2`
`  <i>Col3_header:</i> Col3` (или просто через дефис: `• <b>Col1:</b> Col2 — Col3`)

- **Плюсы:**
  - 100% надёжная защита: ни одна таблица не появится в виде сырых пайпов в Telegram.
  - Идеально адаптировано под мобильный экран: текст переносится естественно, нет полос прокрутки.
  - Сохраняется инлайн-разметка (`<b>`, `<a>`, `<code>`).
- **Минусы:**
  - Требует аккуратной реализации парсера таблиц в `format.go`.

### Вариант 4: Рекомендуемый эшелонированный подход (Defense-in-depth)
Объединить **Вариант 1** и **Вариант 3**:
1. **Основной рубеж (Промпты):** Запрет Markdown-таблиц в профилях `worker.md` и `secretary.md`. Модель изначально выдаёт читаемый структурированный текст.
2. **Страховочный рубеж (Адаптер):** Форматтер `formatTelegramChunk` перехватывает любые непредусмотренные Markdown-таблицы и преобразует их в структурированные списки, гарантируя, что «каша» из пайпов никогда не дойдёт до пользователя.

---

## План проверки

1. **Unit-тесты форматтера (`internal/telegram/format_test.go`):**
   - Таблица 2 колонки (термин — определение).
   - Таблица 3 колонки (как в скриншоте: Слово / Когда использовать / Пример).
   - Таблица с форматированием внутри ячеек (`**bold**`, `[link](url)`, `` `code` ``).
   - Неполная или кривая таблица (без закрывающего пайпа, с разным количеством колонок).
   - Обычный текст с одиночным символом `|` (не должен ошибочно распознаваться как таблица).
   - Проверка лимитов: трансформация таблицы не должна приводить к выходу за лимит 4096 UTF-16 единиц.
   - Проверка Plain fallback: отсутствие сырых `|---|---|---|`.
2. **Тесты профилей (`internal/config/secretary_test.go`, `internal/ctl/worker_profile_dispatch_test.go`):**
   - Проверка наличия инструкций по стилю в компилируемых профилях.
3. **Ручная/E2E проверка:**
   - Отправка запроса с требованием сравнить несколько понятий в Telegram.
   - Проверка читаемости отображения в мобильном клиенте Telegram.

---

## Открытые вопросы

1. **Формат трансформации по умолчанию:**
   Как именно форматировать строки таблицы с 3+ колонками при fallback-трансформации в `format.go`?
   - Вариант А: `• <b>Col1</b> (Col2) — Col3`
   - Вариант Б: 
     ```
     • <b>Col1</b>: Col2
       <i>Header3:</i> Col3
     ```
   - Вариант В: Карточки с явными названиями колонок:
     ```
     — <b>Col1</b>
     • Header2: Col2
     • Header3: Col3
     ```
2. **Поддержка курсива `*` и `_`:**
   Стоит ли в рамках этой задачи добавить базовую поддержку курсива `*text*` -> `<i>text</i>` в `format.go`? На скриншоте видно, что `*Don't get your clothes dirty.*` осталось сырым текстом.
3. **Обновление внешних профилей на production:**
   Если на боевом сервере используются внешние профили (например, `profiles/worker-template-...md`), необходимо синхронизировать новые инструкции также и в них, либо полагаться на форматтер адаптера.
