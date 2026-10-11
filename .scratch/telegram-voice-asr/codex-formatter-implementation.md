# Форматтер через подписку Codex

2026-10-11. Уточнение пользователя заменяет прежний API-key transport. Текущий код: `CodexFormatter`, `provider: codex`, GPT-6 Luna, reasoning none. API-ключи и `api_key_env` удалены из схемы; исторические отчёты описывают предыдущую версию и не являются инструкциями настройки текущей.

## Реализовано

- Существующий Codex CLI выбирается из `SECRETARY_CODEX_COMMAND`, затем `CODEX_PATH`, затем PATH. Используется подписочный login, `forced_login_method="chatgpt"`; API-key environment не передаётся дочернему процессу.
- Запуск `codex exec` с явной моделью, `model_reasoning_effort="none"`, ephemeral, ignore-user-config/rules, skip-git-repo-check, sandbox read-only и approval never. Не используются resume, история или рабочая директория проекта.
- Отдельный temporary cwd; согласованный `formatter.md` записывается как `model_instructions_file`, project_doc_max_bytes=0. Transcript находится только в JSON stdin, не в args и не в логах. Prompt content тоже не помещается в args.
- CLI features shell_tool/unified_exec/apps/multi_agent/hooks/plugins/memories/goals/browser_use/computer_use/image_generation/view_image/shell_snapshot/sleep_tool отключены; web_search disabled, update_plan.enabled=false, features.skip_host_skill_discovery=true. suppress_unstable_features_warning=true исключает известный warning из JSON output.
- JSON output проходит allowlist: только thread/turn lifecycle и agent_message. Tool/action, error, reasoning item или ненулевые reasoning_output_tokens отклоняют результат и включают существующий raw fallback. Отключение функций и sandbox — граница выполнения; не обещаем, что будущие версии CLI никогда не предложат дополнительные служебные инструменты.
- Timeout 20 секунд, stdout event output до 512 KiB, final message до 64 KiB; temp directory удаляется при успехе, ошибке и timeout. Ошибки процесса и stderr не раскрываются. WaitDelay ограничивает ожидание унаследованных pipe handles.
- Существующий ValidateFormatting и durable voice/text pipeline сохранены.
- Strict YAML отклоняет старый api_key_env. Текущие spec/ticket/example/операционная инструкция обновлены; исторические review/deployment сохраняют свою дату и прежний контракт.

## Изменённые файлы

`internal/telegram/formatter.go`, `formatter_test.go`, `adapter.go`; `internal/config/asr.go`, `asr_test.go`; `docs/config.example.yaml`, `docs/telegram-voice.md`; `.scratch/telegram-voice-asr/spec.md`, `issues/01-telegram-voice-asr.md`, этот отчёт и пометка в прежнем implementation-report.

## Проверки и ограничения

Mock command tests проверяют точные flags/model/reasoning/auth selection, stdin data separation, временный cwd/промпт, исключение key env, timeout, cleanup, oversized output, отказ при tool/MCP/reasoning events, preservation продиктованной инструкции как текста. YAML regression проверяет отказ obsolete key setting. Это не live subscription tests.

Родитель отдельно подтвердил настоящий Codex login через подписку и два синтетических вызова на omarchy: GPT-6 Luna none, reasoning_output_tokens=0, без tool calls, 13.788 и 10.895 секунд. Второй probe использовал временный model_instructions_file/project_doc_max_bytes=0. Эти измерения относятся к CLI probe, не являются проверкой полной Go-интеграции или пользовательского Telegram voice. Parent выполняет real-Go smoke/review/deploy.

Официальные страницы, проверенные 2026-10-11: [Config basics](https://learn.chatgpt.com/docs/config-file/config-basic), [Configuration reference](https://learn.chatgpt.com/docs/config-file/config-reference). CLI help и реальный установленный runtime дополняют документацию.

Изменение YAML/prompt по-прежнему требует restart Telegram adapter. Недоступная подписка, квота или несовместимый Codex CLI дают raw fallback с уведомлением; модель не подменяется. Семантические guards остаются консервативными эвристиками.

Полный `go test ./... -timeout 120s`, race Telegram/config, `go build ./...` и scoped `git diff --check` прошли. Логи: `/tmp/secretary-asr-codex-go-test.txt`, `/tmp/secretary-asr-codex-race-test.txt`. Код заморожен перед независимым ревью. Коммиты, deployment и SSH-записи этим агентом не выполнялись.

## Завершение процессов и реальный Go smoke

По замечанию родителя переиспользован существующий `configureTitleProcess`: на Darwin/Linux дочерний CLI запускается в отдельной process group, cancellation посылает TERM всей группе и затем KILL через 200 мс; formatter ждёт завершения этой процедуры до очистки temporary cwd. Это закрывает timeout для JS wrapper и native child; `WaitDelay` сам по себе детей не останавливал. Добавлен реальный subprocess regression `formatter_process_test.go`: wrapper и ребёнок игнорируют TERM, ребёнок пишет heartbeat; после timeout проверяется остановка ребёнка (Linux zombie допускается как уже неисполняющийся процесс), прекращение heartbeat и cleanup. Fixture startup allowance увеличен с 250 до 750 мс после наблюдения, что при общей нагрузке первый shell не успевал создать child PID до короткого deadline.

Parent выполнил реальный Go `CodexFormatter.Format`, а не обход CLI: на omarchy в environment Secretary получен валидный текст «Давай завтра в 14:30 проверим config.yaml и не будем удалять три файла.». Время 9.048 секунды, peak memory 55.7 MB, swap 0, CPU 264 ms. Probe использовал MemoryMax=1G, CPU150%, Runtime35s; это caps проверки, не runtime гарантии самого formatter. Вызов прошёл JSON allowlist и ValidateFormatting с `gpt-6-luna`. Smoke был до добавления process-group cleanup; успешный транспорт от этого не меняется. Полный Telegram E2E по-прежнему ждёт user voice.

После process-group fix финальные `go test ./... -timeout 120s` и `go test -race ./internal/telegram ./internal/config -timeout 90s` снова завершились с exit 0; build и scoped diff-check также прошли. Freeze кода подтверждён родителю; новый файл `internal/telegram/formatter_process_test.go` входит в текущий список реализации.
