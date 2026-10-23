import test from 'node:test';
import assert from 'node:assert/strict';
import { transitionLoad } from './load-state.js';

test('load lifecycle permits successful and failed requests followed by retry', () => {
  assert.equal(transitionLoad('loading', 'resolved'), 'ready');
  assert.equal(transitionLoad('loading', 'rejected'), 'error');
  assert.equal(transitionLoad('ready', 'start'), 'loading');
  assert.equal(transitionLoad('error', 'start'), 'loading');
});

test('load lifecycle rejects impossible transitions', () => {
  assert.throws(() => transitionLoad('ready', 'resolved'), /Invalid load transition/);
  assert.throws(() => transitionLoad('error', 'resolved'), /Invalid load transition/);
  assert.throws(() => transitionLoad('loading', 'start'), /Invalid load transition/);
});
