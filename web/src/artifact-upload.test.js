import test from 'node:test';
import assert from 'node:assert/strict';
import { installUpload } from '../artifact-upload.js';

test('actual Web attachment change/retry retains idempotency key after lost upload response', async () => {
  const oldDocument = globalThis.document;
  const oldStorage = globalThis.sessionStorage;
  const oldFetch = globalThis.fetch;
  const oldEvent = globalThis.Event;
  const memory = new Map();
  let selectedInput;
  const form = {
    style: {},
    nodes: [],
    querySelector: () => null,
    insertBefore(node) { this.nodes.push(node); },
    append(node) { this.nodes.push(node); }
  };
  const composer = {
    value: 'What is this?',
    closest: () => form,
    dispatchEvent() {},
    focus() {}
  };
  globalThis.document = {
    createElement(tag) {
      const element = {
        tag, dataset: {}, style: {}, hidden: false, disabled: false, files: [], value: '',
        setAttribute() {},
        append(child) { this.child = child; if (child.tag === 'input') selectedInput = child; },
        addEventListener(kind, fn) { this.events ??= {}; this.events[kind] = fn; }
      };
      return element;
    }
  };
  globalThis.sessionStorage = {
    getItem(key) { return memory.get(key) || null; },
    setItem(key, value) { memory.set(key, value); },
    removeItem(key) { memory.delete(key); }
  };
  globalThis.Event = class { constructor(type) { this.type = type; } };
  const keys = [];
  globalThis.fetch = async (_url, options) => {
    keys.push(options.headers['Idempotency-Key']);
    if (keys.length === 1) throw new Error('response lost after server committed');
    return {
      ok: true,
      async json() { return { reference: 'artifact://art_' + 'a'.repeat(32), artifact: { id: 'art_' + 'a'.repeat(32) }, duplicate: keys.length === 2 }; }
    };
  };
  try {
    installUpload(composer, false);
    const bytes = Uint8Array.of(137, 80, 78, 71, 1, 2, 3);
    const file = { name: 'image.png', size: bytes.length, arrayBuffer: async () => bytes.buffer };
    selectedInput.files = [file];
    await selectedInput.events.change();
    assert.equal(composer.value, 'What is this?');
    assert.equal(keys.length, 1);
    assert.equal(memory.size, 1);

    selectedInput.files = [file];
    await selectedInput.events.change();
    assert.equal(keys[1], keys[0], 'retry must use the committed upload idempotency key');
    assert.equal(memory.size, 0, 'key must clear after acknowledged success');
    assert.equal((composer.value.match(/artifact:\/\//g) || []).length, 1);
    assert.equal(composer.value, 'What is this?\n\nAnalyze attached image artifact://art_' + 'a'.repeat(32) + ' (image.png).');

    selectedInput.files = [file];
    await selectedInput.events.change();
    assert.notEqual(keys[2], keys[1], 'new intentional upload gets a fresh identity');
  } finally {
    globalThis.document = oldDocument;
    globalThis.sessionStorage = oldStorage;
    globalThis.fetch = oldFetch;
    globalThis.Event = oldEvent;
  }
});


test('T12 binds video before conversion, blocks premature Send and retries failed original without duplicate caption', async () => {
  const previous = {
    document: globalThis.document,
    sessionStorage: globalThis.sessionStorage,
    fetch: globalThis.fetch,
    Event: globalThis.Event,
  };
  const memory = new Map();
  let chosen, unblockStatus;
  let signalStatus;
  const pendingStatus = new Promise(resolve => { signalStatus = resolve; });
  const originalID = 'art_' + 'b'.repeat(32);
  const originalRef = 'artifact://' + originalID;
  const calls = [];
  const form = {
    style: {}, listeners: {},
    querySelector: () => null, insertBefore() {}, append() {},
    addEventListener(kind, fn) { this.listeners[kind] = fn; }
  };
  const composer = {
    value: 'Explain the clip', listeners: {}, closest: () => form, focus() {},
    addEventListener(kind, fn) { this.listeners[kind] = fn; },
    dispatchEvent() {}
  };
  globalThis.document = {
    createElement(tag) {
      const item = {
        tag, dataset: {}, style: {}, files: [], value: '', hidden: false, disabled: false,
        setAttribute() {},
        append(child) { if (child.tag === 'input') chosen = child; },
        addEventListener(kind, fn) { this.listeners ??= {}; this.listeners[kind] = fn; },
      };
      return item;
    }
  };
  globalThis.sessionStorage = {
    getItem: key => memory.get(key) || null,
    setItem: (key, value) => memory.set(key, value),
    removeItem: key => memory.delete(key)
  };
  globalThis.Event = class { constructor(type) { this.type = type; } };
  let uploads = 0, retryRequests = 0, firstStatus = true;
  globalThis.fetch = async (url, options = {}) => {
    calls.push({ url, options });
    if (url.startsWith('/v1/artifacts?')) {
      uploads++;
      return {
        ok: true,
        async json() {
          return {
            reference: originalRef, duplicate: uploads > 1,
            artifact: { id: originalID, processing_status: uploads > 1 ? 'failed' : 'queued' }
          };
        }
      };
    }
    if (url.endsWith('/retry')) {
      retryRequests++;
      return { ok: true, status: 202 };
    }
    if (url.endsWith('/status')) {
      if (firstStatus) {
        firstStatus = false;
        signalStatus();
        return await new Promise(resolve => {
          unblockStatus = () => resolve({
            ok: true,
            async json() { return { processing_status: 'failed', processing_error: 'invalid clip' }; }
          });
        });
      }
      return { ok: true, async json() { return { processing_status: 'ready', video_frames: [{ artifact_id: 'art_' + 'c'.repeat(32) }] }; } };
    }
    throw new Error('unexpected URL ' + url);
  };
  try {
    installUpload(composer, true);
    const bytes = Uint8Array.of(0, 1, 2, 3, 4);
    const file = { name: 'clip.mp4', size: bytes.length, arrayBuffer: async () => bytes.buffer };
    chosen.files = [file];
    const first = chosen.listeners.change();
    const observed = { blocked: false };
    form.listeners.submit({
      preventDefault() { observed.blocked = true; },
      stopImmediatePropagation() {}
    });
    assert.equal(observed.blocked, true, 'Send before upload response must not omit attachment');
    await pendingStatus;
    assert.equal((composer.value.match(/artifact:\/\//g) || []).length, 1);
    assert.ok(composer.value.includes(originalRef), 'reference bound before async processing finishes');
    let prematureBlocked = false;
    form.listeners.submit({ preventDefault() { prematureBlocked = true; }, stopImmediatePropagation() {} });
    assert.equal(prematureBlocked, false, 'Send during conversion is allowed with durable reference');
    const capturedSentText = composer.value;
    assert.ok(capturedSentText.includes(originalRef));
    assert.equal(capturedSentText, 'Explain the clip\n\nAnalyze sampled frames of attached video ' + originalRef + ' (clip.mp4).');
    unblockStatus();
    await first;
    assert.equal(memory.size, 1, 'failed processing must retain upload identity for retry');

    chosen.files = [file];
    await chosen.listeners.change();
    assert.equal(retryRequests, 1, 'failed original must receive explicit retry');
    assert.equal(memory.size, 0, 'successful conversion clears retry identity');
    assert.equal((composer.value.match(/artifact:\/\//g) || []).length, 1, 'retry must not duplicate caption');
    const keys = calls.filter(x => x.url.startsWith('/v1/artifacts?')).map(x => x.options.headers['Idempotency-Key']);
    assert.equal(keys[0], keys[1], 'retry must identify exactly the original');
  } finally {
    Object.assign(globalThis, previous);
  }
});
