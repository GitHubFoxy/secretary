import { prepareMcp } from './external-mcp';
import { slashCommands } from './commands';
import { onAbort, readJsonLines, spawnHarness, type Turn } from './harness';
import type { ThreadStartParams } from './protocol/v2/ThreadStartParams';
import type { TurnStartParams } from './protocol/v2/TurnStartParams';

export async function codexThreadStart(options: Pick<import('./harness').TurnOptions, 'folder' | 'secretary' | 'systemPrompt' | 'model'>): Promise<ThreadStartParams> {
  const agentsFile = Bun.file(`${options.folder}/AGENTS.md`);
  const preferences = options.secretary && await agentsFile.exists() ? await agentsFile.text() : undefined;
  const developerInstructions = [options.systemPrompt, preferences].filter(Boolean).join('\n\n') || undefined;
  const start: ThreadStartParams = { model: options.model, cwd: options.folder, approvalPolicy: 'never', sandbox: 'danger-full-access', developerInstructions };
  return start;
}

export async function codexLaunch(options: import('./harness').TurnOptions) {
  const args = ['codex', 'app-server'];
  args.push('-c', 'web_search="disabled"');
  if (options.mcpUrl) args.push('-c', `mcp_servers.secretary.url="${options.mcpUrl}"`, '-c', `mcp_servers.secretary.http_headers.Authorization="Bearer ${options.mcpToken}"`);
  const {env, servers} = await prepareMcp(options.mcpRoot, options.mcpRole);
  for (const {server, mapped} of servers) {
    const prefix = `mcp_servers.${JSON.stringify(server.name)}`;
    if (server.type === 'stdio') {
      args.push('-c', `${prefix}.command=${JSON.stringify(process.execPath)}`, '-c', `${prefix}.args=${JSON.stringify([`${import.meta.dir}/mcp-exec.ts`, server.command, JSON.stringify(server.args), JSON.stringify(mapped)])}`, '-c', `${prefix}.env_vars=${JSON.stringify(Object.values(mapped))}`);
    } else {
      args.push('-c', `${prefix}.url=${JSON.stringify(server.url)}`);
      for (const [name, key] of Object.entries(mapped)) args.push('-c', `${prefix}.env_http_headers.${JSON.stringify(name)}=${JSON.stringify(key)}`);
    }
  }
  return { args, env };
}

export const codexTurn: Turn = async (options, text, images) => {
  if (options.interrupt.aborted) return false;
  const { args, env } = await codexLaunch(options);
  const harness = spawnHarness(args, options.folder, options, env);
  let id = 0, dispatched = false;
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
    const start = await codexThreadStart(options);
    const { developerInstructions } = start;
    const thread = options.resumeId
      ? await request('thread/resume', { threadId: options.resumeId, model: options.model, excludeTurns: true, developerInstructions })
      : await request('thread/start', start);
    options.onSessionId(thread.thread.id);
    if (options.interrupt.aborted) return false;
    if (options.compact) {
      options.onDispatching();
      await request('thread/compact/start', { threadId: thread.thread.id }, options.onAccepted);
      return await completed;
    }
    const match = text.match(/^\/([^\s]+)(?=\s|$)/);
    const skills = match ? (await slashCommands('codex', options.folder)).filter(skill => skill.name === match[1]) : [];
    const skill = skills.length === 1 ? skills[0] : undefined;
    if (options.interrupt.aborted) return false;
    const turn: TurnStartParams = { threadId: thread.thread.id, model: options.model, effort: options.reasoning, input: [{ type: 'text', text, text_elements: [] }, ...(skill?.path ? [{type: 'skill' as const, name: skill.name, path: skill.path}] : []), ...images.map(image => ({ type: 'image' as const, url: `data:${image.type};base64,${image.data}` }))] };
    dispatched = true;
    options.onDispatching();
    const started = await request('turn/start', turn, options.onAccepted);
    onAbort(options.interrupt, () => { request('turn/interrupt', { threadId: thread.thread.id, turnId: started.turn.id }).catch(() => {}); });
    return await completed;
  } catch (error) {
    if (options.interrupt.aborted && !dispatched) return false;
    throw error;
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
