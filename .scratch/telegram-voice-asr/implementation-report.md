# Реализация Telegram voice: отчёт перед независимым ревью

2026-10-11. Код реализован; commit, deployment и живая приёмка ещё не выполнялись этим агентом. Тикет остаётся открытым до review и voice от пользователя.

## Изменённые файлы реализации

- `cmd/secretaryd/telegram.go`: только подключение ASR-конфига и промпта из snapshot; исходный event bridge восстановлен полностью после несвязанного изменения Gemini.
- `internal/config/config.go`, `manager.go`, `asr.go`, `asr_test.go`, `defaults/formatter.md`: TOML сохраняется основным конфигом, `config.yaml` рядом читается как строгий ASR sidecar; путь weights/prompt относительно его каталога, defaults/validation, version/diff учитывают все ASR-поля и содержимое промпта.
- `internal/telegram/adapter.go`, `input_queue.go`: durable очередь входящих updates, сохранение до advancement offset, отдельные chat/topic lanes, независимое polling от voice, стабильные server idempotency keys, восстановление pending после restart, последовательная обработка lane.
- `internal/telegram/http.go`: Telegram voice DTO, getFile, bounded download, reply_parameters.
- `internal/telegram/asr.go`, `asr_test.go`: выбранный nemo-speech CPU, ffmpeg mono PCM16 16k, проверка фактической WAV-длительности, общий timeout, один ASR-процесс, cancellation ожидания слота, очистка временных файлов и остатков после restart, проверка бинарников/весов.
- `internal/telegram/voice.go`, `voice_test.go`, `voice_http_test.go`: owner/topic проверяется до download; raw/formatted/final-variant вместе с Telegram identity в state; повтор доставки использует сохранённый transcript; Worker request ID сохранён для replay; скрытая расшифровка по умолчанию, показ по флагу ответом на voice, уведомления через durable outbox.
- `internal/telegram/formatter.go`, `formatter_test.go`: OpenAI Chat Completions, явный model ID, reasoning_effort none, system prompt и user transcript отдельно, без tools/history; bounded response, rejection отказа/обрыва/пустоты, консервативная проверка защищённых элементов, ошибки без API body/секретов.
- `docs/config.example.yaml`, `docs/formatter.md`, `docs/telegram-voice.md`: согласованная конфигурация и промпт.
- `go.mod`, `go.sum`: YAML parser `gopkg.in/yaml.v3`, уже присутствовавший в dependency graph; heavy inference dependencies не добавлены.

Несвязанные пользовательские изменения `.scratch/.archived/`, `spec/` и другие untracked материалы не трогались и не относятся к реализации.

## Контракт и поведение

Основная конфигурация остаётся `config.toml`. Положить `config.yaml` и `formatter.md` рядом; пример в `docs/config.example.yaml`. ASR settings и prompt фиксируются в Telegram adapter при старте, поэтому после изменения нужен restart Secretary. `Manager.Reload` корректно обнаруживает ASR sidecar/prompt changes; это не объявляет live hot reload адаптера.

Model `gpt-6-luna`, Chat Completions и reasoning_effort `none` подтверждены родительским агентом в официальных страницах OpenAI, проверенных 2026-10-11:

