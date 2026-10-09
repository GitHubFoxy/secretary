# Phase 5: локальная проверка Codex

Дата: 2026-10-09. Host: Mac пользователя. Проверки выполнены в отдельных временных Workspace, без изменения production services, credentials или глобального config. Это частичная приёмка ticket 01, а не завершение deployment gate.

## Реальный runtime

- `codex-cli 0.162.0`, `@agentclientprotocol/codex-acp 1.12.0`.
- `secretary-node --check-codex-readiness`: PASS. Проверены фактический Codex executable/version/auth и adapter initialize. Наблюдались protocol 1, версия adapter, `loadSession: true`, `_meta.steering.supported: true` и каталог native session/new (семь model IDs в итоговом запуске).
- SHA-256 содержимого существующего `~/.codex/config.toml` до и после readiness совпал. Значения config и credentials не публиковались.
- Новый default выбирает native model/reasoning (`default`), без OpenCode provider prefixes. Явные pins остаются точными, adapter обязан подтвердить selection.

`SECRETARY_NATIVE_CODEX_TEST=1 mise exec -- go test ./internal/node -run '^TestCodexNativeProfileFinalResume$' -count=1 -v`: PASS, два model calls за 24 секунды.

- Profile через session-scoped `CODEX_CONFIG.developer_instructions`: final answer содержит `PHASE5_CODEX_NATIVE_73`.
- Native session: `01a1218c-f41e-7bd1-b6e9-5054b8792943`.
- После Close, нового process и `session/load` identity сохранилась; второй final answer содержит ранее сообщённый `FOLLOWUP_91`.
- Только `_meta.codex.phase=final_answer` вошёл в Result. Commentary остаётся Activity. Пустой/неполный/error terminal не превращается в `completed`.

`SECRETARY_NATIVE_CODEX_TEST=1 mise exec -- go test ./internal/node -run '^TestCodexNativeSteeringDuringSafeTool$' -count=1 -v`: PASS, один model turn за 25 секунд.

- Runtime исполнил безопасный `sleep 12` в отдельном Workspace.
- Во время `tool_call` со статусом `in_progress` отправлено изменение final marker.
- `_session/steering` вернул `injected`, не `startedNewTurn`.
- Одна `Prompt`, session `01a12190-3e29-7ae2-b4a9-f7d146e30a09`, естественный terminal после работы содержит `STEER_APPLIED_NATIVE_87`.
- Adapter 1.12 имеет race fallback на новый turn. Codex Session отклоняет idle steering; если native ответ всё же `startedNewTurn`, запрашивает cancellation и возвращает явную неопределённость выполнения, без успеха или автоматического повтора.

## Детерминированные проверки

Scoped packages `internal/config`, `internal/node`, `internal/ctl`, `internal/secretary`, `cmd/secretaryd`, `cmd/secretary-node`: PASS.

Новый public outbound Node fixture проверяет Codex managed profile на внешней process boundary, native delivery metadata, frozen binding, same-session idle Follow-up, Node restart, идемпотентный Result, cancel и explicit resume. До исправления binder проверка падала: native process получал `workspace_instructions` вместо `native`. Это executable fixture, не model call.

Native Session fixtures проверяют явные Codex final phases, пустой/неполный terminal, idle steering и rejection `startedNewTurn`. CLI canary `scripts/phase5-setup-test.sh`: PASS. Clean setup/Doctor/standalone Node не вызывают FX/OpenCode, не создают native store manifests, сохраняют global config canary. Readiness executable в CLI canary синтетический.

## Остаётся deployment gate

На enrolled local/remote Nodes необходимо проверить настоящий Secretary MCP read-only call и spawn, Worker Result в общей Conversation ровно один раз без Secretary turn, actual Worker MCP capability, Node restart/resume в deployed topology, Web/Telegram и отсутствующую native session. Native addressed-reply-v1 для Codex не объявляется: выбран canonical assistant final, opt-in addressed contract отклоняется явно. Ticket 01 не закрыт до этих свидетельств.
