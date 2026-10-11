# Учёт времени Telegram voice

2026-10-11. Добавлены минимальные per-voice measurements без нового framework, config flags или сообщений в чат. Форматтер автоматически не включается; текущий production `enabled: false` не менялся. Commit/deploy/SSH этим агентом не выполнялись.

## Данные и границы

У новых успешных ASR-записей `VoiceTranscript` содержит optional pointer `timing`:

- `download_ms`: Telegram getFile и скачивание.
- `asr_ms`: полный wall time вызова Transcribe; включает ожидание единственного локального слота, ffmpeg, загрузку модели и inference. Это не отдельно время математического inference.
- `formatter_ms`: полный вызов Format; в случае Codex включает CLI startup и облачный ответ. При disabled/unavailable равен 0.
- `processing_ms`: от начала preprocessing до окончания download/ASR/formatter и проверки результата. Завершается до state save, Telegram notices, server/agent dispatch. Это не задержка полного ответа пользователя.
- `formatter_status`: `disabled`, `succeeded`, `failed`, `unavailable`. В журнале раннего сбоя, когда включённый форматтер ещё не вызывался, статус `not_run`; такой transcript не создаётся.

Использованы `time.Now`/`time.Since` с монотонным clock. UTC `a.now` остаётся только CreatedAt, не используется для durations. Значения округляются вниз до целых миллисекунд; быстрые стадии могут иметь 0.

После успешного ASR metrics сохраняются рядом с raw/formatted и исходной message identity. Formatter failure сохраняет measured duration с raw fallback. Replay/restart/повтор server delivery используют первоначальные metrics и не запускают модели заново. Старые records без timing читаются с nil и не получают выдуманные значения.

Журнал содержит только update/message numeric IDs, durations и фиксированные stage/status. Transcript, audio, Telegram file IDs, API/CLI diagnostics и ошибки с приватным содержимым не пишутся. Download/ASR failures логируют elapsed durations до уведомления. Formatter fallback логируется с `formatter_status=failed`; cancellation во время форматтера логирует failed stage до возврата. ASR failures не создают transcript.

## Файлы

`internal/telegram/voice.go`, `voice_test.go`, новый `voice_timing_test.go`, `docs/telegram-voice.md`, ASR ticket и этот отчёт. Runtime config/formatter enablement/production не изменялись.

## Проверки

- Focused `go test ./internal/telegram -run 'TestVoice(Timings|FailureElapsed|TranscriptLegacy|RetryReuses)' -count=1 -timeout 30s` — прошло.
- Полный `go test ./... -timeout 120s` — прошло.
- `go test -race ./internal/telegram -timeout 90s` — прошло.
- `go build ./...` и scoped `git diff --check` — прошли.

Tests проверяют persisted measurement до dispatch, success/failure/disabled/unavailable formatter, zero time невызываемой стадии, privacy и согласованность stored/log values, replay после обычной обработки и restart после неудачной server delivery, старую JSON запись, elapsed error logs и truthful not_run до formatter. Короткие искусственные задержки 2 мс дают измеримые интервалы без узких upper-bound thresholds.

Логи suite: `/tmp/secretary-asr-timing-go-test.txt`, `/tmp/secretary-asr-timing-race-test.txt`. Код заморожен для независимого ревью. Production measurements появятся только после deployment и следующего voice; результаты synthetic tests не выданы за реальные ASR latency.
