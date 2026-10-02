# POC: ранний Secretary Bridge через AgentHub

Дата: 2026-10-02.

Описание каталога `poc/` перед удалением по просьбе пользователя. Исходники остаются в Git history; этот документ не заменяет их резервную копию.

## Точка восстановления

- Repository: `GitHubFoxy/secretary`.
- Branch: `phase4-implementation`.
- Commit: `5bad653c33c20dc18b889bdef19e26df11984624`.
- Git tree `HEAD:poc`: `cdc7bfe7376c380fc10c010fe00fdf7ba5ebd98c`.
- Четыре tracked files, около 52 КБ с локальным Python-кэшем.
- Перед удалением `git status --short --untracked-files=all -- poc` был пустым. Несохранённых изменений вне ignore rules не было.

Для извлечения исходников в отдельный каталог без восстановления POC в рабочем проекте:

```sh
mkdir -p /tmp/secretary-poc-recovery
git archive 5bad653c33c20dc18b889bdef19e26df11984624 poc \
  | tar -x -C /tmp/secretary-poc-recovery
```

Команда приведена для будущего восстановления и при удалении не выполнялась.

## Что проверял прототип

Первый trusted-local сценарий на Linux host с AgentHub и Codex ACP, без fork AgentHub. Пользователь открывал stock AgentHub Web UI с MacBook по доверенной LAN, просил Secretary выполнить работу, получал короткое подтверждение делегирования и мог продолжать разговор, пока Worker работал.

Bridge проверял минимальные contracts: Dispatch, persistent Worker binding, terminal Result и Follow-up тому же Worker. Он был одноразовым прототипом, а не частью нынешнего Go Secretary server.

## Состав

| Файл | Назначение |
| --- | --- |
| `secretary_bridge.py` | Python CLI для AgentHub HTTP API и локального Worker binding state. |
| `secretary_coordinator_AGENTS.md` | Профиль Secretary для derived Coordinator Workspace. |
| `secretary_coordinator_prompt.txt` | Исходная версия prompt с правилами делегирования и Follow-up. |
| `README.md` | Установка, конфигурация и примеры команд. |

`__pycache__/` содержал только локальный Python-кэш и не был частью tracked source.

## Команды и исполнение

`delegate TASK`:

1. Создавал opaque `worker_ref`, отдельный workspace и callback capability.
2. Проверял заранее созданный AgentHub Team и работающую Origin Coordinator session.
3. Создавал AgentHub agent и добавлял его в Team через HTTP API.
4. Сохранял binding с AgentHub agent/team/session, workspace, Task и Attempt.
5. Отправлял Task envelope с командой terminal callback в Worker.
6. Возвращал `accepted` после передачи input.

`send-follow-up WORKER TEXT` направлял input в существующего AgentHub agent, увеличивал Attempt и выдавал новый callback capability. Для active Attempt возвращал `not_ready`; durable очередь не реализовывалась.

`report-result` принимал `succeeded`, `failed` или `canceled`, проверял active Attempt и capability, сохранял summary, переводил Attempt в terminal и удалял capability. Затем направлял сообщение с Result в сохранённую Origin Coordinator session. Coordinator должен был опубликовать его verbatim. Это не была современная прямая server-owned доставка Result клиентам.

`show [WORKER]` печатал binding либо весь local state. Он не был безопасным public DTO: state мог содержать capability и внутренние IDs.

## State и конфигурация

- Config по умолчанию: `~/.config/secretary-bridge-poc/env`, без group/world permissions.
- State по умолчанию: `~/.secretary-bridge-poc/state.json`, права `0600`.
- State записывался через временный файл и atomic replace.
- Credentials передавались в AgentHub HTTP API как Bearer token.
- Настройки определяли AgentHub URL, operator token, Team, Origin agent, workspace root, ACP launcher command/args.
- Provider-default пример запускал Codex через AgentHub ACP command.

Фактические tokens и содержимое локальных credentials в этот документ не переносились. Удаление `poc/` не удаляет эти внешние config/state/workspace paths.

## Политика Secretary

Новая исполнимая просьба вызывала только `secretary-bridge delegate`. После acceptance Secretary сообщал о делегировании и заканчивал turn, не выполняя задачу сам и не ожидая Worker.

Явно адресованный Follow-up вызывал `send-follow-up`. Ошибка или `not_ready` показывались без automatic retry и самостоятельной очереди. Простые вопросы без исполнения Secretary отвечал сам.

Final версия `secretary_coordinator_AGENTS.md` отдельно требовала публиковать сообщение `Secretary Bridge Result` без делегирования, интерпретации и комментариев. Чтобы AgentHub применил workspace policy, использовалась новая provider session: одного изменения Team member prompt было недостаточно для direct ACP input.

## Историческое evidence

`.scratch/local-agenthub-secretary/map.md` фиксирует создание Worker, выполнение `sleep 60`, принятие callback, передачу Follow-up в ту же session и возврат Result в Secretary conversation. Это исторические записи прототипа, не новый прогон при удалении.

Stock AgentHub UI был признан operator/debug-консолью, а не окончательным пользовательским интерфейсом. Дальнейшая архитектура отказалась от AgentHub как постоянного control plane/runtime.

## Ограничения

- Bridge и agents работали с правами одного Unix user; ограничения команд не были security isolation.
- Operator token выдавался вручную, автоматического renewal не было.
- Team и Origin Coordinator session должны были существовать заранее.
- Delivery зависела от сохранённой работающей Coordinator session.
- Не было public Client API, retention, remote Nodes, современной durable delivery/replay и полноценного credential lifecycle.
- Ошибка HTTP могла оставить созданного Worker для ручного осмотра.
- Atomic запись JSON не обеспечивала транзакций между API и state или безопасной конкурентной записи несколькими CLI processes.
- Result сохранялся как terminal до отправки Coordinator. При delivery failure повторный callback уже отклонялся; durable outbox/retry не было.

## Последствия удаления

Удаляется только project directory `poc/`, вместе с Python-кэшем. Go server, Web, Telegram, существующие Workers, внешние Bridge installations и always-on сервер omarchy не изменяются.

Исторические задачи в `.scratch/local-agenthub-secretary/` сохраняются. Их ссылки на удалённые `poc/` files теперь относятся к исходникам в Git history. Запись `poc/__pycache__/` в `.gitignore` остаётся безвредной исторической записью. Эти файлы не перерабатывались в рамках удаления.

Для текущей разработки использовать contracts Go Secretary server и Phase 4. Не переносить в них автоматически JSON state, Coordinator callbacks, manual tokens и trusted-local допущения этого прототипа.
