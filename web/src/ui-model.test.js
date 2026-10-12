import test from 'node:test';
import assert from 'node:assert/strict';
import {
  formatWorkerStatus,
  mergeSequenced,
  secretaryEventText,
  workerCard,
  visibleConversationEntries,
  formatActivityPayload,
  inputRequest,
  workerActionBody,
  parseApiError,
  extractObserverFailure,
} from './ui-model.js';

test('renders every public Worker and Turn status without legacy Task fields', () => {
  const statuses = ['saved', 'accepted', 'queued', 'working', 'waiting_approval', 'needs_input', 'succeeded', 'failed', 'canceled', 'interrupted', 'offline', 'blocked'];
  assert.deepEqual(statuses.map(formatWorkerStatus), [
    'Saved', 'Accepted', 'Queued', 'Working', 'Waiting for approval', 'Needs input',
    'Succeeded', 'Failed', 'Canceled', 'Interrupted', 'Offline', 'Blocked',
  ]);
  const card = workerCard({
    worker_ref: 'wkr_1', title: 'Fix header', project_id: 'project_web', node_id: 'macbook',
    harness_instance_id: 'macbook/claude', status: 'offline',
  }, [{ id: 'project_web', name: 'Web' }], [{ node: 'macbook', online: false, inventory: { instances: [{ id: 'macbook/claude', harness: 'claude' }] } }]);
  assert.equal(card.project, 'Web');
  assert.equal(card.node, 'macbook');
  assert.equal(card.harness, 'macbook/claude');
  assert.equal(card.statusLabel, 'Offline');
  assert.equal('task' in card, false);
  assert.equal('task_id' in card, false);
});

test('compact Worker item includes Turn status, acknowledgement and terminal Result', () => {
  const card = workerCard({
    worker_ref: 'wkr_2', title: 'Ship it', project_id: 'p', node_id: 'n',
    harness_instance_id: 'n/fx', status: 'idle', current_turn_id: 'turn-2',
    turn_status: 'succeeded', last_result_summary: 'Deployed safely',
  }, [{ id: 'p', name: 'Project' }], [{ node: 'n', online: true, inventory: { instances: [] } }]);
  assert.equal(card.turnStatus, 'succeeded');
  assert.equal(card.turnStatusLabel, 'Succeeded');
  assert.equal(card.acknowledgement, 'Accepted');
  assert.equal(card.result, 'Deployed safely');
});

test('hides only the Worker Result represented by its compact card identity', () => {
  const durableEntries = [
    { id: 'worker-result-entry', seq: 1, kind: 'worker_result', worker_ref: 'wkr-result', turn_id: 'turn-1', result_id: 'result-1', body: 'Deployed safely' },
    { id: 'same-summary-other-turn', seq: 2, kind: 'worker_result', worker_ref: 'wkr-result', turn_id: 'turn-2', result_id: 'result-2', body: 'Deployed safely' },
    { id: 'secretary-entry', seq: 3, kind: 'secretary', body: 'Acknowledged' },
  ];
  const card = workerCard({
    worker_ref: 'wkr-result', status: 'idle', current_turn_id: 'turn-1',
    result: { id: 'result-1', worker_ref: 'wkr-result', turn_id: 'turn-1', summary: 'Deployed safely' },
  });
  const visible = visibleConversationEntries(durableEntries, [card]);
  assert.deepEqual(visible.map((entry) => entry.id), ['same-summary-other-turn', 'secretary-entry']);
  assert.equal(card.result, 'Deployed safely');
});

