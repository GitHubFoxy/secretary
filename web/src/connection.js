export function createConnection({url, onOpen, onClose, onMessage, WebSocketImpl = WebSocket, schedule = setTimeout, cancel = clearTimeout}) {
  let socket, timer, stopped = false, attempts = 0;
  // Every command gets one answer with its requestId: `reply` or `error`. Unanswered ones fail when the socket closes.
  const waiting = new Map();
  function failWaiting() {
    for (const {reject} of waiting.values()) reject(Object.assign(new Error('нет соединения'), {disconnected:true}));
    waiting.clear();
  }
  function connect() {
    if (stopped || socket && socket.readyState < 2) return;
    clearTimer();
    const current = socket = new WebSocketImpl(url);
    current.onopen = () => { if (socket !== current || stopped) return; attempts = 0; onOpen(); };
    current.onmessage = event => {
      if (socket !== current || stopped) return;
      const message = JSON.parse(event.data);
      const answer = (message.type === 'reply' || message.type === 'error') && waiting.get(message.requestId);
      if (!answer) return onMessage(message);
      waiting.delete(message.requestId);
      if (message.type === 'reply') answer.resolve(message);
      else answer.reject(new Error(message.message));
    };
    current.onclose = () => {
      if (socket !== current || stopped) return;
      socket = null;
      failWaiting();
      onClose();
      const delay = Math.min(30000, 1000 * 2 ** Math.min(attempts++, 5));
      timer = schedule(connect, delay);
    };
    current.onerror = () => current.close();
  }
  function clearTimer() { if (timer != null) { cancel(timer); timer = null; } }
  return {
    connect,
    reconnect() {
      if (socket) { const current = socket; socket = null; current.close(); failWaiting(); onClose(); }
      connect();
    },
    request(command) {
      if (!socket || socket.readyState !== 1) return Promise.reject(Object.assign(new Error('нет соединения'), {disconnected:true}));
      const requestId = command.requestId ?? crypto.randomUUID();
      return new Promise((resolve, reject) => {
        waiting.set(requestId, {resolve, reject});
        socket.send(JSON.stringify({...command, requestId}));
      });
    },
    stop() { stopped = true; clearTimer(); failWaiting(); const current = socket; socket = null; current?.close(); },
  };
}
