/** @typedef {{seq?: number, id?: string}} Sequenced */

export const STATUS_LABELS = {
  saved: 'Saved',
  accepted: 'Accepted',
  queued: 'Queued',
  starting: 'Working',
  active: 'Working',
  working: 'Working',
  waiting_approval: 'Waiting for approval',
  waitingApproval: 'Waiting for approval',
  needs_input: 'Needs input',
  needsInput: 'Needs input',
  succeeded: 'Succeeded',
  failed: 'Failed',
  canceled: 'Canceled',
  interrupted: 'Interrupted',
  offline: 'Offline',
  blocked: 'Blocked',
  idle: 'Saved',
  closed: 'Canceled',
};

export function formatWorkerStatus(status = '') {
  return STATUS_LABELS[status] || status.replaceAll('_', ' ') || 'Unknown';
}

export function mergeSequenced(current = [], incoming = []) {
  const result = [];
  const ids = new Set();
  const sequences = new Set();
  for (const item of [...current, ...incoming]) {
    const id = item?.id;
    const seq = item?.seq;
    if ((id && ids.has(id)) || (Number.isFinite(seq) && sequences.has(seq))) continue;
    if (id) ids.add(id);
    if (Number.isFinite(seq)) sequences.add(seq);
    result.push(item);
  }
  return result.sort((left, right) => (left.seq ?? 0) - (right.seq ?? 0));
}

function eventPayload(event) {
  if (!event?.payload) return event || {};
  if (typeof event.payload === 'object') return { ...event, ...event.payload };
  try { return { ...event, ...JSON.parse(event.payload) }; } catch (_) { return event; }
}

export function secretaryEventText(event) {
  const value = eventPayload(event);
  switch (value.kind) {
    case 'secretary.text_delta': return value.text || value.delta || '';
    case 'secretary.thinking_summary': return value.summary || value.text || '';
    case 'secretary.tool_call': {
      const tool = value.tool || value.name || 'Secretary tool';
      const args = value.arguments && value.arguments !== '{}' ? ` · ${typeof value.arguments === 'string' ? value.arguments : JSON.stringify(value.arguments)}` : '';
      return `${tool}${args}`;
    }
    case 'secretary.tool_result': {
      const tool = value.tool || value.name || 'Secretary tool';
      const result = value.result || value.text || value.status || '';
      const rendered = typeof result === 'string' ? result : JSON.stringify(result);
      return rendered ? `${tool} · ${rendered}` : tool;
    }
    case 'secretary.turn.queued': return 'Secretary turn queued';
    case 'secretary.turn.started': return 'Secretary turn started';
    case 'secretary.turn.finished': return value.error || value.status || 'Secretary turn finished';
    default: return value.text || value.summary || value.status || '';
  }
}

export function formatActivityPayload(event) {
  const payload = event?.payload;
  let value = payload;
  if (typeof payload === 'string') {
    try { value = JSON.parse(payload); } catch (_) { value = payload; }
  }
  if (value === undefined) value = event || {};
  return typeof value === 'string' ? value : JSON.stringify(value, null, 2);
}

export function visibleConversationEntries(entries = [], workerCards = []) {
  const renderedResults = new Set(workerCards.map((card) => card?.result).filter(Boolean));
  return entries.filter((entry) => entry?.kind !== 'worker_result' || !renderedResults.has(entry.body));
}

export function workerCard(worker, projects = [], nodes = []) {
  const project = projects.find((item) => item.id === worker?.project_id);
  const node = nodes.find((item) => item.node === worker?.node_id);
  const blocked = Boolean(node?.revoked || node?.draining || worker?.status === 'blocked');
  const offline = Boolean(worker?.status === 'offline' || (node && !node.online));
  const status = blocked ? 'blocked' : offline ? 'offline' : worker?.status;
  const instance = node?.inventory?.instances?.find((item) => item.id === worker?.harness_instance_id);
  const result = worker?.result?.summary || worker?.last_result_summary || '';
  const turnStatus = worker?.turn_status || worker?.current_turn_status || (result && status === 'idle' ? 'succeeded' : status);
  return {
    worker,
    project: project?.name || worker?.project_id || 'Unknown Project',
    node: worker?.node_id || 'Unbound Node',
    harness: worker?.harness_instance_id || instance?.id || 'Unknown HarnessInstance',
    nodeOnline: node?.online !== false,
    nodeState: blocked ? 'blocked' : offline ? 'offline' : 'online',
    status,
    statusLabel: formatWorkerStatus(status),
    turnStatus,
    turnStatusLabel: formatWorkerStatus(turnStatus),
    acknowledgement: worker?.acknowledgement || (worker?.worker_ref ? 'Accepted' : ''),
    result,
    hasTerminalResult: Boolean(result),
    bindingImmutable: true,
  };
}
