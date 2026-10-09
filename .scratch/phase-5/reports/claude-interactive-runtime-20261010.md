# Интерактивная Session Claude — 2026-10-10

## Результат

Native streaming adapter реализован через установленный Claude Code 2.1.295 на Mac. SDK bridge не требуется для deferred Start, Prompt, MCP, profile, interrupt и resume. Same-Attempt steering **не доказан и не объявлен**: реальные запросы заканчиваются provider HTTP403 до первого tool_use. CLI transport input, ACK и найденный в bundle folding не заменяют model acceptance.

## Реализация и границы

`ClaudeCodeRuntime.Start` запускает persistent JSONL stdin/stdout process. В deferred режиме Task не передаётся как positional CLI argument и модель не вызывается до Prompt. Prompt принимает только idle Session; active ввод через Steer даёт `ErrClaudeCodeSteeringUnsupported`. Queue возвращает явную ошибку: durable `/q` остаётся server-owned.

Profile и managed skill content доставляются через `--append-system-prompt`; LocalNode больше не переписывает AGENTS.md ради Claude. Managed tool names преобразуются в native `--tools` и `--allowedTools`; dontAsk исключает необслуживаемый native approval. MCP — explicit `--strict-mcp-config`; env values приходят через namespaced subprocess environment и `${VAR}` expansion, не через argv. Глобальные auth/config не меняются; provider credentials не копируются.

Reader использует complete assistant blocks; partial stream frames, user replay, control receipt и subagent content не добавляют повторный пользовательский текст. Input UUID отделён от native session ID. UUID correlation отклоняет terminal другого input, duplicate assistant envelope не выводится повторно. `is_error`, error subtype/terminal_reason и пустой terminal дают failed Result. EOF активной Attempt даёт transport failure. Wrong native session ID завершает process явной ошибкой.

Cancel отправляет native interrupt и удерживает active Attempt до terminal result. Receipt не считается Result. Новый Prompt раньше terminal отклоняется; старый terminal не привязывается к следующему input. Secretary сохраняет interactive process. ExecutionNode закрывает Claude Worker process после terminal и продолжает native transcript через --resume в новой Attempt; это исключает concurrent writers прежней session.

Resume делает native initialize handshake до возврата Session. Missing transcript/early exit отклоняется до Prompt, новая session не создаётся. Profile с addressed-reply-v1 отклоняется: grouping этого opt-in контракта не доказан; legacy canonical reply/completion guard остаётся прежним.

## Реальные наблюдения

Host: Mac, executable `/Users/beruseruko/.local/share/mise/installs/node/latest/bin/claude`, CLI version 2.1.295. Протокольный spike использует изолированный temp CLAUDE_CONFIG_DIR, runtime probe — штатную пользовательскую конфигурацию и собственный пустой temp workspace. Личные prompts, reasoning и credentials в отчёт не включены.

1. Повтор `python3 /private/tmp/phase5-claude-spike/probe.py steer`: native session `d0c2bb8a-306b-40ca-b057-fd8ff15eaf3b`, user input UUID `e3758863-f09c-4425-b96f-fb03ef93e2b3`. Deferred process жив; initialize success, lifecycle, system/init, replay, assistant и result приходят. Result: subtype success, is_error true, terminal_reason api_error, api_error_status 403, cost 0. До Bash sleep/tool_use не дошёл, steering marker не отправлен. Временные stdout/raw записи остаются в `/private/tmp/phase5-claude-spike/`; это не runtime acceptance PASS.
2. Штатный альтернативный путь с `--setting-sources ''`, normal Claude config directory, `--model sonnet`, пустыми tools и бюджетом $0.10: native session `a4013c87-66b6-4ac1-a4df-b23eb8c0dcc1`, exit 1, is_error true, `Not logged in · Please run /login`, cost 0. Credentials file `.claude/.credentials.json` отсутствует. Обычный auth status loggedIn=true/oauth_token относится к configured AUTH_TOKEN; он не доказал действующую subscription authorization. Keychain и чужие credentials не извлекались.
3. Временный Go probe через public `node.ClaudeCodeRuntime.Start`/`Session.Prompt`: native session `91a1c49a-6135-4cad-8734-a9382002b24e`; deferred Start, managed profile marker `NATIVE_PROFILE_644`, read tool policy и stdio MCP переданы новому adapter. Prompt вызывает MCP read_marker и требует marker; фактический Result.Status=failed, summary содержит HTTP403. Native MCP fixture получил server/discover, initialize, notifications/initialized, tools/list. tools/call отсутствует: доказано подключение MCP, не использование инструмента моделью и не semantic profile delivery.
4. Через тот же Go public runtime Resume с отсутствующим session ID `462620de-1ed6-49ba-9ab1-3bc529a57648` возвращает ошибку initialize до Prompt. Отдельный native CLI missing-resume probe вернул result subtype error_during_execution и exit 1. Hidden new session/retry отсутствуют.

Для существующих fixture transcripts CLI принимает --resume с прежней session identity (предыдущий [spike](claude-streaming-spike-20261009.md)); память модели после restart по-прежнему не проверена.

## Детерминированные проверки

На public Session seam проверены deferred Start, два idle Prompt, native argv/profile/MCP policy, отказ active Prompt, interrupt и terminal drain перед Follow-up, отсутствие repeated assistant text, игнорирование partial frames, empty terminal failure, видимый deferred startup failure, initialize/resume identity, missing-resume отказ. Existing server continuation/reply/completion tests проходят без ослабления guards.

- `MISE_TRUSTED_CONFIG_PATHS=/private/tmp/secretary-p5-02 go test ./...` — PASS.
- `MISE_TRUSTED_CONFIG_PATHS=/private/tmp/secretary-p5-02 go test ./internal/node -run TestClaude -race -count=1` — PASS.
- `git diff --check` — PASS.

## Оставшийся blocker

Нужен работающий авторизованный native Claude model backend на enrolled Mac. Текущий provider отвечает quota403; штатная first-party авторизация без этого provider отсутствует. Это внешний blocker, а не доказательство отсутствия native steering. После восстановления доступа повторить safe-tool spike с input во время sleep и доказать marker до первого terminal той же Attempt; затем semantic profile/MCP lifecycle, idle recall и restart recall через реальные Nodes. CapabilitySteering, ticket 02 и Phase 5 остаются незакрытыми до этих свидетельств.

Официальные источники: [CLI reference](https://code.claude.com/docs/en/cli-reference), [streaming input](https://code.claude.com/docs/en/agent-sdk/streaming-vs-single-mode), [MCP env expansion](https://code.claude.com/docs/en/mcp#environment-variable-expansion-in-mcpjson). Streaming docs описывают persistent input, sequential queue и interruptions; гарантии same-turn injection из них не следует.
