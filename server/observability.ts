import { readFileSync } from 'node:fs';
import { claudeLaunch } from './claude';
import { codexLaunch, codexThreadStart } from './codex';
import { toolsFor } from './mcp';
import { ExternalMcp } from './external-mcp';
import { Memory } from './memory';
import { instructionsFile, SECRETARY_PROMPT, specialistPrompt } from './persona';
import { Secrets } from './secrets';
import { Skills } from './skills';
import { SECRETARY_ID } from './session';
import type { Sessions } from './sessions';
import type { TurnOptions } from './harness';

export async function agentView(root: string, sessions: Sessions, id?: string | null) {
  const external = new ExternalMcp(root);
  const servers = external.list();
  const secrets = new Secrets(root);
  let names: string[] = [];
  try { names = Object.keys(JSON.parse(readFileSync(`${root}/secrets.json`, 'utf8'))); } catch (error: any) { if (error.code !== 'ENOENT') throw error; }
  const sensitive = (await Promise.all(names.map(name => secrets.get(name)))).filter((v): v is string => !!v);
  for (const server of servers) sensitive.push(...Object.values(server.type === 'stdio' ? server.env : server.headers).filter(Boolean));
  for (const server of servers) if (server.type === 'http') for (const value of new URL(server.url).searchParams.values()) if (value) sensitive.push(value);
  const read = (path: string) => { try { return readFileSync(path, 'utf8'); } catch (error: any) { if (error.code === 'ENOENT') return ''; throw error; } };
  const memory = new Memory(root), skills = new Skills(root);
  const result: any = {
    roles: {
      secretary: { prompt: SECRETARY_PROMPT, instructions: read(instructionsFile(root, 'secretary')), tools: toolsFor(true, sessions.harnesses) },
      specialist: { prompt: specialistPrompt(root), tools: toolsFor(false, sessions.harnesses) },
      worker: { prompt: '', tools: [] },
    },
    memory: memory.list().map(name => ({ name, text: memory.read(name).text })),
    skills: skills.list().map(name => ({ name, text: skills.read(name) })),
    servers: servers.map(s => s.type === 'stdio' ? { ...s, env: Object.fromEntries(Object.keys(s.env).map(k => [k, '[скрыто]'])) } : { ...s, url: s.url.split('?')[0], headers: Object.fromEntries(Object.keys(s.headers).map(k => [k, '[скрыто]'])) }),
    sessions: sessions.list().map(({ id, name, harness, folder }) => ({ id, name, harness, folder })),
  };
  if (id) {
    const session = sessions.get(id), info = session.info, context = session.context;
    if (context.mcpToken) sensitive.push(context.mcpToken);
    const signal = new AbortController().signal;
    const options: TurnOptions = { ...context, folder: info.folder, model: info.model, reasoning: info.reasoning, resumeId: info.harness_session_id, secretary: info.id === SECRETARY_ID, interrupt: signal, stop: signal, onSessionId() {}, onEvent() {}, onDispatching() {}, onAccepted() {} };
    const launch = info.harness === 'claude' ? await claudeLaunch(options) : await codexLaunch(options);
    for (const [key, value] of Object.entries(launch.env)) if (key.startsWith('SECRETARY_MCP_') && value) sensitive.push(value);
    result.session = { harness: info.harness, args: launch.args, ...(info.harness === 'codex' ? { threadStart: await codexThreadStart(options) } : {}), systemPrompt: info.harness === 'codex' ? (await codexThreadStart(options)).developerInstructions : options.systemPrompt ?? '' };
  }
  const redact = (value: any): any => {
    if (typeof value === 'string') {
      for (const secret of sensitive.sort((a, b) => b.length - a.length)) {
        value = value.replaceAll(secret, '[скрыто]').replaceAll(JSON.stringify(secret).slice(1, -1), '[скрыто]');
      }
      return value;
    }
    if (Array.isArray(value)) return value.map(redact);
    if (value && typeof value === 'object') return Object.fromEntries(Object.entries(value).map(([key, item]) => [key, redact(item)]));
    return value;
  };
  return redact(result);
}
