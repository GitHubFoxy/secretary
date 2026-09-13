import test from 'node:test';
import assert from 'node:assert/strict';
import {
  formatWorkerStatus,
  mergeSequenced,
  secretaryEventText,
  workerCard,
} from './ui-model.js';

test('renders every public Worker and Turn status without legacy Task fields', () => {
  const statuses = ['saved', 'accepted', 'queued', 'working', 'waiting_approval', 'needs_input', 'succeeded', 'failed', 'canceled', 'interrupted', 'offline', 'blocked'];
  assert.deepEqual(statuses.map(formatWorkerStatus), [
    'Saved', 'Accepted', 'Queued', 'Working', 'Waiting approval', 'Needs input',
    'Succeeded', 'Failed', 'Canceled', 'Interrupted', 'Offline', 'Blocked',
  ]);
  const card = workerCard({
    worker_ref: 'wkr_1', title: 'Fix header', project_id: 'project_web', node_id: 'macbook',
    harness_instance_id: 'macbook/claude', status: 'offline',
  }, [{ id: 'p', name: 'Web' }], [{ node: 'macbook', online: false, inventory: { instances: [{ id: 'macbook/claude', harness: 'claude' }] } }]);
  assert.equal(card.project, 'Web');
  assert.equal(card.node, 'macbook');
  assert.equal(card.harness, 'macbook/claude');
  assert.equal(card.statusLabel, 'Offline');
  assert.equal('task' in card, false);
  assert.equal('task_id' in card, false);
});

test('merges replay and live sequences idempotently', () => {
  const initial = [{ seq: 2, id: 'b' }, { seq: 1, id: 'a' }];
  const merged = mergeSequenced(initial, [{ seq: 2, id: 'b' }, { seq: 3, id: 'c' }, { seq: 3, id: 'duplicate' }]);
  assert.deepEqual(merged.map((item) => item.id), ['a', 'b', 'c']);
});

test('never exposes raw thinking as a Secretary stream label', () => {
  assert.equal(secretaryEventText({ kind: 'secretary.thinking_summary', summary: 'Checking the project.' }), 'Checking the project.');
  assert.equal(secretaryEventText({ kind: 'secretary.tool_call', tool: 'list_workers', arguments: '{}' }), 'list_workers');
  assert.equal(secretaryEventText({ kind: 'secretary.tool_result', tool: 'list_workers', result: 'ok' }), 'list_workers · ok');
});
