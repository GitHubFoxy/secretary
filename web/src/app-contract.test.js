import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';
import { compile } from 'svelte/compiler';

const source = fs.readFileSync(new URL('./App.svelte', import.meta.url), 'utf8');

test('Svelte app compiles and keeps the Worker-first API seams', () => {
  const compiled = compile(source, { generate: 'client' });
  assert.ok(compiled.js?.code);
  for (const endpoint of ['/v1/user', '/v1/messages', '/v1/workers/', '/activity', '/v1/secretary/turns/']) {
    assert.match(source, new RegExp(endpoint.replaceAll('/', '\\/')));
  }
  assert.match(source, /Worker compact item/);
  assert.match(source, /Terminal Result/);
  assert.match(source, /Shift\+Enter/);
  assert.match(source, /key !== 'Enter'|event\.key !== 'Enter'/);
});
