// Фейковый `codex app-server` для тестов: JSON-RPC по строкам.
// Текст с HANG при первом получении не получает ответа на turn/start (зависание до начала хода). Повтор HANG отвечает.
// LONG держит ход до turn/interrupt. Полученный ввод пишется в FAKE_LOG.
import { appendFileSync, existsSync, writeFileSync } from 'node:fs';
import { createInterface } from 'node:readline';

const log = (entry: object) => appendFileSync(process.env.FAKE_LOG!, JSON.stringify(entry) + '\n');
const hangMarker = `${process.env.FAKE_LOG}.hang`;
const send = (message: object) => process.stdout.write(JSON.stringify(message) + '\n');
const threadId = 'codex-thread';
let turns = 0;
let busy: string | null = null;

const rl = createInterface({ input: process.stdin });
rl.on('line', line => {
  const message = JSON.parse(line);
  if (message.id === undefined) return;
  const { id, method, params } = message;
  if (method === 'initialize') return send({ jsonrpc: '2.0', id, result: {} });
  if (method === 'thread/start' || method === 'thread/resume') return send({ jsonrpc: '2.0', id, result: { thread: { id: threadId } } });
  if (method === 'turn/interrupt') {
    send({ jsonrpc: '2.0', id, result: {} });
    if (busy) send({ jsonrpc: '2.0', method: 'turn/completed', params: { turn: { id: busy, status: 'interrupted' } } });
    busy = null;
    return;
  }
  if (method === 'turn/start') {
    const text: string = params.input[0].text;
    log({ harness: 'codex', received: text, thread: params.threadId });
    if (text.includes('HANG') && !existsSync(hangMarker)) { writeFileSync(hangMarker, ''); return; }
    const turnId = `turn-${++turns}`;
    send({ jsonrpc: '2.0', id, result: { turn: { id: turnId } } });
    send({ jsonrpc: '2.0', method: 'turn/started', params: { turn: { id: turnId } } });
    if (text.includes('LONG')) { busy = turnId; return; }
    send({ jsonrpc: '2.0', method: 'item/completed', params: { item: { type: 'agentMessage', text: `ANSWER ${text}` } } });
    send({ jsonrpc: '2.0', method: 'turn/completed', params: { turn: { id: turnId, status: 'completed' } } });
  }
});
rl.on('close', () => process.exit(0));
