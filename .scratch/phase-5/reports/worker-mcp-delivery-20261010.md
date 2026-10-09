# Доставка Node-local MCP в Worker runtime — 2026-10-10

На базе integration `18f0baa` добавлен optional `DeploymentConfig.mcp_servers` с существующим stdio типом MCPServer. `secretary-node` передаёт список в Daemon, затем ExecutionNode хранит собственную копию и заполняет StartRequest.MCPServers при initial Dispatch, continuation Dispatch, explicit Resume и recovery Session для команды. Config default пустой; в server-owned WorkerEnvelope MCP list не добавлен. Credentials и endpoint не проходят через Secretary server, глобальные native config/auth не меняются.

Load/Save используют прежний строгий decoder и private file permissions. Duplicate servers/env, invalid names/commands/arguments/env и очевидные Secretary-only definitions отклоняются без вывода env values. Setter не меняет действующую конфигурацию при ошибке; runtime и вызывающий setter код получают независимые копии. Имя `secretary`, `SECRETARY_MCP_*` и `SECRETARY_CAPABILITY` запрещены. Это Node-owner-approved external tool, не Secretary lifecycle capability и не product-owned Child Worker MCP.

Worker default больше не упоминает отсутствующий server-owned child tool. Native harness skills и internal subagents остаются допустимыми средствами выполнения работы.

Публичные fixture checks: LoadDeploymentConfig, ExecutionNode.HandleCommand и native Runtime/Session boundary. Для Codex и CC проверены initial request, Follow-up после terminal Result, reopen local durable store и Resume, recovery Session input path, args/env и отсутствие config mutation. Негативные конфигурации проверены через public config loader.

- `MISE_TRUSTED_CONFIG_PATHS=/private/tmp/secretary-p5-worker-mcp go test ./internal/node ./cmd/secretary-node ./internal/config ./internal/ctl` — PASS после merge latest integration.
- `MISE_TRUSTED_CONFIG_PATHS=/private/tmp/secretary-p5-worker-mcp go test ./internal/node -run 'TestNodeDeployment.*WorkerMCP|TestExecutionNodeDeliversLocalMCP' -count=1 -race` — PASS.
- `git diff --check` — PASS.

Config schema и пример: [Node deployment](../../../../docs/node-deployment.md#mcp-для-workers-на-node). Передача definitions в fixture runtime не засчитывается как native tools/call. Для gate 05 live агент отдельно проверит `read_acceptance_marker` из собственного Node-local `/private/tmp/p5-mac-live/read-marker-mcp.py` на enrolled Nodes. Secretary MCP и Child Worker tools не используются как замена этой проверке.
