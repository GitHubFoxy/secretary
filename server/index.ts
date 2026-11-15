import { models, modelDetails, claudeModels } from './models';
import { mkdirSync, existsSync, writeFileSync } from 'node:fs';
import { homedir } from 'node:os';
import { resolve } from 'node:path';
import { db, Session, type SessionRow } from './session';

const folder = `${homedir()}/.secretary`;
mkdirSync(folder, { recursive: true });
if (!existsSync(`${folder}/AGENTS.md`)) writeFileSync(`${folder}/AGENTS.md`, `# Secretary Agent

Ты правая рука владельца. Отвечай сам на быстрые вопросы. Долгую работу поручай через delegate, указывая name, harness, model, reasoning, folder и task. Следи за дочерними сессиями. На неважный [report ...] ответь ровно NO_REPLY. Если отчёт содержит важные для владельца сведения, сообщи о них.\n`);
if (!existsSync(`${folder}/CLAUDE.md`)) writeFileSync(`${folder}/CLAUDE.md`, '@AGENTS.md\n');

const sessions = new Map<number, Session>();
function attach(session: Session) {
  sessions.set(session.id, session);
  session.onEvent(event => server.publish('events', JSON.stringify({ type: 'event', sessionId: session.id, event })));
  session.onComplete = answer => {
    const info = session.info;
    sessions.get(info.parent_id!)?.send(`[report #${info.id} ${info.harness} ${info.folder}] ${answer}`);
  };
  return session;
}
const rows = db.query('SELECT * FROM sessions ORDER BY id').all() as SessionRow[];
if (!rows.length) attach(Session.create('opus', folder));
else for (const row of rows) attach(Session.get(row.id)!);
Session.cleanInterrupted();
const interrupted = rows.filter(row => row.status === 'running');
db.query("UPDATE sessions SET status = 'interrupted' WHERE status = 'running'").run();

function create(harness: string, model: string, dir: string, parentId: number | null = null, name: string | null = null, reasoning: string | null = null) {
  if (!['claude', 'codex'].includes(harness) || !model || !dir) throw new Error('Invalid session settings');
  return attach(Session.create(model, resolve(dir.replace(/^~(?=\/|$)/, homedir())), harness, parentId, name, reasoning));
}
function session(id: number) { const value = sessions.get(Number(id)); if (!value) throw new Error('Session not found'); return value; }
function listSessions() { return [...sessions.values()].map(s => { const last = db.query('SELECT json FROM events WHERE session_id = ? ORDER BY rowid DESC LIMIT 1').get(s.id) as {json:string} | null; return {...s.info, last_event: last ? JSON.parse(last.json) : null}; }); }
const tools = [
  ['delegate', 'Delegate a task to a background session', { harness: {type:'string', enum:['claude','codex']}, model:{type:'string'}, name:{type:'string', minLength:1}, reasoning:{type:'string', enum:['low','medium','high','xhigh','max','ultra'], description:'Claude: low, medium, high, xhigh, max; Codex: levels supported by the selected model'}, folder:{type:'string'}, task:{type:'string'} }, ['harness','model','name','reasoning','folder','task']],
  ['send', 'Send a message to a session', { id:{type:'number'}, message:{type:'string'} }, ['id','message']],
  ['list', 'List sessions', {}, []],
  ['read', 'Read session events', { id:{type:'number'} }, ['id']],
  ['stop', 'Stop a session', { id:{type:'number'} }, ['id']],
].map(([name, description, properties, required]) => ({name, description, inputSchema:{type:'object', properties, required}}));
async function call(name: string, args: any) {
  switch (name) {
    case 'delegate': { if (typeof args.name !== 'string' || !args.name.trim() || !['low','medium','high','xhigh','max','ultra'].includes(args.reasoning) || (args.harness === 'claude' && args.reasoning === 'ultra') || typeof args.task !== 'string' || !args.task.trim()) throw new Error('Invalid delegate settings'); if (args.harness === 'codex') { const model = (await modelDetails()).find(m => m.id === args.model); if (!model?.supportedReasoningEfforts.some(e => e.reasoningEffort === args.reasoning)) throw new Error('Unsupported model or reasoning level'); } const child = create(args.harness, args.model, args.folder, 1, args.name.trim(), args.reasoning); child.send(args.task); return {id:child.id, status:'running'}; }
    case 'send': session(args.id).send(args.message); return {ok:true};
    case 'list': return listSessions();
    case 'read': return session(args.id).events().slice(-100);
    case 'stop': if (args.id === 1) throw new Error('Cannot stop secretary through MCP'); session(args.id).stop(); return {ok:true};
    default: throw new Error('Unknown tool');
  }
}
Bun.serve({ hostname:'127.0.0.1', port:3001, async fetch(req) {
  if (new URL(req.url).pathname !== '/mcp' || req.method !== 'POST') return new Response(null,{status:405});
  const msg = await req.json();
  if (msg.id === undefined) return new Response(null,{status:202});
  const ok = (result: unknown) => Response.json({jsonrpc:'2.0',id:msg.id,result});
  if (msg.method === 'initialize') return ok({protocolVersion:msg.params.protocolVersion,capabilities:{tools:{}},serverInfo:{name:'secretary',version:'0.1'}});
  if (msg.method === 'tools/list') return ok({tools});
  if (msg.method === 'tools/call') { try { return ok({content:[{type:'text',text:JSON.stringify(await call(msg.params.name,msg.params.arguments || {}))}]}); } catch (error) { return ok({isError:true,content:[{type:'text',text:String(error)}]}); } }
  return Response.json({jsonrpc:'2.0',id:msg.id,error:{code:-32601,message:'Method not found'}});
}});

