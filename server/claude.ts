import { mcpUrl } from './config';
import { killHarness, readJsonLines, spawnHarness, type Turn } from './harness';

export const claudeTurn: Turn = async (options, text, images) => {
  const args = ['claude', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose', '--dangerously-skip-permissions', '--model', options.model];
  if (options.reasoning) args.push('--effort', options.reasoning);
  if (options.resumeId) args.push('--resume', options.resumeId);
  if (options.secretary) args.push('--mcp-config', JSON.stringify({ mcpServers: { secretary: { type: 'http', url: mcpUrl } } }));
  const child = spawnHarness(args, options.folder);
  options.onProcess(child);
  const write = (message: object) => { child.stdin.write(JSON.stringify(message) + '\n'); child.stdin.flush(); };
  const content = images.length
    ? [{ type: 'text', text }, ...images.map(image => ({ type: 'image', source: { type: 'base64', media_type: image.type, data: image.data } }))]
    : text;
  options.onDispatching();
  write({ type: 'user', message: { role: 'user', content } });
  options.onInterruptable(() => { try { write({ type: 'control_request', request_id: crypto.randomUUID(), request: { subtype: 'interrupt' } }); } catch {} });
  let completed = false;
  try {
    await readJsonLines(child.stdout, event => {
      if (event.session_id) options.onSessionId(event.session_id);
      // Claude Code sends no receipt: its first event after the message means it has read it.
      options.onAccepted();
      options.onEvent(event);
      if (event.type === 'result') { completed = !event.is_error; return false; }
    });
  } finally {
    killHarness(child);
    await child.exited;
  }
  return completed;
};
