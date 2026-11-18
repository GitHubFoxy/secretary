import { expect, test } from 'bun:test';
import { createConnection } from './connection.js';

test('reconnects with backoff, refreshes on open and never resends a command', () => {
  const sockets = [], timers = [], calls = [];
  class FakeSocket {
    constructor(url) { this.url = url; this.readyState = 0; sockets.push(this); }
    send(data) { calls.push(JSON.parse(data)); }
    close() { this.readyState = 3; this.onclose?.(); }
    open() { this.readyState = 1; this.onopen(); }
  }
  const c = createConnection({ url:'ws://example/ws', WebSocketImpl:FakeSocket,
    schedule:(fn, delay) => { const t = {fn, delay}; timers.push(t); return t; }, cancel:t => { t.cancelled = true; },
    onOpen:() => c.send({type:'read', id:1}), onClose:() => {}, onMessage:() => {} });
  c.connect(); expect(c.send({type:'send'})).toBe(false);
  sockets[0].open(); expect(c.send({type:'send', text:'hello'})).toBe(true);
  sockets[0].close(); expect(timers[0].delay).toBe(1000);
  timers[0].fn(); sockets[1].close(); expect(timers[1].delay).toBe(2000);
  timers[1].fn(); sockets[2].open();
  expect(calls).toEqual([{type:'read',id:1},{type:'send',text:'hello'},{type:'read',id:1}]);
  sockets[2].close(); expect(timers[2].delay).toBe(1000);
  c.stop(); expect(timers[2].cancelled).toBe(true);
});

test('manual reconnect cancels pending timer and does not duplicate sockets', () => {
  const sockets = [], timers = [];
  class FakeSocket { constructor() { this.readyState = 0; sockets.push(this); } close() { this.readyState = 3; this.onclose?.(); } }
  const c = createConnection({url:'ws://test', WebSocketImpl:FakeSocket, schedule:(fn, delay) => { const t = {fn, delay}; timers.push(t); return t; }, cancel:t => t.cancelled = true, onOpen:()=>{}, onClose:()=>{}, onMessage:()=>{}});
  c.connect(); c.connect(); expect(sockets.length).toBe(1);
  sockets[0].close(); c.connect(); expect(timers[0].cancelled).toBe(true); expect(sockets.length).toBe(2);
  c.stop();
});
