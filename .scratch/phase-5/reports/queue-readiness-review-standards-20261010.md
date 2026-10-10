## Стандарты

Проверен только diff `1b06ddc...89e3135cc22c0cac5727617d2c9310ff2c246848` (исправление `b347501` и merge-коммиты). Жёстких нарушений документированных стандартов и обоснованных замечаний по эвристикам Fowler не найдено.

Источники: `AGENTS.md`, `docs/agents/domain.md`, `GLOSSARY.md`, `docs/architecture/runtime-contracts.md`, `.scratch/code-comment-policy/spec.md`, `.scratch/phase-5/reports/queued-node-readiness-fix-20261010.md`. Каталог ADR отсутствует; domain instructions разрешают продолжить.

- `ServerManager.CommandReady` читает durable Node record, возвращает явную ошибку для revoked Node и проверяет реальную connection под существующим mutex вместе с Online/draining. Connection появляется после authenticated handshake; проверка не создаёт обход аутентификации.
- `NodeRuntime.CommandReady` сохраняет trusted-local путь и явно возвращает ошибку при отсутствии Manager. Это необходимый адаптер между queue и Node runtime, поэтому эвристика Middle Man здесь не даёт полезного замечания.
- В `worker_queue.go` optional readiness проверяется до promotion pending и после terminal/failed/uncertain/lease guards для prepared handoff. Только `(false, nil)` откладывает доставку; постоянные ошибки проходят существующий видимый failure path. Per-Worker lifecycle gate, authorization, input identity, FIFO и отказ от автоматического replay claimed/unknown исполнения сохранены.
- В добавленных строках кода нет комментариев. Markdown-отчёт допустим по policy. Новые тесты проверяют pending/prepared/draining, прежнюю native identity/history, разные identities одинаковых inputs и revoked failure; ненужной новой production-абстракции нет.

Повторные проверки не запускались: integration test/build/diff и targeted/race PASS уже предоставлены. Исходный live prepared-restart FAIL на `e8de503` сверён с `/private/tmp/p5-mac-live-final-evidence.md`; deterministic fixtures не заменяют свежую native приёмку. Ticket 03 и общий MVP gate этим review не закрываются.

Итог: 0 нарушений стандартов, 0 обоснованных замечаний по эвристикам. Файлы репозитория и пользовательские spec diffs не изменены.

После этого review проведена новая native приёмка на `89e3135`: prepared FIFO после crash/reconnect и сохранение контекста подтверждены, scoped 03 resolved. Подробности и исходный FAIL сохранены в [живой матрице](mac-native-acceptance-20261010.md). Общий gate 05 остаётся открыт.
