export function createConnection({url, onOpen, onClose, onMessage, WebSocketImpl = WebSocket, schedule = setTimeout, cancel = clearTimeout}) {
  let socket, timer, stopped = false, attempts = 0;
  function connect() {
    if (stopped || socket && socket.readyState < 2) return;
    clearTimer();
    const current = socket = new WebSocketImpl(url);
    current.onopen = () => { if (socket !== current || stopped) return; attempts = 0; onOpen(); };
    current.onmessage = event => { if (socket === current && !stopped) onMessage(event); };
    current.onclose = () => {
      if (socket !== current || stopped) return;
      socket = null;
      onClose();
      const delay = Math.min(30000, 1000 * 2 ** Math.min(attempts++, 5));
      timer = schedule(connect, delay);
    };
    current.onerror = () => current.close();
  }
  function clearTimer() { if (timer != null) { cancel(timer); timer = null; } }
  return {
    connect,
    send(value) { if (!socket || socket.readyState !== 1) return false; socket.send(JSON.stringify(value)); return true; },
    stop() { stopped = true; clearTimer(); const current = socket; socket = null; current?.close(); },
  };
}
