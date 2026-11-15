# Production: форматирование через подписку Codex

2026-10-11. Уточнение пользователя выполнено: API-транспорт удалён. Локальный ASR сохраняется; форматтер работает через существующую подписку ChatGPT/Codex. Пользовательский Telegram E2E ещё ожидается, тикет открыт.

## Версия

Commit: `03ed1072de59057faa22ad92211f46ce4b8016fb`.

Candidate: `/home/coder/.local/share/secretary/candidates/asr-codex-03ed107-20261011`.
Backup предыдущей установки: `/home/coder/.local/share/secretary/backups/asr-codex-03ed107-20261011`.

Linux amd64 binaries собраны с CGO=0 из чистого git archive commit. Несвязанные пользовательские изменения не включены. Manifest и source.tar сохранены в candidate, SHA проверены после передачи:

- secretaryd: `271c0014bdad3e017ffc703f4fed8dbaf68d09954bb5bac1a659ee6ab602a1aa`.
- secretary-mcp: `1a01219a10d34e1b8c272209002be5ae129f9597713362517ee8d491ae6efdbc`.
- source.tar: `8ef0295a67c08f85d507d76bc928649a37b52fd1554e6942454e002fce19e0d2`.

Внешний asrcheck — диагностический helper из тех же внутренних модулей для синтетической проверки, не основной production binary.

## Конфигурация и установка

`/home/coder/.local/share/secretary/config.yaml`: `provider: codex`, `model: gpt-6-luna`, `reasoning: none`, timeout 20 секунд. API-key поля удалены. Prompt остаётся `/home/coder/.local/share/secretary/formatter.md`. `show_asr_in_chat: false`. ASR использует прежние nemo-speech/Parakeet Q8 Hermes.

Codex CLI берётся из существующего `SECRETARY_CODEX_COMMAND`, авторизация принудительно ChatGPT. Пользовательский config/rules/project docs не загружаются; запуск ephemeral с отдельным cwd и prompt, transcript только в stdin. Выполнение команд и интеграции отключены; read-only sandbox, output bounds, JSON allowlist, protected-text guards и raw fallback сохранены. Тайм-аут завершает process group wrapper/native child.

Systemd ASR drop-in переключён на новый candidate; `CPUQuota=200%` сохранён для всего Secretary service. Перезапущен только Secretary; Node/Hermes и их настройки не изменялись. Основной TOML совпадает с backup по SHA. YAML/prompt — приватные файлы. Секреты не запрашивались, ключи не добавлялись.

## Проверки

- Full Go suite, race Telegram/config, build, diff-check прошли; реальный timeout regression останавливает wrapper и child.
- Независимые [Spec review](codex-formatter-review-spec.md) и [Standards review](codex-formatter-review-standards.md): подтверждённых замечаний нет.
- Реальный RU Go formatter вызов: 9,048 секунды, peak 55,7 MB, swap 0, CPU 264 ms. Получен валидный отформатированный текст; форматтер JSON/semantic guards прошёл.
- После установки: диагностический Go helper прочитал production config и использовал точное окружение запущенного Secretary из `/proc/MainPID/environ`, без вывода значений секретов. EN synthetic input успешно отформатирован за 7,046 секунды: `Please don't delete 3 files, and check config.yaml tomorrow at 14:30.` Модель `gpt-6-luna`, reasoning `none`, provider `codex`. Прямой HTTP API не использовался.
- Прежние CLI probes подтвердили подписочный login и reasoning_output_tokens=0; текущий formatter также отклоняет reasoning/tool/action events.
- Health ok; Secretary, Node и Hermes active; NRestarts=0; ExecStart указывает на новый candidate. Исходный TOML не изменился.

Измерения formatter относятся к синтетическому тексту. Пользовательский voice не передавался облаку для этих проверок. Реальный локальный ASR проверен раньше на сохранённом RU voice; полный Telegram → ASR → formatter → Secretary/Worker будет проверен после нового voice пользователя.

## Откат

В backup сохранены active.conf, прежний YAML/prompt, deployed-commit, Telegram state и консистентная SQLite backup. Возврат binaries/config: восстановить active.conf в ASR drop-in и YAML/prompt/deployed-commit, daemon-reload, restart Secretary, health check. Предыдущая версия использовала ошибочно выбранный API-transport; такой откат вернёт raw fallback, а не подписочный formatter. DB/Telegram state не восстанавливать автоматически, чтобы не потерять новые сообщения.
