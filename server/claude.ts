import { prepareMcp } from './external-mcp';
import { onAbort, readJsonLines, spawnHarness, type Turn } from './harness';

export async function claudeLaunch(options: import('./harness').TurnOptions) {
  const args = ['claude', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose', '--dangerously-skip-permissions', '--disallowedTools', 'WebSearch', '--model', options.model];
  if (options.reasoning) args.push('--effort', options.reasoning);
  if (options.resumeId) args.push('--resume', options.resumeId);
  const {env, servers} = await prepareMcp(options.mcpRoot, options.mcpRole);
  const configs: Record<string, object> = {};
  for (const {server, mapped} of servers) {
    const values = Object.fromEntries(Object.entries(mapped).map(([name, key]) => [name, `\${${key}}`]));
    configs[server.name] = server.type === 'stdio' ? { type: 'stdio', command: server.command, args: server.args, env: values } : { type: 'http', url: server.url, headers: values };
  }
  if (options.mcpUrl) configs.secretary = { type: 'http', url: options.mcpUrl, headers: { Authorization: `Bearer ${options.mcpToken}` } };
  if (Object.keys(configs).length) args.push('--mcp-config', JSON.stringify({ mcpServers: configs }));
  if (options.systemPrompt) args.push('--append-system-prompt', options.systemPrompt);
  return { args, env };
}

export const claudeTurn: Turn = async (options, text, images) => {
  if (options.interrupt.aborted) return false;
  const { args, env } = await claudeLaunch(options);
  const harness = spawnHarness(args, options.folder, options, env);
  const content = images.length
    ? [{ type: 'text', text }, ...images.map(image => ({ type: 'image', source: { type: 'base64', media_type: image.type, data: image.data } }))]
    : text;
  let accepted = false;
  let completed = false, finished = false;
  try {
    options.onDispatching();
    harness.write({ type: 'user', message: { role: 'user', content } });
    onAbort(options.interrupt, () => harness.write({ type: 'control_request', request_id: crypto.randomUUID(), request: { subtype: 'interrupt' } }));
    await readJsonLines(harness.child.stdout, event => {
      if (event.session_id) options.onSessionId(event.session_id);
      // Claude Code sends no receipt: its first event after the message means it has read it.
      if (!accepted) { accepted = true; options.onAccepted(); }
      options.onEvent(event);
      if (event.type === 'result') { completed = !event.is_error; finished = true; return false; }
    });
    if (!finished && !options.interrupt.aborted && !options.stop.aborted) throw await harness.failure('Claude Code');
  } finally {
    await harness.close();
  }
  return completed;
};
