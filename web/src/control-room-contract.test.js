import test from 'node:test';
import assert from 'node:assert/strict';
import fs from 'node:fs';

const source = fs.readFileSync(new URL('../control-room/src/App.svelte', import.meta.url), 'utf8');

test('Ticket 11 Control Room stays a debug-only diagnostics surface', () => {
  for (const endpoint of ['/v1/control/overview', '/v1/control/nodes', '/v1/control/workers', '/v1/control/clients', '/v1/control/projects', '/v1/control/diagnostics/']) {
    assert.match(source, new RegExp(endpoint.replaceAll('/', '\\/')));
  }
  assert.match(source, /Debug-only|debug-only|Debug mode only/);
  assert.match(source, /recursive|redact|redacted/i);
  assert.doesNotMatch(source, /runtime_session_id|sessionId|callback_capability|credential_secret|chain.of.thought/i);
  assert.doesNotMatch(source, /\/v1\/messages|\/v1\/secretary\/model/);
});
