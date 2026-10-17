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
      async json() { return { reference: 'artifact://art_' + 'a'.repeat(32), duplicate: keys.length === 2 }; }
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
