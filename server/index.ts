import { authorized, pair } from './auth';
import { chatHistoryPage, chatMessage, reportText } from './chat';
import { mcpPort, secretaryFolder } from './config';
import { checkReasoning, modelDetails } from './models';
import { mkdirSync, existsSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { resolve } from 'node:path';
import { db, harnesses, isInternal, Session, type SessionRow } from './session';

const agentsFile = `${secretaryFolder}/AGENTS.md`;
mkdirSync(secretaryFolder, { recursive: true });
if (!existsSync(agentsFile)) writeFileSync(agentsFile, `# Secretary Agent

Ты правая рука владельца. Отвечай сам на быстрые вопросы. Долгую работу поручай через delegate, указывая name, harness, model, reasoning, folder и task. Следи за дочерними сессиями. На неважный [report ...] ответь ровно NO_REPLY. Если отчёт содержит важные для владельца сведения, сообщи о них.\n`);
if (!existsSync(`${secretaryFolder}/CLAUDE.md`)) writeFileSync(`${secretaryFolder}/CLAUDE.md`, '@AGENTS.md\n');

const sessions = new Map<number, Session>();
const publish = (message: object) => server.publish('events', JSON.stringify(message));
const listSessions = () => [...sessions.values()].map(s => { const info = s.info; return { ...info, internal: isInternal(info) }; });
const publishSessions = () => publish({ type: 'sessions', sessions: listSessions() });

function attach(session: Session) {
  sessions.set(session.id, session);
  session.onEvent(event => {
    const message = chatMessage(event);
    if (message) publish({ type: 'message', sessionId: session.id, message });
    else if (event.type === 'status') publishSessions();
    else if (event.type === 'queue' || event.type === 'interrupt_result') publish({ ...event, sessionId: session.id });
  });
  session.onComplete = answer => {
    const info = session.info;
    sessions.get(info.parent_id!)?.send(reportText(info, answer));
  };
  return session;
}
Session.recoverUnconfirmed();
const rows = db.query('SELECT * FROM sessions ORDER BY id').all() as SessionRow[];
if (!rows.length) attach(Session.create('opus', secretaryFolder));
else for (const row of rows) attach(Session.get(row.id)!);
db.query("UPDATE sessions SET status = 'interrupted' WHERE status = 'running'").run();

function create(harness: string, model: string, dir: string, parentId: number | null = null, name: string | null = null, reasoning: string | null = null) {
  if (!harnesses.includes(harness) || !model || !dir) throw new Error('Invalid session settings');
  const session = attach(Session.create(model, resolve(dir.replace(/^~(?=\/|$)/, homedir())), harness, parentId, name, reasoning));
  publishSessions();
  return session;
}
function session(id: number) { const value = sessions.get(Number(id)); if (!value) throw new Error('Session not found'); return value; }

const tools = [
  ['delegate', 'Delegate a task to a background session', { harness: { type: 'string', enum: harnesses }, model: { type: 'string' }, name: { type: 'string', minLength: 1 }, reasoning: { type: 'string', description: 'A reasoning level the selected model supports, e.g. low, medium, high' }, folder: { type: 'string' }, task: { type: 'string' } }, ['harness', 'model', 'name', 'reasoning', 'folder', 'task']],
  ['send', 'Send a message to a session', { id: { type: 'number' }, message: { type: 'string' } }, ['id', 'message']],
  ['list', 'List sessions', {}, []],
  ['read', 'Read session events', { id: { type: 'number' } }, ['id']],
  ['stop', 'Stop a session', { id: { type: 'number' } }, ['id']],
].map(([name, description, properties, required]) => ({ name, description, inputSchema: { type: 'object', properties, required } }));

async function call(name: string, args: any) {
  switch (name) {
    case 'delegate': {
      if (typeof args.name !== 'string' || !args.name.trim() || typeof args.task !== 'string' || !args.task.trim()) throw new Error('Invalid delegate settings');
      await checkReasoning(args.harness, args.model, args.reasoning);
      const child = create(args.harness, args.model, args.folder, 1, args.name.trim(), args.reasoning);
      child.send(args.task);
      return { id: child.id, status: 'running' };
    }
    case 'send': session(args.id).send(args.message); return { ok: true };
    case 'list': return listSessions();
    case 'read': return session(args.id).events().slice(-100);
    case 'stop': if (args.id === 1) throw new Error('Cannot stop secretary through MCP'); session(args.id).stop(); return { ok: true };
    default: throw new Error('Unknown tool');
  }
}

Bun.serve({ hostname: '127.0.0.1', port: mcpPort, async fetch(req) {
  if (new URL(req.url).pathname !== '/mcp' || req.method !== 'POST') return new Response(null, { status: 405 });
  const msg = await req.json();
  if (msg.id === undefined) return new Response(null, { status: 202 });
  const ok = (result: unknown) => Response.json({ jsonrpc: '2.0', id: msg.id, result });
  if (msg.method === 'initialize') return ok({ protocolVersion: msg.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: 'secretary', version: '0.1' } });
  if (msg.method === 'tools/list') return ok({ tools });
  if (msg.method === 'tools/call') { try { return ok({ content: [{ type: 'text', text: JSON.stringify(await call(msg.params.name, msg.params.arguments || {})) }] }); } catch (error) { return ok({ isError: true, content: [{ type: 'text', text: String(error) }] }); } }
  return Response.json({ jsonrpc: '2.0', id: msg.id, error: { code: -32601, message: 'Method not found' } });
} });