// Запросы из браузера с чужим Origin отклоняем; запросы без Origin (например, curl) пропускаем.
function sameOrigin(request: Request, url: URL) {
  const origin = request.headers.get('origin');
  if (!origin) return true;
  try { return new URL(origin).host === url.host; } catch { return false; }
}

const agentsFile = `${folder}/AGENTS.md`;

const server = Bun.serve({
  port: 3000, hostname: '127.0.0.1',
  fetch(request, server) {
    const url = new URL(request.url);
    if (url.pathname === '/models') {
      const harness = url.searchParams.get('harness');
      if (harness === 'claude') return Response.json(claudeModels);
      if (harness !== 'codex') return new Response('Unknown harness', {status:400});
      return models().then(result => Response.json(result)).catch(error => new Response(String(error), {status:500}));
    }
    if (url.pathname === '/settings/agents') {
      if (request.method === 'GET') return Bun.file(agentsFile).text()
        .then(text => new Response(text, {headers:{'content-type':'text/plain; charset=utf-8'}}))
        .catch(error => new Response(String(error), {status:500}));
      if (request.method === 'PUT') {
        if (!sameOrigin(request, url)) return new Response('Forbidden', {status:403});
        return request.text()
          .then(text => Bun.write(agentsFile, text))
          .then(() => new Response(null, {status:204}))
          .catch(error => new Response(String(error), {status:500}));
      }
      return new Response(null, {status:405});
    }
    if (url.pathname === '/ws') {
      if (!sameOrigin(request, url)) return new Response('Forbidden', {status:403});
      if (server.upgrade(request)) return;
      return new Response('WebSocket upgrade failed', { status: 400 });
    }
    const path = url.pathname === '/' ? '/index.html' : url.pathname;
    if (path.includes('..')) return new Response('Not found', {status:404});
    return new Response(Bun.file(`../web/dist${path}`));
  },
  websocket: {
    open(socket) { socket.subscribe('events'); socket.send(JSON.stringify({type:'sessions', sessions:listSessions()})); },
    message(socket, message) {
      try {
        const cmd = JSON.parse(String(message));
        if (cmd.type === 'list') socket.send(JSON.stringify({type:'sessions', sessions:listSessions()}));
        if (cmd.type === 'read') socket.send(JSON.stringify({type:'history', id:cmd.id, events:session(cmd.id).events()}));
        if (cmd.type === 'send') session(cmd.id).send(cmd.text);
        if (cmd.type === 'stop') session(cmd.id).stop();
        if (cmd.type === 'delete') { const s = session(cmd.id); s.delete(); sessions.delete(s.id); server.publish('events', JSON.stringify({type:'sessions', sessions:listSessions()})); }
        if (cmd.type === 'create') { const s = create(cmd.harness, cmd.model, cmd.folder); socket.send(JSON.stringify({type:'created', id:s.id})); server.publish('events', JSON.stringify({type:'sessions', sessions:listSessions()})); }
        if (cmd.type === 'settings') { const s = session(cmd.id); if (s.info.status === 'running') throw new Error('Session is running'); if (s.info.harness !== cmd.harness) s.switchHarness(cmd.harness,cmd.model); else s.setModel(cmd.model); }
      } catch (error) { socket.send(JSON.stringify({type:'error',message:String(error)})); }
    },
  },
});
for (const row of interrupted) session(row.id).send('продолжай');
console.log(`Secretary: ${server.url}`);
