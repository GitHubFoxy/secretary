import assert from 'node:assert/strict';
import { readFileSync } from 'node:fs';
import { test } from 'node:test';
import { createSessionRecovery } from './session-recovery.js';

const src = (path) => readFileSync(new URL(path, import.meta.url), 'utf8');
const manifest = JSON.parse(src('../public/manifest.webmanifest'));

test('PWA installs the existing Web origin and launch route', () => {
  assert.equal(manifest.id, '/');
  assert.equal(manifest.start_url, '/');
  assert.equal(manifest.scope, '/');
  assert.equal(manifest.display, 'standalone');
  assert.ok(manifest.name && manifest.short_name);
  const sizes = new Set(manifest.icons.map((item) => item.sizes));
  assert.ok(sizes.has('192x192') && sizes.has('512x512'));
  for (const icon of manifest.icons) {
    const png = readFileSync(new URL('../public' + icon.src, import.meta.url));
    assert.equal(png.subarray(0, 8).toString('hex'), '89504e470d0a1a0a');
    assert.equal(png.readUInt32BE(16), Number(icon.sizes.split('x')[0]));
    assert.equal(png.readUInt32BE(20), Number(icon.sizes.split('x')[1]));
  }
  for (const html of [src('../index.src.html'), src('../index.html')]) {
    assert.match(html, /href="\/manifest\.webmanifest"/);
    assert.match(html, /src="\/pwa\.js"/);
    assert.match(html, /rel="apple-touch-icon"/);
  }
});

test('PWA keeps existing owner credentials and shows connection failures, without offline caching or push', () => {
  const app = src('./App.svelte');
  const pwa = src('../public/pwa.js');
  const sw = src('../public/sw.js');
  assert.match(app, /request\('\/v1\/web\/session'/);
  assert.match(app, /credentials: 'same-origin'/);
  assert.match(app, /function openWorker\(workerRef\)/);
  assert.match(pwa, /navigator\.serviceWorker\.register\('\/sw\.js'/);
  assert.match(pwa, /fetch\('\/v1\/health'/);
  assert.match(pwa, /role', 'alert'/);
  assert.match(pwa, /You are offline/);
  assert.match(pwa, /Secretary Server is unreachable/);
  assert.match(pwa, /secretary:server-available/);
  assert.match(app, /window.addEventListener\('secretary:server-available', recoverCookieSession\)/);
  assert.match(app, /sessionRecovery.onServerAvailable\(\)/);
  assert.match(app, /createSessionRecovery\(/);
  assert.equal(typeof createSessionRecovery, 'function');
  assert.match(sw, /fetch\(event\.request\)/);
  assert.doesNotMatch(sw, /caches\.|\.cache\.put|push/);
});
