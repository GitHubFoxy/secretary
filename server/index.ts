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
    message(socket, message) {
      socket.send(message);
    },
  },
});

console.log(`Secretary: ${server.url}`);