// Browsers send Origin; a foreign one is rejected. Requests without Origin (curl) pass.
function sameOrigin(request: Request, url: URL) {
  const origin = request.headers.get('origin');
  if (!origin) return true;
  try { return new URL(origin).host === url.host; } catch { return false; }
}

async function settings(cmd: any) {
  const s = session(cmd.id);
  if (!harnesses.includes(cmd.harness) || typeof cmd.model !== 'string' || !cmd.model) throw new Error('Invalid session settings');
  if (cmd.reasoning !== null) await checkReasoning(cmd.harness, cmd.model, cmd.reasoning);
  s.configure(cmd.harness, cmd.model, cmd.reasoning);
  publishSessions();
}

const commands: Record<string, (socket: any, cmd: any) => unknown> = {
  read(socket, cmd) { const s = session(cmd.id); socket.send(JSON.stringify({ type: 'history', id: s.id, ...chatHistoryPage(db, s.id, cmd.all === true), queue: s.queued(), requestId: cmd.requestId })); },
  send(socket, cmd) { session(cmd.id).send(cmd.text, cmd.images, cmd.immediate === true); socket.send(JSON.stringify({ type: 'sent', requestId: cmd.requestId })); },
  stop(_, cmd) { session(cmd.id).stop(); },
  interrupt(socket, cmd) { socket.send(JSON.stringify({ type: 'interrupt_ack', sessionId: cmd.id, requestId: cmd.requestId, accepted: session(cmd.id).interrupt(cmd.requestId) })); },
  rename(_, cmd) { session(cmd.id).setName(cmd.name); publishSessions(); },
  visibility(_, cmd) { session(cmd.id).setHidden(cmd.hidden); publishSessions(); },
  delete(_, cmd) { const s = session(cmd.id); s.delete(); sessions.delete(s.id); publishSessions(); },
  create(socket, cmd) { socket.send(JSON.stringify({ type: 'created', id: create(cmd.harness, cmd.model, cmd.folder).id })); },
  settings: (_, cmd) => settings(cmd),
};

const server = Bun.serve({
  port: 3000, hostname: '127.0.0.1',
  fetch(request, server) {
    const url = new URL(request.url);
    if (url.pathname === '/pair' && request.method === 'POST') {
      if (!sameOrigin(request, url)) return new Response('Forbidden', { status: 403 });
      return request.formData().then(form => {
        const token = pair(String(form.get('code') || ''));
        if (!token) return new Response('Неверный или просроченный код.', { status: 401 });
        return new Response(null, { status: 303, headers: { 'Location': '/', 'Set-Cookie': `secretary=${token}; HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=31536000`, 'Cache-Control': 'no-store' } });
      }).catch(() => new Response('Invalid request', { status: 400 }));
    }
    if (!authorized(request)) {
      if (url.pathname === '/' && request.method === 'GET') return new Response(Bun.file('login.html'), { headers: { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' } });
      return new Response('Unauthorized', { status: 401, headers: { 'Cache-Control': 'no-store' } });
    }
    if ((request.method !== 'GET' || url.pathname === '/ws') && !sameOrigin(request, url)) return new Response('Forbidden', { status: 403 });
    if (url.pathname === '/model-details')
      return Promise.resolve().then(() => modelDetails(url.searchParams.get('harness') || '', true)).then(result => Response.json(result), error => new Response(String(error), { status: 500 }));
    if (url.pathname === '/settings/agents') {
      if (request.method === 'GET') return Bun.file(agentsFile).text().then(text => new Response(text, { headers: { 'content-type': 'text/plain; charset=utf-8' } }), error => new Response(String(error), { status: 500 }));
      if (request.method === 'PUT') return request.text().then(text => Bun.write(agentsFile, text)).then(() => new Response(null, { status: 204 }), error => new Response(String(error), { status: 500 }));
      return new Response(null, { status: 405 });
    }
    if (url.pathname === '/ws') return server.upgrade(request) ? undefined : new Response('WebSocket upgrade failed', { status: 400 });
    const path = url.pathname === '/' ? '/index.html' : url.pathname;
    if (path.includes('..')) return new Response('Not found', { status: 404 });
    return new Response(Bun.file(`../web/dist${path}`), { headers: { 'Cache-Control': ['/index.html', '/build-version.json', '/sw.js'].includes(path) ? 'no-store' : 'no-cache' } });
  },
  websocket: {
    maxPayloadLength: 30 * 1024 * 1024,
    open(socket) { socket.subscribe('events'); socket.send(JSON.stringify({ type: 'sessions', sessions: listSessions() })); },
    async message(socket, message) {
      let cmd: any;
      try {
        cmd = JSON.parse(String(message));
        await commands[cmd.type]?.(socket, cmd);
      } catch (error) { socket.send(JSON.stringify({ type: 'error', message: String(error), requestId: cmd?.requestId })); }
    },
  },
});

// Messages confirmed before a restart and not yet started go now. This runs after the server exists: events are published through it.
for (const session of sessions.values()) session.resumePending();

console.log(`Secretary: ${server.url}`);
