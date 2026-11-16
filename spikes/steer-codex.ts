// Проба: turn/steer и turn/interrupt в codex app-server 0.169.
// Запуск: bun run spikes/steer-codex.ts <steer|interrupt>
const mode = process.argv[2] ?? 'steer';
const t0 = Date.now();
const log = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`);

const dir = '/tmp/steer-codex';
await Bun.$`mkdir -p ${dir}`;

const p = Bun.spawn(['codex', 'app-server'], { cwd: dir, stdin: 'pipe', stdout: 'pipe', stderr: 'ignore' });
let id = 0;
const pending = new Map<number, (r: any) => void>();
let turnDone: ((m: any) => void) | null = null;
let buf = '';
(async () => {
  for await (const chunk of p.stdout) {
    buf += new TextDecoder().decode(chunk, { stream: true });
    let i: number;
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i); buf = buf.slice(i + 1);
      if (!line.trim()) continue;
      const m = JSON.parse(line);
      if (m.id !== undefined && !m.method) {
        if (m.error) log(`ERROR response ${m.id}: ${JSON.stringify(m.error).slice(0, 300)}`);
        pending.get(m.id)?.(m.result ?? m.error);
        pending.delete(m.id);
        continue;
      }
      if (m.method === 'item/completed' && m.params.item.type === 'agentMessage') log(`agent text: ${JSON.stringify(m.params.item.text).slice(0, 200)}`);
      if (m.method === 'item/started' && m.params.item.type === 'commandExecution') log(`command started: ${m.params.item.command}`);
      if (m.method === 'turn/started') log(`turn/started ${m.params.turn.id}`);
      if (m.method === 'turn/completed') { log(`turn/completed status=${m.params.turn.status} id=${m.params.turn.id}`); turnDone?.(m); }
      if (m.method === 'error') log(`error notification: ${JSON.stringify(m.params).slice(0, 300)}`);
    }
  }
})();

const send = (message: object) => { p.stdin.write(JSON.stringify(message) + '\n'); p.stdin.flush(); };
const req = (method: string, params: unknown) => new Promise<any>(resolve => {
  const i = ++id; pending.set(i, resolve); send({ jsonrpc: '2.0', id: i, method, params });
});

await req('initialize', { clientInfo: { name: 'secretary-steer-spike', version: '0.1' }, capabilities: null });
send({ jsonrpc: '2.0', method: 'initialized' });
const th = await req('thread/start', { cwd: dir, approvalPolicy: 'never', sandbox: 'danger-full-access' });
const threadId = th.thread.id;
log(`thread ${threadId}`);

const text = (s: string) => [{ type: 'text', text: s, text_elements: [] }];
const turn = await req('turn/start', { threadId, model: 'gpt-5.6-luna', input: text('Выполни команду `sleep 12 && echo first-done`, затем ответь одним словом: DONE_A.') });
const turnId = turn.turn?.id;
log(`turn/start -> turn ${turnId}`);
await Bun.sleep(4000);

if (mode === 'steer') {
  log('>>> turn/steer');
  const r = await req('turn/steer', { threadId, expectedTurnId: turnId, input: text('Новое указание: вместо этого ответь одним словом BETA_MARKER и больше ничего не делай.') });
  log(`steer response: ${JSON.stringify(r).slice(0, 300)}`);
  await new Promise<void>(res => { turnDone = () => res(); setTimeout(res, 40000); });
} else {
  log('>>> turn/interrupt');
  const done = new Promise<void>(res => { turnDone = () => res(); setTimeout(res, 40000); });
  const r = await req('turn/interrupt', { threadId, turnId });
  log(`interrupt response: ${JSON.stringify(r).slice(0, 300)}`);
  await done;
  log('>>> after interrupt: turn/start with message C');
  const done2 = new Promise<void>(res => { turnDone = () => res(); setTimeout(res, 40000); });
  await req('turn/start', { threadId, model: 'gpt-5.6-luna', input: text('Ответь одним словом: ALIVE.') });
  await done2;
}
p.kill();
await p.exited;
process.exit(0);
