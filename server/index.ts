import { Skills } from './skills';
import { servePreview } from './preview';
import { authorized, pair } from './auth';
import { slashCommands } from './commands';
import { findFiles } from './references';
import { chatPage, chatMessage } from './chat';
import { claudeTurn } from './claude';
import { codexRateLimits, codexTurn } from './codex';
import { port, secretaryFolder } from './config';
import { fileResponse, saveFile } from './files';
import { migrateDatabase, openDatabase } from './database';
import { serveMcp } from './mcp';
import { Memory } from './memory';
import { Sudo } from './sudo';
import { getDraft, saveDraft } from './drafts';
import { listNotes, saveNote, deleteNote } from './notes';
import { checkReasoning, modelDetails } from './models';
import { ensurePersona, instructionsFile, setSpecialistModel, specialistModel, systemPrompts, type Role } from './persona';
import { Sessions } from './sessions';
import { Speech, warmSpeech, ttsEngine, setTtsEngine } from './speech';
import { transcribe, voiceModel, setVoiceModel } from './voice';
import { turnComplete } from './smart-turn';

const memory = new Memory(secretaryFolder);
const videoIntervalFile = `${secretaryFolder}/video-interval`;
ensurePersona(secretaryFolder);

const databasePath = `${secretaryFolder}/secretary.sqlite`;
migrateDatabase('secretary.sqlite', databasePath);
const db = openDatabase(databasePath);
const publish = (message: object) => server.publish('events', JSON.stringify(message));
let speechTarget: ((message: object) => void) | undefined;
let speechOwner: ((message: object) => void) | undefined;
let speechEventSeen = false;
const speech = new Speech(message => speechTarget?.(message));
function stopSpeech() { speech.stop(); speechTarget?.({type:'speech_cancel'}); speechTarget = undefined; speechOwner = undefined; }
const publishSessions = () => publish({ type: 'sessions', sessions: sessions.list() });
const sessions = new Sessions(db, {
  turns: { claude: claudeTurn, codex: codexTurn },
  checkModel: checkReasoning,
  onChange: publishSessions,
  // The browser gets chat messages, the queue and interrupt results; other raw events stay in SQLite.
  onEvent(session, event, cursor) {
    if (event.type === 'secretary_reply') { publish(event); return; }
    if (event.type === 'speech_start') { stopSpeech(); speechTarget = event.speechTarget; speechOwner = event.speechTarget.owner ?? event.speechTarget; speechEventSeen = false; speech.start(); speechTarget?.({type:'speech_start'}); return; }
    if (event.type === 'speech_event') { if (!speechEventSeen) { speechEventSeen = true; speechTarget?.({type:'speech_event'}); } speech.event(event.event); return; }
    if (event.type === 'speech_end') { speech.end(event.interrupted); return; }
    const time = cursor === undefined ? '' : (db.query('SELECT time FROM events WHERE rowid = ?').get(cursor) as {time: string}).time;
    const message = chatMessage(event, time);
    if (message) publish({ type: 'message', sessionId: session.id, message: {...message, cursor} });
    else if (event.type === 'queue' || event.type === 'interrupt_result') publish({ ...event, sessionId: session.id });
  },
});

const skills = new Skills(secretaryFolder, (id, event) => sessions.get(id).emit(event));
const sudo = new Sudo((id, event) => sessions.get(id).emit(event));

// Browsers send Origin; a foreign one is rejected. Requests without Origin (curl) pass.
function sameOrigin(request: Request, url: URL) {
  const origin = request.headers.get('origin');
  if (!origin) return true;
  try { return new URL(origin).host === url.host; } catch { return false; }
}

