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

test('observer displays the complete sanitized activity payload', () => {
  const payload = formatActivityPayload({ kind: 'worker.activity', payload: { kind: 'tool_call', tool_call: { name: 'shell', arguments: { command: 'ls' } } } });
  assert.match(payload, /tool_call/);
  assert.match(payload, /shell/);
  assert.match(payload, /command/);
});

test('never exposes raw thinking as a Secretary stream label', () => {
  assert.equal(secretaryEventText({ kind: 'secretary.thinking_summary', summary: 'Checking the project.' }), 'Checking the project.');
  assert.equal(secretaryEventText({ kind: 'secretary.tool_call', tool: 'list_workers', arguments: '{}' }), 'list_workers');
  assert.equal(secretaryEventText({ kind: 'secretary.tool_result', tool: 'list_workers', result: 'ok' }), 'list_workers · ok');
});
