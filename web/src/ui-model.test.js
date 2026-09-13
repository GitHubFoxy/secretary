import test from 'node:test';
import assert from 'node:assert/strict';
import {
  formatWorkerStatus,
  mergeSequenced,
  secretaryEventText,
  workerCard,
  formatActivityPayload,
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
