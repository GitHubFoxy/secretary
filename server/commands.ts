import { readJsonLines } from './harness';

export type SlashCommand = { name: string; description: string; path?: string };

export function parseClaudeCommands(message: any): SlashCommand[] {
  if (message.response?.subtype !== 'success' || !Array.isArray(message.response.response?.commands)) throw new Error('Claude Code не вернул список команд');
  return message.response.response.commands.filter((item: any) => typeof item.name === 'string' && item.name !== 'compact')
    .map((item: any) => ({name: item.name, description: item.description || ''}));
}

export function parseCodexSkills(result: any): SlashCommand[] {
  if (!Array.isArray(result?.data)) throw new Error('Codex не вернул список skills');
  const commands: SlashCommand[] = [];
  for (const group of result.data) {
    if (group.errors?.length) throw new Error(group.errors.map((error: any) => error.message).join(', '));
    for (const skill of group.skills || []) if (skill.enabled && skill.name !== 'compact' && skill.path) commands.push({name: skill.name, description: skill.description || '', path: skill.path});
  }
  return commands;
}

export function parseOpencodeCommands(update: any): SlashCommand[] {
  if (!Array.isArray(update?.availableCommands)) throw new Error('OpenCode не вернул список команд');
  return update.availableCommands.filter((item: any) => typeof item.name === 'string' && item.name !== 'compact')
    .map((item: any) => ({ name: item.name, description: item.description || '' }));
}

async function loadOpencode(cwd: string): Promise<SlashCommand[]> {
  const p = Bun.spawn(['opencode', 'acp'], { cwd, stdin: 'pipe', stdout: 'pipe', stderr: 'ignore', env: { ...process.env }, signal: AbortSignal.timeout(15000) });
  const send = (message: object) => { p.stdin.write(JSON.stringify(message) + '\n'); p.stdin.flush(); };
  let id = 0;
  const pending = new Map<number, { resolve: (value: any) => void; reject: (error: Error) => void }>();
  const request = (method: string, params: object): Promise<any> => new Promise((resolve, reject) => { pending.set(++id, { resolve, reject }); send({ jsonrpc: '2.0', id, method, params }); });
  let found!: (commands: SlashCommand[]) => void, lost!: (error: Error) => void;
  const ready = new Promise<SlashCommand[]>((resolve, reject) => { found = resolve; lost = reject; });
  const reading = readJsonLines(p.stdout, message => {
    if (message.method === 'session/update' && message.params?.update?.sessionUpdate === 'available_commands_update') found(parseOpencodeCommands(message.params.update));
    if (message.id !== undefined && !message.method) {
      const waiter = pending.get(message.id);
      pending.delete(message.id);
      if (message.error) waiter?.reject(new Error(message.error.message));
      else waiter?.resolve(message.result);
    }
  });
  reading.finally(() => {
    const error = new Error('opencode не ответил на запрос команд');
    for (const waiter of pending.values()) waiter.reject(error);
    lost(error);
  }).catch(() => {});
  try {
    await request('initialize', { protocolVersion: 1, clientCapabilities: {} });
    const session = await request('session/new', { cwd, mcpServers: [] });
    const commands = await ready;
    // The listing session is empty: delete it so command lookups don't fill the project's OpenCode history.
    await request('session/delete', { sessionId: session.sessionId });
    return commands;
  } finally { p.kill(); }
}

async function load(harness: string, cwd: string): Promise<SlashCommand[]> {
  if (harness === 'opencode') return loadOpencode(cwd);
  if (!['claude', 'codex'].includes(harness)) throw new Error('Неизвестный harness');
  const p = Bun.spawn(harness === 'claude' ? ['claude', '-p', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose'] : ['codex', 'app-server'], {cwd, stdin: 'pipe', stdout: 'pipe', stderr: 'ignore', signal: AbortSignal.timeout(15000)});
  const send = (message: object) => { p.stdin.write(JSON.stringify(message) + '\n'); p.stdin.flush(); };
  if (harness === 'claude') send({type:'control_request', request_id:'commands', request:{subtype:'initialize'}});
  else {
    send({id:1, method:'initialize', params:{clientInfo:{name:'secretary', version:'0.1'}, capabilities:null}});
    send({method:'initialized'});
    send({id:2, method:'skills/list', params:{cwds:[cwd]}});
  }
  let commands: SlashCommand[] | undefined;
  try {
    await readJsonLines(p.stdout, message => {
      if (harness === 'claude' && message.type === 'control_response' && message.response?.request_id === 'commands') commands = parseClaudeCommands(message);
      if (harness === 'codex' && message.id === 2) {
        if (message.error) throw new Error(message.error.message);
        commands = parseCodexSkills(message.result);
      }
      if (commands) return false;
    });
  } finally { p.kill(); }
  if (!commands) throw new Error(`${harness} не ответил на запрос команд`);
  return commands;
}

const cache = new Map<string, {expires: number; value: Promise<SlashCommand[]>}>();
export function slashCommands(harness: string, cwd: string) {
  const key = JSON.stringify([harness, cwd]);
  const cached = cache.get(key);
  if (cached && cached.expires > Date.now()) return cached.value;
  const value = load(harness, cwd);
  cache.set(key, {expires: Date.now() + 30000, value});
  value.catch(() => { if (cache.get(key)?.value === value) cache.delete(key); });
  return value;
}
