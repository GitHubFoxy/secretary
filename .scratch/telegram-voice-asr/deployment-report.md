# Telegram voice ASR: production

2026-10-11. Реализация и два независимых раунда ревью завершены. Production обновлён, живая приёмка пользователем ожидается. Тикет не закрыт.

## Версия и установка

- Commit кода: `01055a1f7c6f67a4349a384b68d9239c58871d6a`.
- Linux amd64, CGO выключен; production binaries собраны из чистого `git archive` этого commit, без посторонних изменений checkout.
- Candidate: `/home/coder/.local/share/secretary/candidates/asr-01055a1-20261011`.
- Backup: `/home/coder/.local/share/secretary/backups/asr-01055a1-20261011`: прежний wrapper/dropins, TOML, Telegram state, консистентная SQLite backup, сведения об отсутствовавших YAML/prompt.
- SHA256 secretaryd: `f5708b777e88682adde397390f7aab4ef8631c91e43bc8bf5bb4f3afd1ace8b1`.
- SHA256 secretary-mcp: `dbc180ef214d25f3f6bc5fc62ec2ce76469a70746f93d6de9382130bd48adbd2`.
- SHA256 source.tar: `5c8d3dcf9740cb3c97f5f4cecff56adbf5245444bb3444aed53e992d695720f2`.

Manifest и source.tar сохранены в candidate. Внешний `asrcheck` — вспомогательная проверка конфигурации/синтетического formatter, не production binary. Прежний wrapper скопирован, заменены только пути secretaryd/MCP на новый candidate. Node не обновлялся и не перезапускался; Hermes не менялся.

## Настройки

Основной `/home/coder/.local/share/secretary/config.toml` не изменён: SHA совпадает с backup. ASR sidecar `/home/coder/.local/share/secretary/config.yaml`, prompt `/home/coder/.local/share/secretary/formatter.md`; права обоих `0600`.

Используются существующие nemo-speech и Parakeet Q8 Hermes; весов не скачивали. Formatter: `gpt-6-luna`, `reasoning: none`, `OPENAI_API_KEY`, enabled. Показ отдельной расшифровки выключен (`show_asr_in_chat: false`). После настройки ключа или изменения YAML/prompt нужен restart Secretary.

Новый systemd drop-in: `/home/coder/.config/systemd/user/secretaryd.service.d/zzzzzzz-telegram-local-asr.conf`. `CPUQuota=200%` ограничивает суммарное потребление CPU всего Secretary service, включая его дочерние процессы; это может ограничивать скорость встроенного runtime. ASR запускается по одному. Жёсткий MemoryMax на весь Secretary не вводился, чтобы не обрывать основной сервис при росте памяти других дочерних процессов.

## Что проверено

- Полный Go suite, race Telegram/config, build и diff check прошли после исправлений.
- Независимые повторные Spec/Standards review: новых подтверждённых замечаний нет.
- Настоящий локальный ASR через новый Go wrapper на сохранённой 10-секундной RU-записи: 4,199 секунды, 59 символов, контрольная фраза присутствует. Пик памяти 1,3 ГБ, swap 0. Изолированный probe ограничен MemoryMax=2G, MemorySwapMax=0, CPUQuota=150%, taskset 0,2, RuntimeMaxSec=90. Личная запись в облако не передавалась.
- Production config читается, prompt загружается, engine доступен и веса читаются.
- После `systemctl --user restart secretaryd.service`: health status ok, Secretary/Node/Hermes active, NRestarts=0, новый binary реально выполняется, asr-tmp создан, ошибок ASR/fatal/panic в стартовом журнале не обнаружено.

## Незавершённая приёмка

На момент проверки `OPENAI_API_KEY` отсутствует в environment сервиса. Живой OpenAI-вызов не выполнен; модель не подменялась. При voice сейчас должен использоваться raw fallback с уведомлением. Владелец может настроить ключ непосредственно в серверном `/home/coder/.local/share/secretary/environment`, не передавая секрет в чат, и сообщить о готовности для restart/проверки.

После этого проверить синтетическое форматирование и voice пользователя в настоящем Telegram Secretary/Worker. Полный RU/EN Telegram E2E, оба значения флага показа и 120-секундная запись ещё не проверены на живом production; fixtures не означают их прохождение.

## Откат версии

Прежний candidate и wrapper сохранены. Чтобы вернуть прежний запуск, удалить только новый ASR drop-in, восстановить предыдущие YAML/prompt согласно backup (до установки оба отсутствовали), выполнить daemon-reload и restart Secretary, затем проверить health. Node/Hermes не трогать. SQLite/Telegram backup не восстанавливать автоматически: это может потерять ввод, принятый после deployment; восстановление данных требует отдельного решения.
