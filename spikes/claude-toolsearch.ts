import { startMcp } from "./mcp";
const t0 = Date.now(); const log = (s: string) => console.log(`[${((Date.now()-t0)/1000).toFixed(1)}s] ${s}`);
startMcp(8767, log);
const mcp = JSON.stringify({ mcpServers: { secretary: { type: "http", url: "http://127.0.0.1:8767/mcp" } } });
const p = Bun.spawn(["claude","-p","Use the delegate tool: harness codex, task 'fix tests'. Reply with the id only.","--output-format","stream-json","--verbose","--model","haiku","--dangerously-skip-permissions","--mcp-config",mcp], { env: { ...process.env, ENABLE_TOOL_SEARCH: process.argv[2] }, stdout: "pipe" });
for (const line of (await new Response(p.stdout).text()).split("\n")) { if (!line) continue; const e = JSON.parse(line);
  if (e.type==="system"&&e.subtype==="init") log(`tools: ${e.tools.length}, has ToolSearch=${e.tools.includes("ToolSearch")}, has delegate=${e.tools.some((t:string)=>t.includes("delegate"))}`);
  if (e.type==="assistant") for (const c of e.message.content) if (c.type==="tool_use") log(`tool_use ${c.name}`);
  if (e.type==="result") log(`result: ${e.result}`); }
process.exit(0);
