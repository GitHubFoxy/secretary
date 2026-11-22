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
    onOpen:() => c.request({type:'read', id:1, requestId:'r'}).catch(() => {}), onClose:() => {}, onMessage:() => {} });
  c.connect(); c.request({type:'send'}).catch(e => expect(e.disconnected).toBe(true));
  sockets[0].open(); c.request({type:'send', text:'hello', requestId:'s'}).catch(() => {});
  sockets[0].close(); expect(timers[0].delay).toBe(1000);
  timers[0].fn(); sockets[1].close(); expect(timers[1].delay).toBe(2000);
  timers[1].fn(); sockets[2].open();
  expect(calls).toEqual([{type:'read',id:1,requestId:'r'},{type:'send',text:'hello',requestId:'s'},{type:'read',id:1,requestId:'r'}]);
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

test('ответ сопоставляется с запросом, остальные сообщения идут дальше, обрыв отклоняет ожидающие', async () => {
  const sockets = [], other = [];
  class FakeSocket { constructor() { this.readyState = 0; sockets.push(this); } send() {} close() { this.readyState = 3; this.onclose?.(); } }
  const c = createConnection({url:'ws://test', WebSocketImpl:FakeSocket, schedule:() => 0, cancel:() => {}, onOpen:()=>{}, onClose:()=>{}, onMessage:m => other.push(m)});
  c.connect(); sockets[0].readyState = 1; sockets[0].onopen();
  const ok = c.request({type:'read', requestId:'a'}), bad = c.request({type:'send', requestId:'b'}), lost = c.request({type:'stop', requestId:'c'});
  const deliver = m => sockets[0].onmessage({data:JSON.stringify(m)});
  deliver({type:'reply', requestId:'a', value:1}); deliver({type:'error', requestId:'b', message:'нельзя'}); deliver({type:'sessions', sessions:[]});
  expect(await ok).toMatchObject({value:1});
  await expect(bad).rejects.toThrow('нельзя');
  expect(other).toEqual([{type:'sessions', sessions:[]}]);
  sockets[0].close();
  await expect(lost).rejects.toMatchObject({disconnected:true});
});

test('возврат в приложение заменяет зависшее соединение и отклоняет незавершённый запрос', async () => {
  const sockets = [];
  class FakeSocket {
    constructor() { this.readyState = 0; sockets.push(this); }
    send() {}
    close() { this.readyState = 2; }
  }
  const c = createConnection({url:'ws://test', WebSocketImpl:FakeSocket, onOpen:()=>{}, onClose:()=>{}, onMessage:()=>{}});
  c.connect(); sockets[0].readyState = 1; sockets[0].onopen();
  const pending = c.request({type:'read', requestId:'r'});
  c.reconnect();
  await expect(pending).rejects.toMatchObject({disconnected:true});
  expect(sockets).toHaveLength(2);
  c.stop();
});