// The browser adapter. Every command gets exactly one answer with its requestId: `reply` with the result or `error`.
const commands: Record<string, (cmd: any, send: (message: object) => void) => unknown> = {
  compact: cmd => sessions.get(cmd.id).compact(),
  references: async cmd => {
    const session = sessions.get(cmd.id);
    const query = cmd.query;
    const files = await findFiles(session.info.folder, query);
    const agents = sessions.list().filter(s => !s.internal && s.id !== session.id && `${s.name || ''} #${s.id}`.toLocaleLowerCase().includes(query.toLocaleLowerCase())).slice(0, 30);
    return { references: [...agents.map(s => ({label:`@${s.name || 'Session'} (#${s.id})`, value:`@${s.name || 'Session'} (#${s.id})`})), ...files.map(path => ({label:path, value:path}))] };
  },
  slashCommands: async cmd => { const {harness, folder} = sessions.get(cmd.id).info; return {commands: await slashCommands(harness, folder)}; },
  skillApprove: cmd => skills.approve(cmd.skillId),
  skillReject: cmd => skills.reject(cmd.skillId),
  sudoApprove: cmd => sudo.approve(cmd.sudoId, cmd.password),
  sudoReject: cmd => sudo.reject(cmd.sudoId),
  draft: cmd => ({text: getDraft(db, sessions.get(cmd.id).id)}),
  draftSave: cmd => { const session = sessions.get(cmd.id); if (typeof cmd.from !== 'string') throw new Error('Некорректный id вкладки'); saveDraft(db, session.id, cmd.text); publish({type:'draftChanged', sessionId:session.id, text:cmd.text, from:cmd.from}); },
  notes: cmd => ({notes: listNotes(db, sessions.get(cmd.id).id)}),
  noteSave: cmd => { const session = sessions.get(cmd.id); saveNote(db, session.id, cmd.noteId, cmd.text); publish({type:'notesChanged', sessionId:session.id}); },
  noteDelete: cmd => { const session = sessions.get(cmd.id); deleteNote(db, session.id, cmd.noteId); publish({type:'notesChanged', sessionId:session.id}); },
  read: cmd => {
    const session = sessions.get(cmd.id);
    return {...chatPage(db, session.id, Number.isSafeInteger(cmd.before) && cmd.before > 0 ? cmd.before : undefined), queue: session.queue.list()};
  },
  send: (cmd, send) => {
    const target = typeof cmd.voiceTimingId === 'string' && cmd.voiceTimingId.length <= 64
      ? Object.assign((message: object) => send({...message, voiceTimingId:cmd.voiceTimingId}), {owner:send}) : send;
    sessions.get(cmd.id).send(cmd.text, cmd.images, cmd.immediate === true, cmd.voice === true, cmd.voice === true ? target : undefined, cmd.files);
  },
  voiceTiming: cmd => {
    const keys = ['listening', 'end', 'recognized', 'event', 'audio'];
    if (!cmd.values || keys.some(key => cmd.values[key] !== null && (!Number.isFinite(cmd.values[key]) || cmd.values[key] < 0))) throw new Error('Некорректный замер Voice Call');
    console.log('Voice Call ms', JSON.stringify(Object.fromEntries(keys.map(key => [key, cmd.values[key]]))));
  },
  sendQueued: cmd => { sessions.get(cmd.id).sendQueued(cmd.messageId); },
  speechWarm: (_, send) => { if (speechOwner === send) stopSpeech(); warmSpeech(); },
  speechStop: (_, send) => { if (speechOwner === send) stopSpeech(); },
  stop: cmd => { stopSpeech(); sessions.stop(cmd.id); },
  interrupt: cmd => { stopSpeech(); return { accepted: sessions.get(cmd.id).interrupt(cmd.requestId) }; },
  rename: cmd => { sessions.get(cmd.id).setName(cmd.name); publishSessions(); },
  visibility: cmd => { sessions.setHidden(cmd.id, cmd.hidden); },
  delete: cmd => { sessions.delete(cmd.id); },
  create: async cmd => ({ id: (await sessions.create({ harness: cmd.harness, model: cmd.model, reasoning: cmd.reasoning ?? null, folder: cmd.folder })).id }),
  settings: cmd => sessions.configure(cmd.id, { harness: cmd.harness, model: cmd.model, reasoning: cmd.reasoning ?? null }),
};

