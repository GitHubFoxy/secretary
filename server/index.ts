import { Session } from './session';

const sessions = new Map<number, Session>();

const server = Bun.serve({
  port: 3000,
  fetch(request, server) {
    const url = new URL(request.url);
    if (url.pathname === '/ws') {
      if (server.upgrade(request)) return;
      return new Response('WebSocket upgrade failed', { status: 400 });
    }
    return new Response(Bun.file(`../web/dist${url.pathname === '/' ? '/index.html' : url.pathname}`));
  },
  websocket: {
    open(socket) { socket.subscribe('events'); },
    message(socket, message) {
      const command = JSON.parse(String(message));
      if (command.type === 'create') {
        const session = Session.create(command.model, command.folder);
        const id = session.id;
        sessions.set(id, session);
        session.onEvent(event => server.publish('events', JSON.stringify({ sessionId: id, event })));
        socket.send(JSON.stringify({ type: 'created', id }));
      } else if (command.type === 'send') {
        sessions.get(command.id)?.send(command.text);
      }
    },
  },
});

console.log(`Secretary: ${server.url}`);
