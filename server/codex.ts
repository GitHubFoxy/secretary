import { mcpUrl } from './config';
import { killHarness, readJsonLines, spawnHarness, type Turn } from './harness';
import type { ThreadStartParams } from './protocol/v2/ThreadStartParams';
import type { TurnStartParams } from './protocol/v2/TurnStartParams';

export const codexTurn: Turn = async (options, text, images) => {
  const args = ['codex', 'app-server'];
  if (options.secretary) args.push('-c', `mcp_servers.secretary.url="${mcpUrl}"`);
  const child = spawnHarness(args, options.folder);
  options.onProcess(child);
  let id = 0;
  const pending = new Map<number, { resolve: (value: any) => void; reject: (error: Error) => void }>();
  let done!: (value: boolean) => void;
  const completed = new Promise<boolean>(resolve => { done = resolve; });
  const send = (message: object) => { child.stdin.write(JSON.stringify(message) + '\n'); child.stdin.flush(); };
  const request = (method: string, params: unknown): Promise<any> => new Promise((resolve, reject) => {
    pending.set(++id, { resolve, reject });
    send({ jsonrpc: '2.0', id, method, params });
  });
  const reading = readJsonLines(child.stdout, message => {
    if (message.id !== undefined && !message.method) {
      const waiter = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) waiter?.reject(new Error(JSON.stringify(message.error)));
      else waiter?.resolve(message.result);
    } else if (message.method) {
      options.onEvent(message);
      if (message.method === 'turn/completed') done(message.params.turn.status === 'completed');
    }
  }).finally(() => {
    for (const waiter of pending.values()) waiter.reject(new Error('Codex process exited'));
    done(false);
  });
  try {
    await request('initialize', { clientInfo: { name: 'secretary', version: '0.1.0' }, capabilities: null });
    send({ jsonrpc: '2.0', method: 'initialized' });
    const start: ThreadStartParams = { cwd: options.folder, approvalPolicy: 'never', sandbox: 'danger-full-access', developerInstructions: options.secretary ? await Bun.file(`${options.folder}/AGENTS.md`).text() : undefined };
    const thread = options.resumeId
      ? await request('thread/resume', { threadId: options.resumeId, excludeTurns: true })
      : await request('thread/start', start);
    options.onSessionId(thread.thread.id);
    if (options.shouldCancel()) return false;
    const turn: TurnStartParams = { threadId: thread.thread.id, model: options.model, effort: options.reasoning, input: [{ type: 'text', text, text_elements: [] }, ...images.map(image => ({ type: 'image' as const, url: `data:${image.type};base64,${image.data}` }))] };
    options.onDispatching();
    const started = await request('turn/start', turn);
    options.onAccepted();
    options.onInterruptable(() => { request('turn/interrupt', { threadId: thread.thread.id, turnId: started.turn.id }).catch(() => {}); });
    return await completed;
  } finally {
    killHarness(child);
    await reading;
    await child.exited;
  }
};
