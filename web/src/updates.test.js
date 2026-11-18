import { expect, test } from 'bun:test';
import { createUpdateChecker } from './updates.js';

test('reloads only for a new build and waits for a draft to be safe', async () => {
  let version = 'old', busy = false, reloads = 0;
  const checker = createUpdateChecker({currentVersion:'old', fetchVersion:async () => version, isBusy:() => busy, reload:() => reloads++});
  await checker.check(); expect(reloads).toBe(0);
  busy = true; version = 'new'; await checker.check(); expect(checker.available).toBe(true); expect(reloads).toBe(0);
  busy = false; await checker.check(); await checker.check(); expect(reloads).toBe(1);
});

test('network failures and invalid version do not reload; checks recover', async () => {
  let value = null, reloads = 0;
  const checker = createUpdateChecker({currentVersion:'a', fetchVersion:async () => { if (value === null) throw Error('offline'); return value; }, isBusy:() => false, reload:() => reloads++});
  await checker.check(); value = ''; await checker.check(); expect(reloads).toBe(0);
  value = 'b'; await checker.check(); expect(reloads).toBe(1);
});

test('does not reload after checker is stopped during a request', async () => {
  let resolve, reloads = 0;
  const checker = createUpdateChecker({currentVersion:'a', fetchVersion:() => new Promise(r => resolve = r), isBusy:() => false, reload:() => reloads++});
  const pending = checker.check(); checker.stop(); resolve('b'); await pending; expect(reloads).toBe(0);
});
