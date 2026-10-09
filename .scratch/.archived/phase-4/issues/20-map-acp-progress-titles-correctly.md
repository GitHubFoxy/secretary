# 20 Do not treat ACP progress titles as tool names

Type: task
Status: resolved

## Work

`fx` sends ACP activity whose `title` is an action/progress label, not necessarily a tool name. The adapter currently turns values such as `Running`, `Waiting for` and `Reading` into tool events, producing messages like `Worker запускает инструмент Waiting for`.

Map ACP status, tool identity and lifecycle fields separately. Emit a tool-started or tool-finished event only when ACP data identifies an actual tool and its state. If an update contains only a progress title, show a generic progress status or suppress it; do not invent a command name. Repeated status updates must not be presented as evidence of separate tool invocations.

## Acceptance

- `Running`, `Waiting for` and `Reading` are never displayed as tool names by themselves.
- Real tool events use the actual available tool identity and correct lifecycle state.
- Status-only events do not claim to identify a command or prove that a tool started or finished.
- Repeated equivalent progress updates are deduplicated or coalesced.
- Tests cover status-only titles, valid tool lifecycle data, repeats and missing metadata.
- Пользователь видит настоящее имя tool без обрезки и компактное безопасное превью основного аргумента, а не progress title или полный JSON.
- Превью команды/обычного аргумента содержит первые 25 Unicode-символов; при обрезке добавляется `…`. Имя tool и обозначение его состояния в этот лимит не входят.
- Превью пути использует путь относительно известного Worker workspace, когда это возможно. Длинный путь сокращается по середине с сохранением имени файла, например `src/…/adapter.go`, в сопоставимом лимите до 25 символов плюс маркер обрезки.
- Secrets скрываются до сокращения аргументов и путей. Превью не раскрывает credentials, raw ACP, внутренние identifiers, reasoning или tool output.
- Tests дополнительно покрывают короткие и длинные команды, пути, Unicode, границу 25 символов, чувствительные значения за пределами первых 25 символов и ошибки tool.

## Согласованный формат

Примеры пользовательского отображения:

```text
read src/config.toml
bash python3 - <<'PY' import…
```

Не добавлять громоздкую фразу "Worker запускает инструмент" к каждой строке. Actual tool identity и lifecycle определяются независимо: старт, завершение и ошибка показываются только при наличии соответствующих данных. Статус может отображаться отдельно от имени и аргумента; имя и аргумент не должны подменять сведения о состоянии.

Для `read`, `edit`, `write` и других файловых tools основной аргумент это безопасный путь. Для `bash`/`shell` это безопасное превью команды. Для остальных tools выбирается понятный основной аргумент из доступной схемы. Не выводить весь JSON и не угадывать неизвестные значения.

Переносы строк и лишние пробелы в превью сворачиваются в одну строку. Обычный аргумент сначала полностью проверяется/очищается от чувствительных значений, затем сокращается до первых 25 символов с `…` при необходимости. Сокращение не разрезает UTF-8 символы.

Для пути разрешено утверждённое исключение из правила "первые 25 символов": сохранять информативный конец с именем файла, сокращая середину. Если одно имя файла само длиннее лимита, сократить его середину, сохранив расширение, когда оно есть. Относительный путь вычислять только при известном workspace и без доступа к файловой системе удалённого Node. При отсутствии надёжного workspace использовать безопасное компактное отображение имеющегося пути, не выдавая его за относительный.

Связывать обновления с конкретным tool invocation, когда есть идентификатор. Повторные сообщения о том же состоянии не должны выглядеть как новые вызовы; разные реальные вызовы одного и того же tool не должны исчезать из-за dedupe только по имени.

## Comments

Пользователь утвердил компактный формат с настоящими tool names, превью аргументов до 25 символов и сохранением имени файла при сокращении длинных путей. Для команд вроде длинного `python3` heredoc достаточно короткого однострочного начала; полный скрипт в progress не нужен. Исправление не включает смешивание progress с Result или Telegram Markdown delivery из тикета 21.

## Answer

ACP tool identity берётся только из явных `name`/`tool_name`/`tool`; `title` не подменяет отсутствующую identity. Initial `pending`/`queued` сохраняются без start, terminal states не превращаются в start, а подтверждённый `in_progress` запускает событие один раз. Standalone terminal frames связываются с ID и дедуплицируются; sparse arguments обновляются только при наличии input.

Sanitizer редактирует чувствительные значения без изменения whitespace; сворачивание строк остаётся только в preview. URL userinfo, percent-encoded credentials и curl `-u`/`--user` скрываются до truncation. Dedupe fingerprints хранят только локальный digest. Telegram Topics, Web observer и связанные Web diagnostics сохраняют явную identity/lifecycle и не показывают полный JSON или tool output.

Финальный targeted review: новые id-less `tool_call` одного имени всегда создают отдельные invocations; ambiguous sparse updates не коррелируются и не меняют их arguments. Уже подтверждённый `in_progress` монотонен относительно повторных `pending`/`queued`/unknown updates, поэтому повторный `in_progress` не даёт второго start.

Проверки: focused `go test ./internal/node`; `go test ./...`; `go test -race ./...`; `go vet ./...`; `go build ./...`; `git diff --check` — прошли. Web не менялся после предыдущих прошедших `npm test`, `npm run build:all` и `go test ./web`.
