# Claude native streaming input spike — 2026-10-09

## Результат

Mac Claude Code 2.1.295 поддерживает настоящий persistent JSONL stdin, deferred startup, initialize, idle interrupt receipt и явный stdio MCP connect. Повторный process принимает `--resume` и сохраняет native session ID. Настоящий model turn, использование MCP tool моделью, применение steering на границе длинного инструмента и interrupt активного model turn **не проверены**: существующий настроенный provider отвечает HTTP 403 «недостаточная квота». Без env provider CLI выдаёт `Not logged in`. На omarchy Claude Code 2.1.270 найден через login shell, `auth status` loggedIn=false; ~/.claude/settings.json отсутствует и текущий SSH env не содержит ANTHROPIC/CLAUDE credentials. Credentials не искались за пределами штатного metadata/settings пути и не выводились.

Нельзя утверждать ни «Claude same-turn steering доказан», ни «Claude умеет только очередь». Installed bundle содержит native mid-turn folding сообщений на безопасных границах. Документированный `send_now` может прервать turn; он не эквивалент same-turn steering.

## Выполненные команды

- `claude --version`, `claude auth status`, `claude --help`.
- `ssh omarchy 'bash -lc "command -v claude; claude --version; claude auth status"'`.
- `python3 /private/tmp/phase5-claude-spike/probe.py steer`.
- `python3 /private/tmp/phase5-claude-spike/control_probe.py`.
- `python3 /private/tmp/phase5-claude-spike/resume_probe.py resume`.

Product/repo не изменялся. Fixtures находятся здесь. Первые два собственных CLI transcripts были автоматически записаны CLI в ~/.claude/projects/-private-tmp-phase5-claude-spike-workspace; затем перемещены в изолированный `claude-config/projects/` здесь, исходный пустой каталог удалён. Ни чужие sessions, ни processes не изменялись. `probe.py` теперь использует `CLAUDE_CONFIG_DIR` в этом temp каталоге. Env авторизованного существующего provider читается в память subprocess и не сериализуется. Скрипт ограничен 90s и `$0.75`; observed total_cost_usd=0.

## Точные наблюдения

### Deferred start / Prompt / ошибки

Process со stream stdin пережил 2 секунды без user input; после `initialize` пришёл `control_response`. Первый user input вызвал `system/init` с native session ID. `--replay-user-messages` возвратил исходный user UUID, `isReplay:true`. В result присутствуют `user_message_uuid`, `user_message_uuids`, `queued_turn_count`, `result_index`, native `session_id`.

Configured provider request вернул authentication_failed/HTTP403 до первого реального assistant/tool response. Result неожиданно имеет `subtype:"success"`, но `is_error:true`, `terminal_reason:"api_error"`, `api_error_status:403`. Поэтому **subtype success сам по себе не означает успешную Attempt**. Process exit=1. Native assistant message id и envelope UUID — не turn ID; отдельного native turn identifier в наблюдавшихся кадрах нет. Input UUID и result index следует хранить отдельно.

### Безмодельный control + MCP

- t=0.000 initialize sent; t=0.246 success control response; `session_state:"idle"`.
- t=3.059 `{type:"control_request",request_id:"idle-interrupt",request:{subtype:"interrupt"}}`.
- t=3.061 response success `{still_queued:[]}`; process жив.
- t=5.072 mcp_status sent; t=5.075 connected server `phase5readonly`, tool `read_marker` listed.
- t=8.700 stdin EOF завершил process, exit=0.

Fixture MCP server получил server/discover, initialize, notifications/initialized, tools/list. tools/call не происходил: модель не работала. Это доказательство передачи `--mcp-config` и handshake/list, **не** доказательство Secretary MCP lifecycle authorization. Config shape: `{"mcpServers":{"phase5readonly":{"command":"python3","args":["/private/tmp/phase5-claude-spike/mcp_fixture.py"]}}}`. `--strict-mcp-config` исключает чужие MCP definitions.

### Resume

Изолированный собственный transcript исходной fixture session `f7379b80-7ab7-4377-9de6-b959e4659104` загружен CLI через `--resume` из temp CLAUDE_CONFIG_DIR; deferred process и последующие `command_lifecycle`, `system/init`, user replay и result сохраняют этот session_id. Модельный ответ на вопрос о BASE marker не получен (Not logged in): семантическое восстановление истории **не доказано**. Нельзя закрывать full resume acceptance этой проверкой.

## Минимальный wire contract для Go adapter

Вызов: `claude --print --input-format stream-json --output-format stream-json --verbose --replay-user-messages` плюс runtime-owned session/MCP/settings/policy options. `Start` создаёт process, stdin writer и постоянный stdout reader, не посылает пустой Task. Prompt сериализуется одной строкой JSON:

```json
{"type":"user","uuid":"<server input UUID>","session_id":"<native session UUID>","message":{"role":"user","content":"<text>"},"parent_tool_use_id":null}
```

Optional startup handshake (observed real):

```json
{"type":"control_request","request_id":"<unique id>","request":{"subtype":"initialize"}}
```

