import { authorized, pair } from './auth';
import { chatPage, chatMessage } from './chat';
import { claudeTurn } from './claude';
import { codexTurn } from './codex';
import { secretaryFolder } from './config';
import { migrateDatabase, openDatabase } from './database';
import { serveMcp } from './mcp';
import { checkReasoning, modelDetails } from './models';
import { ensurePersona } from './persona';
import { Sessions } from './sessions';
import { Speech, warmSpeech, ttsEngine, setTtsEngine } from './speech';
import { transcribe, voiceModel, setVoiceModel } from './voice';

const videoIntervalFile = `${secretaryFolder}/video-interval`;
const agentsFile = `${secretaryFolder}/AGENTS.md`;
ensurePersona(secretaryFolder);

const databasePath = `${secretaryFolder}/secretary.sqlite`;
migrateDatabase('secretary.sqlite', databasePath);
const db = openDatabase(databasePath);
const publish = (message: object) => server.publish('events', JSON.stringify(message));
const speech = new Speech(publish);
const publishSessions = () => publish({ type: 'sessions', sessions: sessions.list() });
const sessions = new Sessions(db, {
  turns: { claude: claudeTurn, codex: codexTurn },
  checkModel: checkReasoning,
  onChange: publishSessions,
  // The browser gets chat messages, the queue and interrupt results; other raw events stay in SQLite.
  onEvent(session, event, cursor) {
    if (event.type === 'secretary_reply') { publish(event); return; }
    if (event.type === 'speech_start') { speech.start(); publish({type:'speech_start'}); return; }
    if (event.type === 'speech_event') { speech.event(event.event); return; }
    if (event.type === 'speech_end') { speech.end(event.interrupted); return; }
    const message = chatMessage(event);
    if (message) publish({ type: 'message', sessionId: session.id, message: {...message, cursor} });
    else if (event.type === 'queue' || event.type === 'interrupt_result') publish({ ...event, sessionId: session.id });
  },
});
sessions.load(secretaryFolder);
serveMcp(sessions);

// Browsers send Origin; a foreign one is rejected. Requests without Origin (curl) pass.
function sameOrigin(request: Request, url: URL) {
  const origin = request.headers.get('origin');
  if (!origin) return true;
  try { return new URL(origin).host === url.host; } catch { return false; }
}

// The browser adapter. Every command gets exactly one answer with its requestId: `reply` with the result or `error`.
const commands: Record<string, (cmd: any) => unknown> = {
  read: cmd => {
    const session = sessions.get(cmd.id);
    return {...chatPage(db, session.id, cmd.all === true), queue: session.queue.list()};
  },
  send: cmd => { sessions.get(cmd.id).send(cmd.text, cmd.images, cmd.immediate === true, cmd.voice === true); },
  sendQueued: cmd => { sessions.get(cmd.id).sendQueued(cmd.messageId); },
  speechWarm: () => { speech.stop(); publish({type:'speech_cancel'}); warmSpeech(); },
  speechStop: () => { speech.stop(); publish({type:'speech_cancel'}); },
  stop: cmd => { speech.stop(); publish({type:'speech_cancel'}); sessions.stop(cmd.id); },
  interrupt: cmd => { speech.stop(); publish({type:'speech_cancel'}); return { accepted: sessions.get(cmd.id).interrupt(cmd.requestId) }; },
  rename: cmd => { sessions.get(cmd.id).setName(cmd.name); publishSessions(); },
  visibility: cmd => { sessions.get(cmd.id).setHidden(cmd.hidden); publishSessions(); },
  delete: cmd => { sessions.delete(cmd.id); },
  create: async cmd => ({ id: (await sessions.create({ harness: cmd.harness, model: cmd.model, reasoning: cmd.reasoning ?? null, folder: cmd.folder })).id }),
  settings: cmd => sessions.configure(cmd.id, { harness: cmd.harness, model: cmd.model, reasoning: cmd.reasoning ?? null }),
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
    if (url.pathname === '/voice' && request.method === 'POST') {
      const size = Number(request.headers.get('content-length'));
      if (!size || size > 20 * 1024 * 1024) return new Response('Запись слишком большая', {status: 413});
      return request.arrayBuffer().then(bytes => transcribe(new Uint8Array(bytes))).then(text => Response.json({text}), error => new Response(error.message, {status: 500}));
    }
    if (url.pathname === '/model-details')
      return Promise.resolve().then(() => modelDetails(url.searchParams.get('harness') || '', true)).then(result => Response.json(result), error => new Response(String(error), { status: 500 }));
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
    open(socket) { socket.subscribe('events'); socket.send(JSON.stringify({ type: 'sessions', sessions: sessions.list() })); },
    async message(socket, message) {
      let cmd: any;
      try {
        cmd = JSON.parse(String(message));
        const command = commands[cmd.type];
        if (!command) throw new Error(`Неизвестная команда: ${cmd.type}`);
        const result = await command(cmd);
        socket.send(JSON.stringify({ type: 'reply', requestId: cmd.requestId, ...(result ?? {}) }));
      } catch (error) { socket.send(JSON.stringify({ type: 'error', message: String(error), requestId: cmd?.requestId })); }
    },
  },
});

// After the server exists: resumed turns publish through it.
sessions.resumePending();

console.log(`Secretary: ${server.url}`);