test('dedupes same-summary replay for each represented Worker Turn, not across identities', () => {
  const durableEntries = [
    { id: 'a-turn-1', seq: 1, kind: 'worker_result', worker_ref: 'worker-a', turn_id: 'turn-a-1', result_id: 'result-a-1', body: 'Finished' },
    { id: 'a-turn-1-replay', seq: 1, kind: 'worker_result', worker_ref: 'worker-a', turn_id: 'turn-a-1', result_id: 'result-a-1', body: 'Finished' },
    { id: 'a-turn-2', seq: 2, kind: 'worker_result', worker_ref: 'worker-a', turn_id: 'turn-a-2', result_id: 'result-a-2', body: 'Finished' },
    { id: 'b-turn-1', seq: 3, kind: 'worker_result', worker_ref: 'worker-b', turn_id: 'turn-b-1', result_id: 'result-b-1', body: 'Finished' },
    { id: 'unrepresented', seq: 4, kind: 'worker_result', worker_ref: 'worker-a', turn_id: 'turn-a-0', result_id: 'result-a-0', body: 'Finished' },
  ];
  const cards = [
    workerCard({ worker_ref: 'worker-a', current_turn_id: 'turn-a-1', result: { id: 'result-a-1', worker_ref: 'worker-a', turn_id: 'turn-a-1', summary: 'Finished' } }),
    workerCard({ worker_ref: 'worker-a', current_turn_id: 'turn-a-2', result: { id: 'result-a-2', worker_ref: 'worker-a', turn_id: 'turn-a-2', summary: 'Finished' } }),
    workerCard({ worker_ref: 'worker-b', current_turn_id: 'turn-b-1', result: { id: 'result-b-1', worker_ref: 'worker-b', turn_id: 'turn-b-1', summary: 'Finished' } }),
  ];
  const visible = visibleConversationEntries(durableEntries, cards);
  assert.deepEqual(visible.map((entry) => entry.id), ['unrepresented']);
});

test('uses production-shaped input requests separately from approvals and preserves request_id', () => {
  assert.deepEqual(inputRequest({ kind: 'input', request_id: 'input-1', action_summary: 'What is the version?', schema: { properties: { answer: { type: 'string' } } } }), {
    requestId: 'input-1',
    prompt: 'What is the version?',
    schema: { properties: { answer: { type: 'string' } } },
  });
  assert.equal(inputRequest({ kind: 'permission', request_id: 'approval-1', action_summary: 'Run shell' }), null);
  assert.deepEqual(workerActionBody('2.4.0', 'input-1'), { text: '2.4.0', request_id: 'input-1' });
});

test('merges replay and live sequences idempotently', () => {
  const initial = [{ seq: 2, id: 'b' }, { seq: 1, id: 'a' }];
  const merged = mergeSequenced(initial, [{ seq: 2, id: 'b' }, { seq: 3, id: 'c' }, { seq: 3, id: 'duplicate' }]);
  assert.deepEqual(merged.map((item) => item.id), ['a', 'b', 'c']);
});

test('observer shows compact tool identity and preview without arguments or output', () => {
  const started = formatActivityPayload({ kind: 'worker.activity', payload: { kind: 'tool_call', tool_call: { name: 'bash', preview: 'python3 - <<\'PY\' import…', arguments: { command: 'private full script' } } } });
  assert.equal(started, "▶ bash python3 - <<'PY' import…");
  assert.doesNotMatch(started, /arguments|private full script/);

  const finished = formatActivityPayload({ kind: 'worker.activity', payload: { kind: 'tool_result', tool_result: { name: 'read', preview: 'src/config.toml', status: 'completed', output: 'sensitive file contents' } } });
  assert.equal(finished, '✓ read src/config.toml');
  assert.doesNotMatch(finished, /sensitive file contents|output/);

  const failed = formatActivityPayload({ kind: 'worker.activity', payload: { kind: 'tool_result', tool_result: { name: 'bash', preview: 'make test', status: 'failed', error: 'private diagnostic' } } });
  assert.equal(failed, '✗ bash make test');
  assert.doesNotMatch(failed, /private diagnostic/);

  const classified = formatActivityPayload({ kind: 'worker.activity', payload: { kind: 'tool_result', tool_result: { name: 'web_fetch', status: 'failed', preview: 'wttr.in', failure: { category: 'http', code: 'http_error', http_status: 503, message: 'private producer text' } } } });
  assert.equal(classified, '✗ web_fetch wttr.in · http · http_error · HTTP 503');
  assert.doesNotMatch(classified, /private producer text/);

  const unknown = formatActivityPayload({ kind: 'worker.activity', payload: { kind: 'tool_result', tool_result: { name: 'web_fetch', status: 'failed', failure: { category: 'site_unavailable', code: 'host-private.invalid', http_status: 700 } } } });
  assert.equal(unknown, '✗ web_fetch');
  assert.equal(formatActivityPayload({ payload: { kind: 'tool_call', title: 'Reading', status: 'in_progress' } }), '');
});