const server = Bun.serve({
  port, hostname: '127.0.0.1',
  async fetch(request, server) {
    const response = await handleRequest(request, server);
    if (response) {
      response.headers.set('Content-Security-Policy', "default-src 'self'; script-src 'self'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; media-src 'self' blob:; connect-src 'self' ws: wss:; frame-ancestors 'none'");
      response.headers.set('X-Frame-Options', 'DENY');
      response.headers.set('Referrer-Policy', 'no-referrer');
      response.headers.set('X-Content-Type-Options', 'nosniff');
    }
    return response;
  },
  websocket: {
    maxPayloadLength: 30 * 1024 * 1024,
    open(socket) { socket.data = (message: object) => { if (socket.readyState === 1) socket.send(JSON.stringify(message)); }; socket.subscribe('events'); socket.send(JSON.stringify({ type: 'sessions', sessions: sessions.list() })); },
    async message(socket, message) {
      let cmd: any;
      try {
        cmd = JSON.parse(String(message));
        const command = commands[cmd.type];
        if (!command) throw new Error(`Неизвестная команда: ${cmd.type}`);
        const result = await command(cmd, socket.data as (message: object) => void);
        socket.send(JSON.stringify({ type: 'reply', requestId: cmd.requestId, ...(result ?? {}) }));
      } catch (error) { socket.send(JSON.stringify({ type: 'error', message: String(error), requestId: cmd?.requestId })); }
    },
  },
});

// Loading and resuming sessions can publish events.
sessions.load(secretaryFolder);
serveMcp(sessions, { sudo, skills });
servePreview();
sessions.resumePending();
sessions.schedules.tick();
setInterval(() => sessions.schedules.tick(), 30_000);

console.log(`Secretary: ${server.url}`);

