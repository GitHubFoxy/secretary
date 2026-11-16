// Проба: что делает Claude Code в stream-json, если во время хода прислать сообщение и control-запрос interrupt.
// Запуск: bun run spikes/steer-claude.ts <mid|interrupt>
const mode = process.argv[2] ?? 'mid';
const t0 = Date.now();
const log = (s: string) => console.log(`[${((Date.now() - t0) / 1000).toFixed(1)}s] ${s}`);

const dir = '/tmp/steer-claude';
await Bun.$`mkdir -p ${dir}`;

const p = Bun.spawn(['claude', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose',
  '--dangerously-skip-permissions', '--model', 'haiku'], { cwd: dir, stdin: 'pipe', stdout: 'pipe', stderr: 'inherit' });

let buf = '';
const results: any[] = [];
(async () => {
  for await (const chunk of p.stdout) {
    buf += new TextDecoder().decode(chunk, { stream: true });
    let i: number;
    while ((i = buf.indexOf('\n')) >= 0) {
      const line = buf.slice(0, i); buf = buf.slice(i + 1);
      if (!line.trim()) continue;
      const e = JSON.parse(line);
      if (e.type === 'assistant') for (const c of e.message.content) {
        if (c.type === 'text') log(`assistant text: ${JSON.stringify(c.text).slice(0, 200)}`);
        if (c.type === 'tool_use') log(`tool_use: ${c.name} ${JSON.stringify(c.input).slice(0, 120)}`);
      } else if (e.type === 'user') {
        const c = e.message.content;
        const text = typeof c === 'string' ? c : JSON.stringify(c).slice(0, 160);
        log(`event user: ${text.slice(0, 160)}`);
      } else if (e.type === 'result') {
        log(`RESULT subtype=${e.subtype} is_error=${e.is_error} result=${JSON.stringify(e.result ?? '').slice(0, 160)}`);
        results.push(e);
      } else if (e.type === 'control_response') {
        log(`control_response: ${JSON.stringify(e).slice(0, 300)}`);
      } else {
        log(`event ${e.type}${e.subtype ? '/' + e.subtype : ''}`);
      }
    }
  }
  log('stdout closed');
})();

const write = (obj: unknown) => { p.stdin.write(JSON.stringify(obj) + '\n'); p.stdin.flush(); };
const user = (text: string) => write({ type: 'user', message: { role: 'user', content: text } });

user('Выполни bash-команду `sleep 12 && echo first-done`, затем ответь одним словом: DONE_A.');
await Bun.sleep(4000);
if (mode === 'mid') {
  log('>>> sending message B mid-turn');
  user('Новое указание: вместо этого ответь одним словом BETA_MARKER и больше ничего не делай.');
} else {
  log('>>> sending control_request interrupt');
  write({ type: 'control_request', request_id: 'int-1', request: { subtype: 'interrupt' } });
}
await Bun.sleep(25000);
if (mode === 'interrupt') {
  log('>>> after interrupt: sending message C');
  user('Ответь одним словом: ALIVE.');
  await Bun.sleep(15000);
}
log(`results so far: ${results.length}`);
p.kill();
await p.exited;
process.exit(0);
