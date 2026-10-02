# 17 Generate readable Worker Topic titles from the Secretary prompt

Type: task
Status: resolved
Blocked by: none

## Work

Worker Topic titles are currently taken from the Worker title and can be awkward or unhelpful. Generate a short, readable Telegram Topic title from the prompt Secretary dispatches to the Worker. Use the configured title model, with `gpt-6-luna` and no or minimal reasoning as the intended default.

Use only the user-facing task prompt sent to the Worker. Do not include hidden system instructions, private metadata or internal reasoning. Keep the generated title within the adapter's current 60-rune topic-name limit. If generation fails or returns an empty/invalid title, create the Topic with a readable deterministic fallback derived from the prompt; title generation must not block Worker dispatch or Result delivery.

## Acceptance

- A new Worker Topic gets a concise title that describes its dispatched task rather than a generic or awkward title.
- Title generation uses the configured title model and reasoning setting.
- Failure, timeout or empty model output falls back predictably and does not fail Dispatch.
- The title respects the adapter's current length limit and does not expose internal prompts or reasoning.
- Tests cover representative prompts, invalid model output, generation failure and fallback.
- Harness явно указан в config; внешний prompt хранится в `title-generation-prompt.md` и применяется через config reload.

## Comments

По запросу пользователя генератор переведён с первоначального варианта на `fx` на OpenCode. В `[telegram]` добавлены `title_harness = "opencode"` и `title_prompt = "title-generation-prompt.md"`. Secretary и Worker harnesses остаются независимыми.

## Answer

Реализовано в `internal/telegram/topic_title*.go` и daemon event bridge:

- Генератор получает только `Worker.intent`, соответствующий `OriginalUserIntent` в Dispatch. Worker title, Project/Policy snapshots, profiles и `user.md` в запрос не передаются.
- Используется OpenCode v2 `run --standalone`. Отдельный primary agent получает внешний system prompt, один model step, deny-all permissions и модель без tools. Временные HOME/config/state исключают пользовательские instructions, skills, MCP, plugins и shared service; provider authentication остаётся в исходной data directory.
- Model ID явно передаётся в CLI. Bare ID означает `openai/<model>`; variant `secretary-title` задаёт configured `reasoningEffort`. Смена модели при ошибке не допускается.
- Генерация ограничена пятью секундами. Ошибка, timeout и невалидный ответ дают детерминированное название из первой читаемой строки задачи. Название не превышает 60 рун, сохраняется в Topic mapping и не генерируется повторно при replay/restart.
- `title-generation-prompt.md` создаётся рядом с `config.toml`, без перезаписи существующего файла. Config snapshot хранит его содержимое/hash; reload prompt меняет `telegram.title_prompt` diff, но не Secretary/Worker profile hashes. Поддерживаются legacy configs без новых ключей.
- CLI stream принимает только завершённые text parts последнего assistant message. Reasoning, tool events, native IDs и provider diagnostics не становятся названием. Stdout ограничен 32 KiB; по timeout на Linux/macOS завершается вся группа процессов.

Проверки прошли:

- `go test ./...`;
- `go test -race ./...`;
- `go vet ./...`;
- `go build ./...`;
- `git diff --check`;
- `SECRETARY_OPENCODE_TITLE_E2E=1 go test ./internal/telegram -run TestOpenCodeTitleNativeHTTPFixture -count=1 -v` с настоящим OpenCode v2.0.22 и локальным HTTP fixture. Получен один запрос с `reasoning_effort = "minimal"`, без tools, с внешним system prompt и заданной задачей. Платный provider не вызывался.

Проверка доступности `gpt-6-luna` у реального provider не заменяется fixture. На `omarchy` изменения не развёртывались; сервер не перезапускался.