function handleRequest(request: Request, server: any) {
    const url = new URL(request.url);
    if (url.pathname === '/pair' && request.method === 'POST') {
      if (!sameOrigin(request, url)) return new Response('Forbidden', { status: 403 });
      return request.formData().then(form => {
        const token = pair(String(form.get('code') || ''), server.requestIP(request)?.address ?? 'unknown');
        if (!token) return new Response('Неверный или просроченный код.', { status: 401 });
        return new Response(null, { status: 303, headers: { 'Location': '/', 'Set-Cookie': `secretary=${token}; HttpOnly; Secure; SameSite=Strict; Path=/; Max-Age=31536000`, 'Cache-Control': 'no-store' } });
      }).catch(() => new Response('Invalid request', { status: 400 }));
    }
    if (!authorized(request)) {
      if (url.pathname === '/' && request.method === 'GET') return new Response(Bun.file(`${import.meta.dir}/login.html`), { headers: { 'Content-Type': 'text/html; charset=utf-8', 'Cache-Control': 'no-store' } });
      return new Response('Unauthorized', { status: 401, headers: { 'Cache-Control': 'no-store' } });
    }
    if ((request.method !== 'GET' || url.pathname === '/ws') && !sameOrigin(request, url)) return new Response('Forbidden', { status: 403 });
    if (url.pathname === '/files' && request.method === 'POST')
      return saveFile(request).then(file => Response.json(file), error => new Response(String(error), {status:400}));
    const stored = url.pathname.match(/^\/files\/([^/]+)\/([^/]+)$/);
    if (stored && request.method === 'GET')
      return fileResponse(stored[1], stored[2], url.searchParams.has('download')).then(response => response ?? new Response('Not found', {status:404}));
    if (url.pathname === '/voice/turn' && request.method === 'POST') {
      const size = Number(request.headers.get('content-length'));
      if (!size || size > 20 * 1024 * 1024) return new Response('Запись слишком большая', {status:413});
      return request.arrayBuffer().then(bytes => turnComplete(new Uint8Array(bytes))).then(complete => Response.json({complete}));
    }
    if (url.pathname === '/voice' && request.method === 'POST') {
      const size = Number(request.headers.get('content-length'));
      if (!size || size > 20 * 1024 * 1024) return new Response('Запись слишком большая', {status: 413});
      return request.arrayBuffer().then(bytes => transcribe(new Uint8Array(bytes))).then(text => Response.json({text}), error => new Response(error.message, {status: 500}));
    }
    if (url.pathname === '/model-details')
      return Promise.resolve().then(() => modelDetails(url.searchParams.get('harness') || '', true)).then(result => Response.json(result), error => new Response(String(error), { status: 500 }));
    if (url.pathname === '/codex/limits')
      return codexRateLimits().then(limits => Response.json(limits), error => new Response(error.message, { status: 500 }));
    if (url.pathname === '/settings/video') {
      if (request.method === 'GET') return Bun.file(videoIntervalFile).exists().then(exists => exists ? Bun.file(videoIntervalFile).text() : '1').then(interval => Response.json({interval}));
      if (request.method === 'PUT') return request.text().then(value => { if (!['1', '2'].includes(value)) throw new Error('Недопустимая частота кадров'); return Bun.write(videoIntervalFile, value); }).then(() => new Response(null, {status:204}), error => new Response(String(error), {status:400}));
      return new Response(null, {status:405});
    }
    if (url.pathname === '/settings/voice') {
      if (request.method === 'GET') return voiceModel().then(model => Response.json({model}), error => new Response(String(error), {status: 500}));
      if (request.method === 'PUT') return request.text().then(setVoiceModel).then(() => new Response(null, {status: 204}), error => new Response(String(error), {status: 400}));
      return new Response(null, {status: 405});
    }
    if (url.pathname === '/settings/tts') {
      if (request.method === 'GET') return ttsEngine().then(engine => Response.json({engine}), error => new Response(String(error), {status:500}));
      if (request.method === 'PUT') return request.text().then(setTtsEngine).then(() => new Response(null, {status:204}), error => new Response(String(error), {status:400}));
      return new Response(null, {status:405});
    }
    if (url.pathname === '/settings/memory') {
      try {
        const name = url.searchParams.get('name');
        if (request.method === 'GET') return Response.json(name === null ? memory.list() : memory.read(name));
        if (request.method === 'PUT' || request.method === 'DELETE') return request.json().then(data => {
          if (!name) throw new Error('Укажите имя файла');
          if (request.method === 'PUT') return Response.json(memory.write(name, data.text, data.version));
          memory.delete(name, data.version);
          return new Response(null, {status: 204});
        }).catch(error => new Response(String(error), {status: 400}));
        return new Response(null, {status: 405});
      } catch (error) { return new Response(String(error), {status: 400}); }
    }
    const role = url.pathname.match(/^\/settings\/agents\/(secretary|specialist)$/)?.[1] as Role | undefined;
    if (role) {
      const file = Bun.file(instructionsFile(secretaryFolder, role));
      if (request.method === 'GET') return file.exists().then(async exists => Response.json({ text: exists ? await file.text() : '', system: systemPrompts[role] }), error => new Response(String(error), { status: 500 }));
      if (request.method === 'PUT') return request.text().then(text => Bun.write(file, text)).then(() => new Response(null, { status: 204 }), error => new Response(String(error), { status: 500 }));
      return new Response(null, { status: 405 });
    }
    if (url.pathname === '/settings/specialist-model') {
      if (request.method === 'GET') return Response.json(specialistModel(secretaryFolder));
      if (request.method === 'PUT') return request.json().then(async settings => {
        if (settings) await sessions.check(settings);
        setSpecialistModel(secretaryFolder, settings);
      }).then(() => new Response(null, { status: 204 }), error => new Response(String(error), { status: 400 }));
      return new Response(null, { status: 405 });
    }
    if (url.pathname === '/ws') return server.upgrade(request) ? undefined : new Response('WebSocket upgrade failed', { status: 400 });
    const path = url.pathname === '/' ? '/index.html' : url.pathname;
    if (path.includes('..')) return new Response('Not found', { status: 404 });
    return new Response(Bun.file(`${import.meta.dir}/../web/dist${path}`), { headers: { 'Cache-Control': ['/index.html', '/build-version.json', '/sw.js'].includes(path) ? 'no-store' : 'no-cache' } });
}
