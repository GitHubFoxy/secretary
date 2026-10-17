import assert from 'node:assert/strict';
import { test } from 'node:test';
import { createSessionRecovery, isTransientSessionFailure } from './session-recovery.js';

test('startup network failure -> server restored -> existing owner cookie session and Web state recover', async () => {
  let online = false;
  const calls = [];
  const ui = { loading: true, authenticated: false, conversation: [], workers: [] };
  const recovery = createSessionRecovery({
    async restore(token) {
      ui.loading = true;
      assert.equal(token, '', 'recovery must not perform a bootstrap exchange');
      calls.push('GET /v1/web/session');
      if (!online) throw new TypeError('Failed to fetch');
      // Existing cookie is included by the Svelte request helper's same-origin credentials.
      calls.push('GET /v1/conversation?after_seq=0');
      calls.push('GET /v1/workers');
      ui.conversation = [{ body: 'existing Personal Conversation' }];
      ui.workers = [{ worker_ref: 'w1', title: 'Existing Worker' }];
      ui.authenticated = true;
      ui.loading = false;
    },
    onFailure() {
      ui.loading = false;
      ui.authenticated = false;
    },
  });

  assert.equal(await recovery.attempt(), false);
  assert.equal(ui.authenticated, false);
  assert.equal(ui.loading, false);
  assert.equal(await recovery.onServerAvailable(), false, 'still-offline retry stays recoverable');

  online = true;
  assert.equal(await recovery.onServerAvailable(), true);
  assert.equal(ui.authenticated, true);
  assert.deepEqual(ui.conversation, [{ body: 'existing Personal Conversation' }]);
  assert.equal(ui.workers[0].worker_ref, 'w1');
  assert.equal(calls.filter(c => c === 'GET /v1/web/session').length, 3);
  assert.ok(calls.every(c => !c.startsWith('POST')), 'no new login or token exchange');
  assert.equal(await recovery.onServerAvailable(), false, 'successful recovery is not repeated');
});

test('server-available notifications coalesce inflight cookie recovery requests', async () => {
  let release;
  let attempts = 0;
  const recovery = createSessionRecovery({
    async restore() {
      attempts++;
      if (attempts === 1) throw new TypeError('network failed');
      await new Promise(resolve => { release = resolve; });
    },
    onFailure() {},
  });
  assert.equal(await recovery.attempt(), false);
  const first = recovery.onServerAvailable();
  const second = recovery.onServerAvailable();
  assert.equal(attempts, 2);
  assert.equal(await second, false);
  release();
  assert.equal(await first, true);
  assert.equal(attempts, 2);
});

test('unauthorized owner session never auto-retries on health success', async () => {
  let attempts = 0;
  const recovery = createSessionRecovery({
    async restore() {
      attempts++;
      throw Object.assign(new Error('not authorized'), { status: 401 });
    },
    onFailure() {},
  });
  assert.equal(await recovery.attempt(), false);
  assert.equal(await recovery.onServerAvailable(), false);
  assert.equal(attempts, 1);
  assert.equal(isTransientSessionFailure({ status: 401 }), false);
  assert.equal(isTransientSessionFailure({ status: 503 }), true);
  assert.equal(isTransientSessionFailure(new TypeError('offline')), true);
});

test('failed bootstrap exchange only reuses the existing cookie on subsequent recovery', async () => {
  const calls = [];
  const recovery = createSessionRecovery({
    async restore(token) {
      calls.push(token ? 'POST bootstrap' : 'GET cookie');
      if (calls.length === 1) throw new TypeError('offline');
    },
    onFailure() {},
  });
  assert.equal(await recovery.attempt('existing-bootstrap-fragment'), false);
  assert.equal(await recovery.onServerAvailable(), true);
  assert.deepEqual(calls, ['POST bootstrap', 'GET cookie']);
});