- [GPT-6 Luna](https://developers.openai.com/api/docs/models/gpt-6-luna)
- [Latest-model guide](https://developers.openai.com/api/docs/guides/latest-model)

Fixture тесты проверяют именно этот request contract без tools. Авторизованный живой API-вызов пока не выполнен: в Secretary environment отсутствовал `OPENAI_API_KEY`. Недоступный форматтер возвращает raw с кратким уведомлением, без молчаливой подмены модели.

Тестовые mock WAV и HTTP responses искусственные. Личные benchmark audio не читались и не отправлялись в облако в ходе реализации. Реального локального inference этот агент не запускал.

## Ограничения проверок и ресурсов

Проверки форматирования являются консервативными эвристиками: Unicode-count отрицаний, обнаруживаемые имена с заглавной буквы, ограниченный словарь числительных, цифры, пути, URL/email и технические команды. Они могут отклонить допустимое удаление начального слова-паразита; не находят все lowercase имена, перемещение отрицания и другие изменения смысла. При обнаружении изменения используется raw. Это не доказательство смысловой эквивалентности.

Код задаёт `OMP_NUM_THREADS=2`, `OPENBLAS_NUM_THREADS=2` и ffmpeg threads=1, а также один локальный ASR одновременно. Это не hard CPU/RAM caps для ggml. MemoryMax=2G/CPU150% были только профилем исторического benchmark; runtime не обещает эти ограничения. Production systemd caps решает родительский deployment. Записи 120 секунд ещё не измерялись, 90 секунд — timeout, не гарантия скорости.

Временные файлы находятся в отдельном `asr-tmp` рядом с Telegram state; удаляются на любом обычном завершении, после kill/restart остатки удаляются до новых заданий.

Durable input plus stable core idempotency обеспечивает отсутствие второго пользовательского запроса при replay/restart. Telegram outbox сохраняет уведомления и markers; как и существующая доставка, crash между фактической отправкой Telegram и записью marker оставляет узкое окно повторного внешнего уведомления, поскольку Bot API sendMessage не предоставляет idempotency key. Core input от этого не повторяется.

## Проверки

- `go test ./internal/telegram ./internal/config ./cmd/secretaryd -timeout 40s` — прошло.
- `go test ./... -timeout 120s` — прошло; полный вывод `/tmp/secretary-asr-go-test.txt`.
- `go test -race ./internal/telegram ./internal/config -timeout 90s` — прошло; вывод `/tmp/secretary-asr-race-test.txt`.
- `go build ./...` — прошло.

Meaningful checks: RU/EN formatter HTTP contract/safety/error/timeout; hidden/shown transcripts; owner/unknown-topic/size/duration до inference; actual decoded duration; ASR cleanup/timeout/cancelled queue; formatter fallback; same-lane voice/text FIFO и independent Worker topic; durable pending restart; core retry без повторного download/ASR/notice; Telegram DTO/limited download; config sidecar defaults, relative paths, reload и missing prompt.

## Осталось родителю

Независимое ревью, исправления по нему, commit только файлов реализации, безопасный deployment с сохранением действующего TOML/Hermes, наличие formatter key, synthetic authorized cloud API check и пользовательский voice в Telegram для финального E2E. Приёмка полного RU/EN Telegram → реальный nemo-speech → реальный OpenAI → Secretary/Worker ещё не доказана fixtures.

## Исправления после независимого ревью, раунд 2

Исправлены два воспроизведённых замечания P2:

1. `ValidateFormatting` теперь симметрично сравнивает полные мультимножества чисел и технических токенов: ловит добавление, удаление, замену и изменение количества повторений. Допустимая концевая пунктуация URL/путей по-прежнему нормализуется. Примеры добавленного времени `14:30`, новой цифры `5`, URL, пути и `config.yaml`, повторённого числа/URL/идентификатора отклоняются тестами.
2. Для обычного текста в Worker topic исходный pending request ID фиксируется в durable `PendingInputs` перед первым server call. Snapshot выполняется на стадии dispatch, а не enqueue: следующий текст не становится повторным ответом на уже закрытый request. Replay использует сохранённый request ID, даже если mapping был очищен перед crash или Worker уже создал новый request. Clearing mapping дополнительно сверяет текущий request ID, чтобы не стереть новый вопрос.

Изменены в этом раунде: `internal/telegram/formatter.go`, `formatter_test.go`, `adapter.go`, `input_queue.go`; добавлен `internal/telegram/input_queue_test.go`. Отчёт дополнен этим разделом.

Focused check: `go test ./internal/telegram -run 'Test(ValidateFormatting|WorkerTextReplay|WorkerQueuedText)' -count=1 -timeout 30s` — прошло. Регрессионный тест воспроизводит принятие server input с потерей ответа, durable clearing mapping без Processed, restart, новый pending request и replay; проверяет неизменный fingerprint и отсутствие повторного принятого ввода.

После интеграции раунда 2 прошли `go test ./... -timeout 120s`, `go test -race ./internal/telegram ./internal/config -timeout 90s`, `go build ./...` и scoped `git diff --check`. Выводы тестов: `/tmp/secretary-asr-go-test-round2.txt`, `/tmp/secretary-asr-race-test-round2.txt`. Код снова заморожен для следующего независимого review; commit/deployment не выполнялись.
