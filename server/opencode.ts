import { createHash } from 'node:crypto';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { prepareMcp } from './external-mcp';
import { onAbort, readJsonLines, spawnHarness, type Turn, type TurnOptions } from './harness';

// OPENCODE_CONFIG_CONTENT merges over the owner's file config: the adapter forces allow-all permissions
// (agents run without confirmations, like Claude with --dangerously-skip-permissions) and passes the
// Secretary system prompt as an instructions file, because systemPrompt must not touch the project's
// folder. Owner keys already in the variable survive.
export async function opencodeLaunch(options: Pick<TurnOptions, 'systemPrompt' | 'mcpRoot' | 'mcpRole'>) {
  const args = ['opencode', 'acp'];
  const prepared = await prepareMcp(options.mcpRoot, options.mcpRole);
  const env = prepared.env;
  let base: Record<string, unknown> = {};
  try { base = JSON.parse(env.OPENCODE_CONFIG_CONTENT ?? 'null') ?? {}; } catch {}
  if (!base || typeof base !== 'object') base = {};
  let instructions = Array.isArray(base.instructions) ? base.instructions.filter((v: unknown): v is string => typeof v === 'string') : [];
  if (options.systemPrompt) {
    const file = join(tmpdir(), `secretary-opencode-${createHash('sha256').update(options.systemPrompt).digest('hex').slice(0, 16)}.md`);
    if (await Bun.file(file).text().catch(() => null) !== options.systemPrompt) await Bun.write(file, options.systemPrompt);
    if (!instructions.includes(file)) instructions = [...instructions, file];
  }
  env.OPENCODE_CONFIG_CONTENT = JSON.stringify({ ...base, permissions: [{ action: '*', resource: '*', effect: 'allow' }], ...(instructions.length ? { instructions } : {}) });
  return { args, env, prepared };
}

type McpServer = { name: string } & (
  | { type: 'http'; url: string; headers: { name: string; value: string }[] }
  | { command: string; args: string[]; env: { name: string; value: string }[] }
);

export function opencodeServers(options: Pick<TurnOptions, 'mcpUrl' | 'mcpToken'>, prepared: Awaited<ReturnType<typeof prepareMcp>>): McpServer[] {
  const servers: McpServer[] = [];
  if (options.mcpUrl) servers.push({ type: 'http', name: 'secretary', url: options.mcpUrl, headers: [{ name: 'Authorization', value: `Bearer ${options.mcpToken}` }] });
  for (const { server, mapped } of prepared.servers) {
    const values = Object.entries(mapped).map(([name, key]) => ({ name, value: prepared.env[key] ?? '' }));
    if (server.type === 'stdio') servers.push({ name: server.name, command: server.command, args: server.args, env: values });
    else servers.push({ type: 'http', name: server.name, url: server.url, headers: values });
  }
  return servers;
}

export const opencodeTurn: Turn = async (options, text, images) => {
  if (options.interrupt.aborted) return false;
  const { args, env, prepared } = await opencodeLaunch(options);
  const servers = opencodeServers(options, prepared);
  const harness = spawnHarness(args, options.folder, options, env);
  let id = 0, dispatched = false, accepted = false, answer = '', sessionId = '';
  let done!: (value: boolean) => void;
  const completed = new Promise<boolean>(resolve => { done = resolve; });
  const pending = new Map<number, { resolve: (value: any) => void; reject: (error: Error) => void }>();
  const request = (method: string, params: unknown): Promise<any> => new Promise((resolve, reject) => {
    pending.set(++id, { resolve, reject });
    harness.write({ jsonrpc: '2.0', id, method, params });
  });
  const reading = readJsonLines(harness.child.stdout, message => {
    if (message.id !== undefined && !message.method) {
      const waiter = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) waiter?.reject(new Error(JSON.stringify(message.error)));
      else waiter?.resolve(message.result);
    } else if (message.method === 'session/update') {
      // The first update after the prompt means OpenCode holds the message, like Claude's first event.
      if (dispatched && !accepted) { accepted = true; options.onAccepted(); }
      const chunk = message.params?.update?.sessionUpdate === 'agent_message_chunk' ? message.params.update.content?.text : null;
      if (typeof chunk === 'string') answer += chunk;
      options.onEvent(message);
    } else if (message.method) {
      options.onEvent(message);
    }
  }).finally(async () => {
    const error = await harness.failure('OpenCode');
    for (const waiter of pending.values()) waiter.reject(error);
    done(false);
  });
  try {
    await request('initialize', { protocolVersion: 1, clientCapabilities: { session: { compaction: true } } });
    const session = options.resumeId
      ? await request('session/resume', { sessionId: options.resumeId, cwd: options.folder, mcpServers: servers })
      : await request('session/new', { cwd: options.folder, mcpServers: servers });
    sessionId = options.resumeId ?? session.sessionId;
    options.onSessionId(sessionId);
    if (options.interrupt.aborted) return false;
    await request('session/set_config_option', { sessionId, configId: 'model', value: options.model });
    if (options.reasoning !== null) await request('session/set_config_option', { sessionId, configId: 'effort', value: options.reasoning });
    if (options.interrupt.aborted) return false;
    if (options.compact) {
      options.onDispatching();
      dispatched = true;
      onAbort(options.interrupt, () => harness.write({ jsonrpc: '2.0', method: 'session/cancel', params: { sessionId } }));
      const compact = await request('session/prompt', { sessionId, prompt: [{ type: 'text', text: '/compact' }] });
      if (!accepted) { accepted = true; options.onAccepted(); }
      options.onEvent({ type: 'opencode_result', text: answer, stopReason: compact.stopReason, usage: compact.usage });
      return compact.stopReason === 'end_turn';
    }
    dispatched = true;
    options.onDispatching();
    onAbort(options.interrupt, () => harness.write({ jsonrpc: '2.0', method: 'session/cancel', params: { sessionId } }));
    const prompt = await request('session/prompt', {
      sessionId,
      prompt: [{ type: 'text', text }, ...images.map(image => ({ type: 'image', data: image.data, mimeType: image.type }))],
    });
    // A turn with no updates still means the message reached the harness once it answers.
    if (!accepted) { accepted = true; options.onAccepted(); }
    options.onEvent({ type: 'opencode_result', text: answer, stopReason: prompt.stopReason, usage: prompt.usage });
    return prompt.stopReason === 'end_turn';
  } catch (error) {
    if (options.interrupt.aborted && !dispatched) return false;
    if (/auth_required/i.test(error instanceof Error ? error.message : String(error))) throw new Error('OpenCode требует входа. На Host выполните: opencode auth login');
    throw error;
  } finally {
    await harness.close();
    await reading;
  }
};
