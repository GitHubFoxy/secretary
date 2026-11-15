import { startMcp } from "./mcp";
const t0 = Date.now();
const log = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`);
startMcp(8766, log);
const DEV = "You are a secretary. Messages starting with [report ...] are background reports: reply with an empty message, write nothing.";

function start() {
  const p = Bun.spawn(["codex", "app-server",
    "-c", 'mcp_servers.secretary.url="http://127.0.0.1:8766/mcp"'],
    { cwd: "/tmp/sspike", stdin: "pipe", stdout: "pipe", stderr: "ignore" });
  let id = 0; const pending = new Map<number, (r: any) => void>(); let turnDone: ((e: any) => void) | null = null;
  const seen = new Set<string>();
  (async () => {
    let buf = "";
    for await (const chunk of p.stdout) {
      buf += new TextDecoder().decode(chunk);
      let i;
      while ((i = buf.indexOf("\n")) >= 0) {
        const line = buf.slice(0, i); buf = buf.slice(i + 1);
        if (!line.trim()) continue;
        const m = JSON.parse(line);
        if (m.id !== undefined && pending.has(m.id)) { if (m.error) log(`ERROR ${JSON.stringify(m.error)}`); pending.get(m.id)!(m.result); pending.delete(m.id); continue; }
        if (m.id !== undefined && m.method) { log(`SERVER REQUEST ${m.method}`); continue; } // approvals etc.
        if (["error","warning","deprecationNotice"].includes(m.method)) log(`${m.method}: ${JSON.stringify(m.params).slice(0,300)}`);
        if (m.method && !m.method.includes("delta") && !seen.has(m.method)) { seen.add(m.method); log(`notif ${m.method}`); }
        if (m.method === "item/completed") {
          const it = m.params.item;
          if (it.type === "agentMessage") log(`agent text: ${JSON.stringify(it.text)}`);
          if (it.type === "mcpToolCall") log(`mcp tool call: ${it.tool} status=${it.status}`);
        }
        if (m.method === "turn/completed") { log(`turn/completed status=${m.params.turn?.status}`); turnDone?.(m); }
      }
    }
  })();
  const req = (method: string, params: any) => new Promise<any>((res) => {
    const i = ++id; pending.set(i, res);
    p.stdin.write(JSON.stringify({ jsonrpc: "2.0", id: i, method, params }) + "\n"); p.stdin.flush();
  });
  const turn = async (threadId: string, text: string, model?: string) => {
    const done = new Promise((r) => (turnDone = r));
    await req("turn/start", { threadId, model, input: [{ type: "text", text, text_elements: [] }] });
    await done;
  };
  return { p, req, turn };
}

let c = start();
await c.req("initialize", { clientInfo: { name: "secretary-spike", title: null, version: "0" }, capabilities: null });
p_notify(c);
const th = await c.req("thread/start", { cwd: "/tmp/sspike", approvalPolicy: "never", sandbox: "danger-full-access", developerInstructions: DEV, ephemeral: false });
const threadId = th.thread.id; log(`thread ${threadId} model=${th.model}`);
await c.turn(threadId, "Use the delegate tool: harness claude, folder /tmp/x, task 'fix tests'. Then tell me the id in one short sentence.");
await Bun.sleep(3000);
log("injecting report");
await c.turn(threadId, "[report #7 claude /tmp/x] Tests fixed, 3 files changed.");
log("model switch within thread");
await c.turn(threadId, "Say 'ok' only.", "gpt-5.6-luna");
c.p.kill(); await c.p.exited;
log("killed; resuming");
const tr = Date.now();
c = start();
await c.req("initialize", { clientInfo: { name: "secretary-spike", title: null, version: "0" }, capabilities: null });
p_notify(c);
await c.req("thread/resume", { threadId });
await c.turn(threadId, "What task id did you get earlier? Answer with just the number.");
log(`resume+turn took ${((Date.now() - tr) / 1000).toFixed(1)}s`);
c.p.kill(); process.exit(0);

function p_notify(c: any) { c.p.stdin.write(JSON.stringify({ jsonrpc: "2.0", method: "initialized" }) + "\n"); }
