// A fake `codex app-server` for adapter tests: JSON-RPC over lines, logs each turn/start to calls.log in its cwd.
// LONG holds the turn until turn/interrupt; HANG never answers turn/start; a cwd containing "slow-thread" delays thread/start.
import { appendFileSync } from 'node:fs';
import { createInterface } from 'node:readline';

const send = (message: object) => process.stdout.write(JSON.stringify(message) + '\n');
const thread = { id: 'codex-thread' };
let busy = '';
let prompt: string | undefined;
let model: string | undefined;

createInterface({ input: process.stdin }).on('line', line => {
  const { id, method, params } = JSON.parse(line);
  if (id === undefined) return;
  if (method === 'initialize') return send({ id, result: {} });
  if (method === 'account/read') return send({ id, result: { account: process.cwd().includes('no-auth') ? null : { type: 'chatgpt' }, requiresOpenaiAuth: true } });
  if (method === 'thread/start' || method === 'thread/resume') {
    prompt = params.developerInstructions;
    model = params.model;
    return setTimeout(() => send({ id, result: { thread } }), process.cwd().includes('slow-thread') ? 300 : 0);
  }
  if (method === 'turn/interrupt') {
    send({ id, result: {} });
    if (busy) send({ method: 'turn/completed', params: { turn: { id: busy, status: 'interrupted' } } });
    busy = '';
    return;
  }
  if (method === 'turn/start') {
    const text: string = params.input[0].text;
    appendFileSync('calls.log', JSON.stringify({ text, thread: params.threadId, prompt, model }) + '\n');
    if (text.includes('HANG')) return;
    const turn = { id: 'turn-1' };
    send({ id, result: { turn } });
    if (text.includes('LONG')) { busy = turn.id; return; }
    send({ method: 'item/completed', params: { item: { type: 'agentMessage', text: `ANSWER ${text}` } } });
    send({ method: 'turn/completed', params: { turn: { ...turn, status: 'completed' } } });
  }
}).on('close', () => process.exit(0));
