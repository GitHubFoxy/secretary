import { onAbort, readJsonLines, spawnHarness, type Turn } from './harness';
import type { ThreadStartParams } from './protocol/v2/ThreadStartParams';
import type { TurnStartParams } from './protocol/v2/TurnStartParams';

export const codexTurn: Turn = async (options, text, images) => {
  if (options.interrupt.aborted) return false;
  const args = ['codex', 'app-server'];
  if (options.mcpUrl) args.push('-c', `mcp_servers.secretary.url="${options.mcpUrl}"`, '-c', `mcp_servers.secretary.http_headers.Authorization="Bearer ${options.mcpToken}"`);
  const harness = spawnHarness(args, options.folder, options);
  let id = 0;
  // `accepted` runs as soon as the response is read, before the notifications that follow it in the same chunk.
  const pending = new Map<number, { resolve: (value: any) => void; reject: (error: Error) => void; accepted?: () => void }>();
  let done!: (value: boolean) => void;
  const completed = new Promise<boolean>(resolve => { done = resolve; });
  const request = (method: string, params: unknown, accepted?: () => void): Promise<any> => new Promise((resolve, reject) => {
    pending.set(++id, { resolve, reject, accepted });
    harness.write({ jsonrpc: '2.0', id, method, params });
  });
  const reading = readJsonLines(harness.child.stdout, message => {
    if (message.id !== undefined && !message.method) {
      const waiter = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) waiter?.reject(new Error(JSON.stringify(message.error)));
      else { waiter?.accepted?.(); waiter?.resolve(message.result); }
    } else if (message.method) {
      options.onEvent(message);
      if (message.method === 'turn/completed') done(message.params.turn.status === 'completed');
    }
  }).finally(async () => {
    const error = await harness.failure('Codex');
    for (const waiter of pending.values()) waiter.reject(error);
    done(false);
  });
  try {
    await request('initialize', { clientInfo: { name: 'secretary', version: '0.1.0' }, capabilities: null });
    harness.write({ jsonrpc: '2.0', method: 'initialized' });
    const account = await request('account/read', { refreshToken: false });
    if (account.requiresOpenaiAuth && !account.account) throw new Error('Codex не вошёл в подписку. На Host выполните: codex login --device-auth');
    const agentsFile = Bun.file(`${options.folder}/AGENTS.md`);
    const preferences = options.secretary && await agentsFile.exists() ? await agentsFile.text() : undefined;
    const developerInstructions = [options.systemPrompt, preferences].filter(Boolean).join('\n\n') || undefined;
    const start: ThreadStartParams = { cwd: options.folder, approvalPolicy: 'never', sandbox: 'danger-full-access', developerInstructions };
    const thread = options.resumeId
      ? await request('thread/resume', { threadId: options.resumeId, excludeTurns: true, developerInstructions })
      : await request('thread/start', start);
    options.onSessionId(thread.thread.id);
    if (options.interrupt.aborted) return false;
    const turn: TurnStartParams = { threadId: thread.thread.id, model: options.model, effort: options.reasoning, input: [{ type: 'text', text, text_elements: [] }, ...images.map(image => ({ type: 'image' as const, url: `data:${image.type};base64,${image.data}` }))] };
    options.onDispatching();
    const started = await request('turn/start', turn, options.onAccepted);
    onAbort(options.interrupt, () => { request('turn/interrupt', { threadId: thread.thread.id, turnId: started.turn.id }).catch(() => {}); });
    return await completed;
  } finally {
    await harness.close();
    await reading;
  }
};

// Лимиты читает отдельный app-server: он не мешает ходам сессий и закрывается сразу после ответа.
export async function codexRateLimits() {
  const p = Bun.spawn(['codex', 'app-server'], { stdin: 'pipe', stdout: 'pipe', stderr: 'ignore', signal: AbortSignal.timeout(15_000) });
  const send = (message: object) => { p.stdin.write(JSON.stringify(message) + '\n'); p.stdin.flush(); };
  send({ id: 1, method: 'initialize', params: { clientInfo: { name: 'secretary', version: '0.1.0' }, capabilities: null } });
  send({ method: 'initialized' });
  send({ id: 2, method: 'account/rateLimits/read' });
  let limits: any;
  try {
    await readJsonLines(p.stdout, message => {
      if (message.id !== 2) return;
      if (message.error) throw new Error(message.error.message);
      limits = message.result;
      return false;
    });
  } finally { p.kill(); }
  if (!limits) throw new Error('Codex не ответил на запрос лимитов');
  return limits;
}
