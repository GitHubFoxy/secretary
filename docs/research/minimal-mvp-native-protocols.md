# Минимальный MVP: Codex и Claude Code

Дата проверки: 2026-10-09. Исследование протоколов; продуктовый код не менялся. Для OpenAI использована официальная документация через скилл OpenAI Docs.

## Рекомендация

Самый короткий путь — сохранить Go server, Execution node и уже работающий Codex `ACPRuntime` через `codex-acp`. Проверить его настоящую доставку steering и resume, прежде чем менять протокол. Для Claude Code провести короткую проверку двустороннего streaming input. Если требуется управляемое прерывание, использовать небольшой процесс с официальным Claude Agent SDK и простым JSONL-интерфейсом к Go. Не писать собственный agent loop и не переносить server lifecycle в SDK.

Есть существенное ограничение: опубликованная документация Claude Agent SDK подтверждает очередь сообщений и прерывание, но не гарантирует добавление нового требования в текущий turn без остановки. Поэтому требование «настоящий steering по умолчанию для обоих harness» пока не доказано для Claude. Нельзя закрыть этот пункт одной заменой `--output-format stream-json` или назвать interrupt + новый запрос настоящим same-turn steering.

## Подтверждённые возможности

### Codex app-server

Это официальный двусторонний протокол: `codex app-server` по stdio, JSONL запросы/ответы и события. После `initialize` и `initialized` клиент создаёт `thread/start` либо загружает `thread/resume`, затем вызывает `turn/start`. `turn/steer` добавляет input в активный turn, требует `expectedTurnId`, возвращает принятый `turnId` и не создаёт нового `turn/started`. Для остановки используется отдельный `turn/interrupt`; окончание приходит через `turn/completed` со статусом `interrupted`. Клиент различает принятие input и фактическое использование требования агентом. Документация протокола не задаёт гарантированный срок реакции во время долгого инструмента. [Официальный app-server](https://developers.openai.com/codex/app-server/).

Этот набор подходит для прямого Go-адаптера, если существующий `codex-acp` не пройдёт проверку. Нативные thread и turn identifiers нужно хранить отдельно от Worker reference, Attempt и server-issued input identity. Не использовать название протокола как доказательство доставки ровно один раз.

### Claude Agent SDK

Streaming input поддерживает долгоживущую интерактивную сессию, последовательную обработку сообщений, interrupt, MCP и запросы разрешений. Это отличается от single-message input с отдельными запросами и resume. Само наличие async input generator не доказывает, что очередной input изменяет незавершённый turn. [Streaming input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode).

Python `ClaudeSDKClient` даёт `connect`, `query`, `receive_messages`, `receive_response`, `interrupt`, `disconnect`. Официальный пример перенаправления работы: вызвать interrupt, вычитать сообщения прерванного запроса до его `ResultMessage`, затем отправить новый query. Буфер после interrupt не очищается; иначе старый результат можно принять за ответ на новый запрос. Это подтверждённое interrupt + follow-up в той же сессии, а не подтверждённый same-turn steer. Новые поля вроде `terminal_reason` в актуальной документации нужно отдельно сверить с установленной версией SDK/CLI. [Python SDK](https://code.claude.com/docs/en/agent-sdk/python).

### Claude headless CLI

CLI документирует `--input-format stream-json`, `--output-format stream-json`, `--replay-user-messages`, `--resume`, `--mcp-config`, `--strict-mcp-config`. Это позволяет исследовать постоянный stdin без SDK. Публичный CLI reference не описывает полноценный стабильный wire contract control requests для interrupt и permissions; самостоятельно воспроизводить внутренний SDK transport сложнее сопровождать. [CLI reference](https://code.claude.com/docs/en/cli-reference).

Для токеновых событий нужен `--include-partial-messages`. При добавлении этих событий нельзя одновременно дописывать полный assistant text в тот же accumulator: получится дублирование текста. SIGTERM оставляет turn незавершённым; официальная документация рекомендует SIGINT или SDK interrupt для завершения turn перед остановкой процесса. [Headless](https://code.claude.com/docs/en/headless).

## Что уже есть в проекте

| Компонент | Текущее поведение | Следствие для MVP |
| --- | --- | --- |
| `internal/node/claude_runtime.go` | Один process на запрос: `--print --output-format stream-json --verbose`, задача в argv, stdin pipe отсутствует. Resume запускает новый process с session ID. `Steer` unsupported, `Prompt` и `Queue` требуют новой Attempt; cancel убивает process. | Это one-shot runtime. Интерактивность нельзя включить только флагом capabilities. |
| `internal/node/acp_runtime.go` | `session/new`, `session/load`, `session/prompt`, `session/cancel`, MCP servers; steering вызывает расширение `_session/steering` и признаёт `injected` / `startedNewTurn`. | Уже есть большая часть необходимого адаптера. `startedNewTurn` нужно отличать от инъекции в активный turn. |
| `internal/node/harness_probe.go` | Codex объявляет steering; Claude Code не объявляет steering/approvals. | Capabilities должны соответствовать измеренному контракту конкретной версии. |
| `cmd/secretary-node/main.go` | Codex запускается через `codex-acp`, default harness — OpenCode. | Для узкого MVP меняются defaults/доступный выбор; новый узловой транспорт не требуется. |
| `internal/mcp/secretary.go` | `list_nodes`, `list_projects`, `list_workers`, `get_worker`, `spawn_worker`, `message_worker`, `cancel_worker`, `close_worker`; opt-in `reply_to_user`. | Сохранить эти инструменты и authority Secretary server. |

Соседнее исследование runtime сообщает, что установленный `codex-acp 1.12` содержит реализации `_session/steering` и `session/load`. Это локальное свидетельство реализации, не независимый live acceptance test данного исследования. Подчёркивание в имени метода само по себе не повод переписывать работающий адаптер.

Существующий `docs/architecture/runtime-contracts.md` уже задаёт: обычный input — Steering message на ближайшей безопасной границе, `/q` — доставка только в idle; resume восстанавливает существующую runtime session; restart не повторяет активную Attempt автоматически. Эта рекомендация сохраняет контракт. Узкий MVP только Codex/Claude потребует отдельно обновить зафиксированные defaults OpenCode в документации. `docs/adr/` отсутствует.

## MCP и local/remote nodes

Codex поддерживает MCP stdio и Streamable HTTP; HTTP конфигурация включает `url`, `bearer_token_env_var`, дополнительные headers, allowlist `enabled_tools`, а `required` делает ошибку подключения блокирующей. Предпочтительно передавать capability token через окружение процесса, сохраняя отдельную конфигурацию runtime. Не менять глобальный `~/.codex/config.toml` ради одной Secretary session. Точный способ session-scoped конфигурации app-server проверить на установленной версии. [Codex MCP](https://developers.openai.com/codex/mcp/).

Для Claude первый spike должен подтвердить доступность существующего Secretary MCP endpoint через `--mcp-config` либо SDK options и успешный вызов read-only инструмента. Затем проверить lifecycle tool с точной server identity. Новая MCP-система и новый набор tools для MVP не нужны.

Оба runtime запускаются там, где находится Execution node. Local и remote Node используют существующее outbound соединение к Secretary server, локальные workspace, runtime session store и локальные credentials. App-server stdio и SDK subprocess не следует выставлять наружу как отдельный remote API. URL MCP должен быть доступен с соответствующего Node; `localhost` удалённого Node не указывает на Secretary host. После reconnect неизвестное принятие steering не превращать в автоматическую повторную отправку.

## Минимальные шаги реализации после выбора варианта

1. **Codex acceptance до переписывания.** На закреплённых версиях `codex`/`codex-acp` запустить долгий безопасный инструмент; передать уникальное уточнение через `_session/steering`; проверить принятие, появление требования до естественного окончания работы и сохранение той же сессии/активного turn. Отдельно проверить `/q`, cancel, `session/load`, read-only Secretary MCP. Ответ `injected` сам по себе недостаточен.
2. **Claude spike с чётким критерием.** В постоянный stream-json stdin либо `ClaudeSDKClient` передать input, пока выполняется длинный инструмент. Установить: input применяется в текущем turn на безопасной границе, запускается лишь после завершения, либо требует interrupt. Зафиксировать события и session ID. Не повышать `CapabilitySteering` до доказательства первого варианта.
3. **Claude адаптер по результату.** Если same-turn steering подтверждён — реализовать минимальный двусторонний Session вокруг доказанного механизма. Если подтверждена только очередь/interrupt — SDK bridge остаётся подходящим для интерактивной сессии, но требования steering надо изменить явно. Без такого решения задача настоящего steering для Claude остаётся блокером, а не «готовой с fallback».
4. **Сохранить границы Go.** Process читатель постоянно принимает события; запросы stdin сериализуются; Go хранит runtime session ID и native turn ID, durable input/command identity и authoritative Attempt lifecycle. `/q` остаётся server-owned очередью до idle. MCP lifecycle operations остаются на server.
5. **Не путать runtime stop и Result.** Для interrupt + follow-up явно решить границу Attempt: по текущему контракту одна Attempt заканчивается одним Result. Не публиковать canceled и succeeded как два terminal Result одной Attempt. Для SDK обязательно вычитать terminal события прерванного запроса перед новым.
6. **Узкий запуск MVP.** Выбор только Codex/Claude, worker default после подтверждения возможностей; использовать существующие local/remote node enrollment и binding. Сделать одинаковый acceptance сценарий на обоих Node без нового deployment механизма.

Если Codex ACP acceptance провалится, резервная замена ограничена одним Codex runtime: JSONL app-server, handshake, thread start/resume, turn start/steer/interrupt, постоянный event reader и ответы на server-initiated approval requests. Эта замена не должна становиться обязательным первым этапом MVP.

## Что исследование не доказало

- Истинную same-turn семантику Claude steering на установленной версии CLI.
- Время применения принятого steering во время долгого/зависшего tool call для обоих harness.
- Совместимость всех текущих SDK fields с закреплёнными CLI версиями.
- Живую доставку MCP capability и поведение resume после остановки process на конкретных Node.

Это отдельные короткие проверки протокола. Исследование не выполняло модельных запросов, подключения по SSH или изменений продуктового кода.
