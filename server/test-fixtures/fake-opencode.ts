// A fake `opencode acp` for adapter tests: JSON-RPC over lines, logs each prompt to calls.log in its cwd.
// LONG holds the prompt until session/cancel; HANG never answers session/prompt; CRASH exits with an error on stderr.
// A cwd containing "slow-resume" delays session/resume; a cwd containing "no-auth" fails session/prompt with auth_required.
import { appendFileSync } from 'node:fs';
import { createInterface } from 'node:readline';

const send = (message: object) => process.stdout.write(JSON.stringify(message) + '\n');

const modelOptions = ['pag/beam-free', 'pag/hungerg-free', 'pag/deepseek-v4-pro'].map(value => ({ value, name: value }));
const effortFor = (model: string) => model === 'pag/deepseek-v4-pro' ? ['high', 'max'] : ['low', 'medium', 'high', 'xhigh', 'max'];
let model = 'pag/beam-free';
const configOptions = () => [
  { id: 'model', name: 'Model', category: 'model', type: 'select', currentValue: model, options: modelOptions },
  { id: 'effort', name: 'Effort', category: 'thought_level', type: 'select', currentValue: 'default', options: [...effortFor(model).map(value => ({ value, name: value })), { value: 'default', name: 'Default' }] },
  { id: 'mode', name: 'Session Mode', category: 'mode', type: 'select', currentValue: 'build', options: [{ value: 'build', name: 'Build' }] },
];

let pending: { id: number; sessionId: string; text: string } | null = null;

createInterface({ input: process.stdin }).on('line', line => {
  const message = JSON.parse(line);
  if (message.id === undefined) {
    if (message.method === 'session/cancel' && pending) {
      const { id, sessionId } = pending;
      pending = null;
      send({ jsonrpc: '2.0', id, result: { stopReason: 'cancelled', _meta: {} } });
      void sessionId;
    }
    return;
  }
  const { id, method, params } = message;
  if (method === 'initialize') return send({ id, result: { protocolVersion: 1, agentCapabilities: {}, agentInfo: { name: 'OpenCode', version: '0.0.0' } } });
  if (method === 'session/new') {
    appendFileSync('calls.log', JSON.stringify({ setup: 'new', sessionId: 'opencode-session', config: process.env.OPENCODE_CONFIG_CONTENT ?? null }) + '\n');
    const slow = process.cwd().includes('slow-resume');
    setTimeout(() => {
      send({ id, result: { sessionId: 'opencode-session', configOptions: configOptions() } });
      send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: 'opencode-session', update: { sessionUpdate: 'available_commands_update', availableCommands: [{ name: 'compact', description: 'Compact the session' }, { name: 'review', description: 'review changes' }] } } });
    }, slow ? 300 : 0);
    return;
  }
  if (method === 'session/resume') {
    appendFileSync('calls.log', JSON.stringify({ setup: 'resume', sessionId: params.sessionId, config: process.env.OPENCODE_CONFIG_CONTENT ?? null }) + '\n');
    setTimeout(() => send({ id, result: { configOptions: configOptions() } }), process.cwd().includes('slow-resume') ? 300 : 0);
    return;
  }
  if (method === 'session/set_config_option') {
    if (params.configId === 'model') model = params.value;
    return send({ id, result: { configOptions: configOptions() } });
  }
  if (method === 'session/delete') {
    appendFileSync('calls.log', JSON.stringify({ delete: params.sessionId }) + '\n');
    return send({ id, result: {} });
  }
  if (method === 'session/prompt') {
    const text: string = params.prompt[0].text;
    appendFileSync('calls.log', JSON.stringify({ text, session: params.sessionId }) + '\n');
    if (text.includes('CRASH')) { process.stderr.write('connection refused\n'); process.exit(1); }
    if (process.cwd().includes('no-auth')) return send({ id, error: { code: -32000, message: 'auth_required' } });
    if (text.includes('HANG')) return;
    if (text === '/compact') {
      if (!process.cwd().includes('quiet-compact')) send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: params.sessionId, update: { sessionUpdate: 'compaction_update', status: 'completed' } } });
      return send({ id, result: { stopReason: 'end_turn', usage: {}, _meta: {} } });
    }
    if (text.includes('LONG')) {
      send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: params.sessionId, update: { sessionUpdate: 'agent_thought_chunk', content: { type: 'text', text: 'думаю' } } } });
      pending = { id, sessionId: params.sessionId };
      return;
    }
    send({ jsonrpc: '2.0', method: 'session/update', params: { sessionId: params.sessionId, update: { sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: `ANSWER ${text}` } } } });
    send({ id, result: { stopReason: 'end_turn', usage: { inputTokens: 1, outputTokens: 1 }, _meta: {} } });
  }
}).on('close', () => process.exit(0));
