import test from 'node:test';
import assert from 'node:assert/strict';
import { readFile } from 'node:fs/promises';
import vm from 'node:vm';

const source = await readFile(new URL('./App.svelte', import.meta.url), 'utf8');
function deferred() {
  let resolve, reject;
  const promise = new Promise((yes, no) => { resolve = yes; reject = no; });
  return { promise, resolve, reject };
}
function harness() {
  const requests = [], sockets = [];
  class Socket {
    constructor(url) { this.url = url; sockets.push(this); }
    close() { this.closed = true; }
  }
  const context = vm.createContext({
    secretarySocket: null, secretaryStreamError: '', secretaryTurnID: '', secretaryStream: [],
    secretaryConnectionGeneration: 0, refreshRequestGeneration: 0, authenticated: true,
    secretaryReconnectTimer: null, observer: null, workers: [], nodes: [],
    location: {protocol: 'http:', host: 'example.test'}, WebSocket: Socket,
    request: (url) => { const wait = deferred(); requests.push({url, ...wait}); return wait.promise; },
    mergeSequenced: (_, incoming) => incoming,
    appendSecretaryEvent: (event) => context.secretaryStream.push(event),
    setTimeout: (callback) => { context.reconnect = callback; return 1; },
    clearTimeout: () => { context.reconnect = null; },
  });
  for (const name of ['connectSecretaryStream', 'refreshState']) {
    const start = source.indexOf(`  async function ${name}(`);
    const end = source.indexOf('\n  function ', start + 1);
    const asyncEnd = source.indexOf('\n  async function ', start + 1);
    vm.runInContext(source.slice(start, Math.min(...[end, asyncEnd].filter((position) => position > start))), context);
  }
  return { context, requests, sockets };
}

test('late replay cannot replace the selected turn or its socket', async () => {
  const {context, requests, sockets} = harness();
  const first = context.connectSecretaryStream('A');
  context.secretaryStream = [{turn: 'A', seq: 1}];
  const second = context.connectSecretaryStream('B');
  assert.equal(context.secretaryStream.length, 0);
  requests[1].resolve({events: [{turn: 'B', seq: 2}]});
  await second;
  requests[0].resolve({events: [{turn: 'A', seq: 1}]});
  await first;
  assert.equal(context.secretaryTurnID, 'B');
  assert.equal(context.secretaryStream[0].turn, 'B');
  assert.equal(sockets.length, 1);
  assert.match(context.secretarySocket.url, /turns\/B\//);
});

test('callbacks from a replaced socket cannot affect a same-turn reconnect', async () => {
  const {context, requests, sockets} = harness();
  const first = context.connectSecretaryStream('A');
  requests[0].resolve({events: []});
  await first;
  const second = context.connectSecretaryStream('A');
  requests[1].resolve({events: [{turn: 'A', seq: 2}]});
  await second;
  sockets[0].onmessage({data: JSON.stringify({turn: 'stale'})});
  sockets[0].onerror();
  sockets[0].onclose();
  assert.equal(sockets[1].closed, undefined);
  assert.equal(context.secretaryStream.length, 1);
  assert.equal(context.reconnect, undefined);
  sockets[1].onclose();
  assert.equal(typeof context.reconnect, 'function');
  context.reconnect();
  assert.equal(requests.length, 3);
});

test('late replay errors cannot replace current stream error', async () => {
  const {context, requests} = harness();
  const first = context.connectSecretaryStream('A');
  const second = context.connectSecretaryStream('B');
  requests[1].resolve({events: []});
  await second;
  requests[0].reject(new Error('stale failure'));
  await first;
  assert.equal(context.secretaryStreamError, '');
});

test('out-of-order bootstrap refresh and old bootstrap during user selection are ignored', async () => {
  const {context, requests} = harness();
  const first = context.refreshState();
  const second = context.refreshState();
  requests[3].resolve([]); requests[4].resolve([]); requests[5].resolve({secretary_turn_id:'B'});
  await new Promise((resolve) => setImmediate(resolve));
  requests[6].resolve({events: [{turn:'B'}]});
  await second;
  requests[0].resolve([]); requests[1].resolve([]); requests[2].resolve({secretary_turn_id:'A'});
  await new Promise((resolve) => setImmediate(resolve));
  if (requests[7]) requests[7].resolve({events:[{turn:'A'}]});
  await first;
  assert.equal(context.secretaryTurnID, 'B');
  const third = context.refreshState();
  const chosen = context.connectSecretaryStream('C');
  requests[10].resolve({events: [{turn:'C'}]});
  await chosen;
  requests[7].resolve([]); requests[8].resolve([]); requests[9].resolve({secretary_turn_id:'B'});
  await third;
  assert.equal(context.secretaryTurnID, 'C');
  assert.equal(context.secretaryStream[0].turn, 'C');
});
