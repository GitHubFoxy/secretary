# Native MCP tool activity в ACP

Исходный integration snapshot: `372bee3`. Работа выполнена в отдельном worktree `/private/tmp/secretary-p5-cc-close`, ветка `p5/cc-close`; пользовательские spec изменения и работающий native state не редактировались.

## Воспроизведение и причина

Обязательный issue 04 criterion включает Worker tool/status в observer. Live actual MCP tools/call состоялся, но Activity API не показывал его. Безопасный load-only native ACP 1.12 replay собственной сессии дал `/private/tmp/p5-mac-independent/wire-tool-metadata-safe.json`: `tool_call`, `toolCallId`, `kind=execute`, `status=completed`, `title=mcp.acceptance.read_acceptance_marker`, `_meta`, `rawInput` с keys `arguments/server/tool`, `rawOutput`; полей `name/toolName/tool_name` нет. Load-only probe не выполнял Prompt/model call и не сохранял raw history, аргументы, outputs, reasoning или secrets.

Installed primary source `/opt/homebrew/lib/node_modules/@agentclientprotocol/codex-acp/dist/index.js` версии 1.12.0 подтверждает: `createMcpToolCallUpdate` устанавливает `_meta.is_mcp_tool_call=true` и передаёт explicit server/tool/arguments в rawInput. `completeItemEvent` передаёт sparse update с прежним toolCallId без name/MCP flag. Поэтому прежний фильтр `tool_call` без имени отбрасывал реальную invocation ещё до tracker dedup.

Ранжированные проверки: name filter, invocation correlation по toolCallId, nested arguments normalization. Публичный runtime regression подтвердил первую причину: `go test ./internal/node -run '^TestACPRuntimeShowsNativeCodexMCPActivity$' -count=1` — FAIL до исправления, `native completed MCP activity=[]`. Он читает `ACP Runtime/Session.Activity`, затем публичный `NormalizeRuntimeActivity`; private tracker helper не является seam нового теста.

## Изменение

14 строк в существующем tracker: MCP frame с явным provenance получает identity `mcp.<server>.<tool>` из semantic rawInput, а не из display title. Arguments извлекаются из rawInput.arguments. Completion сохраняет прежнюю identity и unwrap только при совпадении семантической MCP identity с явным именем либо tracked invocation. Existing toolCallId, lifecycle, concurrent/idless correlation и dedup не переписывались.

Completed initial tool_call остаётся одним tool_result; synthetic start не создаётся. Title не определяет tool identity и не попадает в normalized telemetry. Без explicit имени или подтверждённого MCP provenance кадр не превращается в выдуманную tool card. Existing sanitizers формируют redacted argument preview/output; тест подставляет безопасные fixture secrets и проверяет отсутствие их значений и private display title в normalized event.

## Non-MCP и предел свидетельств

Отдельный actual load-only probe `/private/tmp/p5-mac-independent/wire-sleep-metadata-safe.json` подтвердил non-MCP exec: `name=exec_command`, `kind=execute`, rawInput command/cwd и sparse completion без name; wait: `name=wait`, kind other и sparse completion. Эти native явные имена уже поддерживались. Новая публичная fixture проверяет их unchanged lifecycle и duplicate completed suppression, concurrent MCP calls и ambiguous idless update. Generic fallback по title/kind не добавлен.

Это исправление подтверждённой MCP shape и проверка реально наблюдённых exec/wait shapes. Оно не объявляет поддержку всех title-only native категорий или полный live observer gate. После integrated build нужен свежий native Activity API/observer check; renderer fixture и load-only metadata сами по себе не заменяют его.

## Проверки

- `TestACPRuntimeShowsNativeCodexMCPActivity` — RED до исправления, PASS после.
- `TestACPRuntimeNativeToolIdentityAndLifecycle` — PASS.
- `go test -race ./internal/node -run 'ACPRuntime|ACPToolTracker|NormalizeRuntimeActivity|ToolArgumentPreview|ToolPreview|ToolResult|ToolArguments|NativeTool|Activity' -count=1` — PASS.
- `go test -race ./internal/core -run 'NodeActivity|Activity|Harness' -count=1` — PASS.
- `go build ./...` — PASS.
- `git diff --check` — PASS.

Native MCP prompt workaround, изменение auth/config и новые explanatory code comments отсутствуют. Claude quota403, remote/Telegram и прочие открытые native gate constraints этим исправлением не закрыты.
