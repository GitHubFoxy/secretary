import { startMcp } from "./mcp";
const t0 = Date.now();
const log = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`);
startMcp(8765, log);
const mcp = JSON.stringify({ mcpServers: { secretary: { type: "http", url: "http://127.0.0.1:8765/mcp" } } });
const SYS = "You are a secretary. Messages starting with [report ...] are background reports: reply with an empty message, write nothing.";

function start(model: string, resume?: string) {
  const args = ["claude", "--input-format", "stream-json", "--output-format", "stream-json", "--verbose",
    "--dangerously-skip-permissions", "--model", model, "--mcp-config", mcp, "--append-system-prompt", SYS];
  if (resume) args.push("--resume", resume);
  const p = Bun.spawn(args, { cwd: "/tmp/sspike", stdin: "pipe", stdout: "pipe", stderr: "inherit" });
  let sid = "", waiters: ((r: any) => void)[] = [];
  (async () => {
    let buf = "";
    for await (const chunk of p.stdout) {
      buf += new TextDecoder().decode(chunk);
      let i;
      while ((i = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, i); buf = buf.slice(i + 1);
        if (!line.trim()) continue;
        const e = JSON.parse(line);
        if (e.session_id) sid = e.session_id;
        if (e.type === "assistant") for (const c of e.message.content) {
          if (c.type === "text") log(`assistant text: ${JSON.stringify(c.text)}`);
          if (c.type === "tool_use") log(`assistant tool_use: ${c.name}`);
        }
        if (e.type === "result") { log(`result (${e.subtype}) model=${model}`); waiters.shift()?.(e); }
      }
    }
  })();
  const send = (text: string) => new Promise<any>((res) => {
    waiters.push(res);
    p.stdin.write(JSON.stringify({ type: "user", message: { role: "user", content: text } }) + "\n");
    p.stdin.flush();
  });
  return { p, send, sid: () => sid };
}

// 1. MCP delegate
let s = start("haiku");
await s.send("Use the delegate tool: harness codex, folder /tmp/x, task 'fix tests'. Then tell me the id in one short sentence.");
// 2. inject a message into the idle live session after a pause
await Bun.sleep(3000);
log("injecting report into idle live session");
await s.send("[report #7 codex /tmp/x] Tests fixed, 3 files changed.");
// 3. kill, then resume with another model and measure
const sid = s.sid(); s.p.kill(); await s.p.exited;
log(`killed; resuming ${sid} with sonnet`);
const tr = Date.now();
s = start("sonnet", sid);
await s.send("What task id did you get earlier? Answer with just the number.");
log(`resume+turn took ${((Date.now() - tr) / 1000).toFixed(1)}s`);
s.p.kill();
process.exit(0);