`control_response.response.request_id` коррелирует control request. Pending controls требуется обслуживать reader-ом; response может прийти до init/user события. Cancel (source capability-gated):

```json
{"type":"control_request","request_id":"<unique id>","request":{"subtype":"interrupt","cancel_queued":true}}
```

`cancel_queued:true` удаляет оставшиеся stamped user inputs; plain interrupt сохраняет очередь. receipt — принятие прерывания, **не terminal turn result**. Active interrupted turn заканчивается последующим result; не отправлять два terminal Result одной Attempt. Старые CLI, не объявляющие interrupt_cancel_queued_v1, игнорируют это поле; поэтому `/q` нужно оставлять server-owned до idle.

Обычный steering candidate: отправить тот же user envelope активной session без interrupt. Bundle принимает `priority:"next"` (также `now`/`later`), но published CLI docs не задаёт стабильный priority contract. Обычный default user достаточно исследовать первым. Пока live test blocked, capability/status должны явно различать implemented transport и verified acceptance; нельзя объявлять guaranteed same-turn capability по одному write/ack.

Explicit Send now (НЕ default steering):

```json
{"type":"control_request","request_id":"<unique id>","request":{"subtype":"interrupt","send_now":true,"message_uuid":"<already sent user UUID>"}}
```

Только если `system/init.capabilities` включает `interrupt_send_now_v1`. В native schema описано: pending сообщение может быть absorbed после backgrounding wait, либо CLI abort текущего turn (`error_during_execution`). response `send_now` = `stopped` / `delivering` / `nothing_waiting`; даже delivering не гарантирует сохранение turn — CLI может abort позже. Не называть это same-turn steering. При необходимости interrupt+requery явно учитывать границу Attempt.

Измеренные system/init capabilities 2.1.295: interrupt_receipt_v1, interrupt_cancel_queued_v1, interrupt_send_now_v1, msg_lifecycle_v1, request_marker_lists_v1, sdk_mcp_tools_list_changed, sdk_mcp_manifests, mcp_read_resource_v1, mcp_tool_ui_meta_v1, ui_surface_v1. Initialize response перечисляет меньший набор; использовать system/init для version-gated поведения.

Reader: system/init сохраняет native session; command_lifecycle сохраняет accepted/started/completed/cancelled input metadata; user isReplay acknowledgement не выдаёт user-visible duplicate; assistant complete blocks/tool blocks публикуются один раз. Если включать `--include-partial-messages`, нельзя одновременно дописывать полное assistant content в тот же text accumulator. Result classification учитывает is_error/terminal_reason/subtype, process EOF и result корреляцию. При EOF нет result для активной Attempt — transport failure, не успех.

## Installed bundle свидетельство, а не live acceptance

Файл `/Users/beruseruko/.local/share/mise/installs/node/latest/lib/node_modules/@anthropic-ai/claude-code/bin/claude.exe`, 2.1.295. Точечные source snippets сохранены как interrupt-schema.txt, input-queue.txt, preempt.txt.

- Byte 187388xxx: schema `subtype:interrupt`, cancel_queued/send_now descriptions and caveats, receipt contract.
- Byte 192888279: send_now predicate true only when send_now=true and cancel_queued!=true.
- Byte 194656253: priority accepts now,next,later.
- Byte 203116064: active query loop gets queued commands through `getCommandsByMaxPriority("next")`, emits queued_command attachments, tracks fold-in-flight and consumed messages. Byte203121009 screening may defer inputs; no guaranteed timeout.
- Byte221560xxx: streaming request dispatch (initialize, interrupt, end_session).
- Byte221644430: incoming user converted to queued prompt preserving UUID and priority.
- Byte221068xxx: arrival preemption behind feature gate; implicit interrupts are another reason version+live semantics must be measured.

Не опираться на undocumented source symbols в production parser. Они объясняют гипотезу safe-boundary folding и риск send_now, но не заменяют native model run.

## Следующая приёмка после восстановления квоты

`python3 /private/tmp/phase5-claude-spike/probe.py steer` удерживает контролируемый Bash `sleep 12; printf TOOL_DONE_937` и через 1s после первого assistant tool_use отправляет уникальный STEER_MARKER_641. Требуется доказать: user UUID replay/started, применение marker в final до отдельного result исходного turn, stable session ID, отсутствие отдельного first-turn terminal перед steering response. Затем `probe.py interrupt` проверяет active interrupt + terminal drain + follow-up memory. Текущий harness закончился до tool call, поэтому измерений marker/timing нет. Отдельно проверить результат первой turn, idle second Prompt, `/q` held server-side, resume marker recall, read-only Secretary MCP tool call, обоих Nodes.

## Официальные источники

[Streaming input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode) документирует persistent session, sequential queued messages, interruptions и MCP. Это не гарантия same-turn injection.

[CLI reference](https://code.claude.com/docs/en/cli-reference) подтверждает stream-json input/output, replay-user-messages, resume, mcp-config и strict-mcp-config. Internal send_now/receipt fields выше подтверждены installed bundle и частично control-only live probe, а не опубликованным стабильным CLI contract.
