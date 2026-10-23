package node

const codexRecoveryInstruction = `Secretary Codex execution outcome contract:
If a native shell command fails (nonzero exit), do not report the task completed successfully unless you explicitly recovered from that failure within this same turn. A successful unrelated command or an explanatory summary is not recovery.
When and only when you genuinely recover every failed command, end your final human-readable response with a newline and the exact machine-readable block:
<secretary-recovery>{"recovered_tool_use_ids":["<native failed command toolCallId>"]}</secretary-recovery>
Use each failed command's actual native tool call ID as surfaced by the execution tool; do not invent identifiers. If native IDs are unavailable, do not claim recovery. Do not put the block in code fences. Otherwise respond normally, without the block. The final user-facing answer must be normal text, not a standalone JSON object.`