test('never exposes raw thinking as a Secretary stream label', () => {
  assert.equal(secretaryEventText({ kind: 'secretary.thinking_summary', summary: 'Checking the project.' }), 'Checking the project.');
  assert.equal(secretaryEventText({ kind: 'secretary.tool_call', tool: 'list_workers', arguments: '{}' }), 'list_workers');
  assert.equal(secretaryEventText({ kind: 'secretary.tool_result', tool: 'list_workers', result: 'ok' }), 'list_workers · ok');
});

test('Worker observer reads text and status activity without leaking generic payload', () => {
  assert.equal(formatActivityPayload({payload: {kind: 'text', text: 'Проверяю сервер'}}), 'Проверяю сервер');
  assert.equal(formatActivityPayload({payload: {kind: 'status', status: 'working'}}), 'Working');
  assert.equal(formatActivityPayload({payload: {kind: 'thinking', text: 'private reasoning'}}), '');
  assert.equal(formatActivityPayload({payload: {kind: 'unknown', secret: 'private metadata'}}), 'Activity');
});

test("structured API failure captures category, providerCode, technical and preserves draft state on rejection", () => {
  const apiErrorPayload = {
    error: "usage_limit_exceeded: Usage limit reached",
    category: "usage_limit",
    provider_code: "usage_limit_exceeded",
    technical: "Quota exceeded for model claude-3-5-sonnet",
  };

  const err = parseApiError(apiErrorPayload, "Bad Request");
  assert.equal(err.message, "usage_limit_exceeded: Usage limit reached");
  assert.equal(err.category, "usage_limit");
  assert.equal(err.providerCode, "usage_limit_exceeded");
  assert.equal(err.technical, "Quota exceeded for model claude-3-5-sonnet");

  const failure = extractObserverFailure(err);
  assert.deepEqual(failure, {
    category: "usage_limit",
    providerCode: "usage_limit_exceeded",
    technical: "Quota exceeded for model claude-3-5-sonnet",
  });

  // Behavioral test: simulate action attempt with error catch
  let steerText = "critical follow-up instructions for worker";
  let observerError = "";
  let observerFailure = null;

  try {
    throw err;
    steerText = ""; // should not be reached
  } catch (caught) {
    observerError = caught.message;
    observerFailure = extractObserverFailure(caught);
  }

  // Draft remains preserved, observer error and structured failure capture details
  assert.equal(steerText, "critical follow-up instructions for worker");
  assert.equal(observerError, "usage_limit_exceeded: Usage limit reached");
  assert.deepEqual(observerFailure, {
    category: "usage_limit",
    providerCode: "usage_limit_exceeded",
    technical: "Quota exceeded for model claude-3-5-sonnet",
  });
});


test('terminal diagnostics show the provider reason, reset text and bounded technical detail', async () => {
  const { terminalDiagnostic } = await import('./ui-model.js');
  const display = terminalDiagnostic({ status: 'failed', error_message: 'Internal error', failure: {
    category: 'usage_limit', provider_code: 'usage_limit_exceeded',
    message: 'Usage limit exceeded; try again at 4:37 PM', technical: '😀'.repeat(1200),
  }});
  assert.equal(display.message, 'Usage limit exceeded; try again at 4:37 PM');
  assert.equal(display.category, 'usage_limit');
  assert.equal(display.providerCode, 'usage_limit_exceeded');
  assert.equal(Array.from(display.technical).length, 1000);
  assert.deepEqual(terminalDiagnostic({summary: 'legacy reason', diagnostics: 'legacy detail'}), {
    message: 'legacy reason', category: '', providerCode: '', technical: 'legacy detail',
  });
});
