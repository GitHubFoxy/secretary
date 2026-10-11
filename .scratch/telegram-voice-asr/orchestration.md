# Реализация ASR: оркестрация

Пользователь 2026-10-11 разрешил реализацию, вложенных субагентов, отдельное последующее ревью, Git-коммит и deployment production. Финальный live E2E — попросить пользователя отправить voice после технических проверок и развёртывания. Повторное разрешение на commit/deploy не требуется.

Последнее изменение UX: `asr.show_asr_in_chat: false` по умолчанию; `true` включает показ итоговой расшифровки. Ошибки не скрываются.

## Текущий этап

Gemini 3.8 Flash High реализует тикет. Task ID:
`node:delegated-task:command%3Amcp%3A180dde3a-aaf3-41fa-b85f-5ddb7601b6d1%3Adelegate-task%3Atelegram-local-asr-implementation-20261011-v1`

Результат читать через T3 `task_status`. Отчёт ожидается в `implementation-report.md`. После завершения запускать НОВЫЙ `delegate_task` независимого review-субагента (пользователь предпочитает Gemini; выбрать текущий ID через capabilities), передав исходный brief, spec/ticket, baseline, отчёт реализации и все ограничения. Review должен проверить code + spec + correctness/security, необходимые tests. Замечания исправлять отдельным implementation round и повторно review; не продолжать review через backing childThreadId. Production меняет parent после успешных проверок.

## Baseline и сохранность

Git HEAD: `8ceaee3a56d45baf532238d8054d881faf4d53e6`.
Предсуществующие изменения: `/tmp/secretary-asr-baseline-status-20261011.txt`, `/tmp/secretary-asr-parent-baseline-diff-20261011.patch`. Большое количество удалений `.scratch` и пользовательские изменения `spec/THE spec.md`, `spec/memory.md` не относятся к реализации; не reset/stash и не включать в commit. Commit только проверенных файлов ASR и связанных docs. Research cloud уже в `spec/research-cloud-*.md`, не возвращать к локальному тикету.

## Production: предварительная read-only проверка

omarchy по SSH alias `omarchy`. 2026-10-11: secretaryd, secretary-node и hermes-gateway active; `GET http://127.0.0.1:8081/v1/health` возвращает status ok.
Unit secretaryd: `/home/coder/.config/systemd/user/secretaryd.service`; WorkingDirectory `/home/coder`.
Текущий config Secretary — `/home/coder/.local/share/secretary/config.toml`, не YAML. User запросил ASR settings в `config.yaml`; implementation должен явно обеспечить чтение этого файла без разрушения существующей TOML-конфигурации. Не мигрировать silently все settings.
Локальный engine: `/home/coder/.local/bin/nemo-speech` -> `/home/coder/.local/share/nemo-speech/bin/nemo-speech`.
Веса: `/home/coder/.local/share/nemo-speech/models/parakeet-tdt-0.6b-v3.q8_0.gguf`, 713975456 bytes. Переиспользовать, не менять Hermes.
Deployment runbook: `docs/always-on-runbook.md`. Он исторический; проверить фактический release/layout и unit перед изменениями. Сохранить текущие binary/config/data, приготовить rollback и кандидат до restart; через systemd, не launcher stop/start. Не выводить secrets или полный environment. После deployment проверить health и оба Secretary services, Hermes untouched. Модель formatter/API reasoning и key availability должны быть подтверждены, не заявлять actual cloud success по mocked fixtures.

## Осталось

1. Получить integrated Gemini result.
2. Независимое review, исправить реальные замечания, необходимые tests/build.
3. Scope-only Git commit; проверить deployment, установить config/prompt/binary с backup и restart после готовности.
4. Попросить владельца прислать voice и проверить реальный путь ASR→formatter→Secretary/Worker; различать synthetic/fixtures и live.

## Продолжение после квоты Gemini

Gemini завершился с Usage Limit Reached (reset через 2 ч 16 мин), оставив частичный непроверенный код без implementation-report. Завершение поручено native Codex-субагенту `/root/finish_local_asr`, без deployment/commit. Обнаружена регрессия Gemini в cmd/secretaryd/telegram.go event bridge, субагенту поручено восстановить baseline mapper. Production не изменялся.

Read-only production: active binary `/home/coder/.local/share/secretary/candidates/response-style-6b8f826-20261010T151452Z/linux-amd64/secretaryd`, SHA256 `83a1ad7ccc8f1ccbd6454319abd690f5ca01331f49e2dccddd9331548b1d1c79`; argv0 тот же, data-dir `/home/coder/.local/share/secretary`, listen loopback8081. Repo `/home/coder/projects/secretary-v2` HEAD `204c466a4a74956331153f2136858d691b7fac19`, clean. API_KEY vars в environment отсутствуют; пользователь уже попросили добавить export OPENAI_API_KEY в серверный environment, не присылать ключ в чат. Реализация не блокируется, actual cloud live требует ключа.

OpenAI Docs parent verification: https://developers.openai.com/api/docs/models/gpt-6-luna и https://developers.openai.com/api/docs/guides/latest-model открыты2026-10-11. Подтверждены modelID gpt-6-luna, ChatCompletions и reasoning_effort:none. APIдоступконкретногоаккаунтаещёнепроверенбезkey. Субагентformatter_hardenготов, testsfakeHTTPpass. Главныйfinish_local_asrдоводитintegratedtests.
