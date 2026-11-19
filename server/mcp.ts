import { mcpPort } from './config';
import { SECRETARY_ID } from './session';
import type { Sessions } from './sessions';

// The MCP adapter: tools the Secretary Agent uses to manage Specialists. Only the Secretary Agent gets this server,
// so delegation is one level deep and every call comes from the Secretary Agent.
export function serveMcp(sessions: Sessions) {
  const tools = [
    ['delegate', 'Delegate a task to a background session', { harness: { type: 'string', enum: sessions.harnesses }, model: { type: 'string' }, name: { type: 'string', minLength: 1 }, reasoning: { type: 'string', description: 'A reasoning level the selected model supports, e.g. low, medium, high' }, folder: { type: 'string' }, task: { type: 'string' } }, ['harness', 'model', 'name', 'reasoning', 'folder', 'task']],
    ['send', 'Send a message to a session', { id: { type: 'number' }, message: { type: 'string' } }, ['id', 'message']],
    ['list', 'List sessions', {}, []],
    ['read', 'Read the last 100 events of a session; images are replaced by their count', { id: { type: 'number' } }, ['id']],
    ['stop', 'Stop a session', { id: { type: 'number' } }, ['id']],
  ].map(([name, description, properties, required]) => ({ name, description, inputSchema: { type: 'object', properties, required } }));

  const notSecretary = (id: unknown) => {
    if (Number(id) === SECRETARY_ID) throw new Error('The Secretary Agent cannot do this to itself');
    return id;
  };

  async function call(name: string, args: any) {
    switch (name) {
      case 'delegate': {
        for (const field of ['name', 'task', 'reasoning']) if (typeof args[field] !== 'string' || !args[field].trim()) throw new Error(`Missing ${field}`);
        const child = await sessions.create({ harness: args.harness, model: args.model, reasoning: args.reasoning, folder: args.folder, parentId: SECRETARY_ID, name: args.name.trim() });
        child.send(args.task);
        return { id: child.id, status: 'running' };
      }
      case 'send': sessions.get(notSecretary(args.id)).send(args.message); return { ok: true };
      case 'list': return sessions.list();
      case 'read': return sessions.get(args.id).events().slice(-100).map(event => Array.isArray(event.images) ? { ...event, images: event.images.length } : event);
      case 'stop': sessions.get(notSecretary(args.id)).stop(); return { ok: true };
      default: throw new Error('Unknown tool');
    }
  }

  return Bun.serve({ hostname: '127.0.0.1', port: mcpPort, async fetch(req) {
    if (new URL(req.url).pathname !== '/mcp' || req.method !== 'POST') return new Response(null, { status: 405 });
    const msg = await req.json();
    if (msg.id === undefined) return new Response(null, { status: 202 });
    const ok = (result: unknown) => Response.json({ jsonrpc: '2.0', id: msg.id, result });
    if (msg.method === 'initialize') return ok({ protocolVersion: msg.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'secretary', version: '0.1' } });
    if (msg.method === 'tools/list') return ok({ tools });
    if (msg.method === 'tools/call') {
      try { return ok({ content: [{ type: 'text', text: JSON.stringify(await call(msg.params.name, msg.params.arguments || {})) }] }); }
      catch (error) { return ok({ isError: true, content: [{ type: 'text', text: String(error) }] }); }
    }
    return Response.json({ jsonrpc: '2.0', id: msg.id, error: { code: -32601, message: 'Method not found' } });
  } });
}
