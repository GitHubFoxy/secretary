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

export function inputRequest(request) {
  if (!request || request.kind !== 'input' || !request.request_id) return null;
  return { requestId: request.request_id, prompt: request.action_summary || request.prompt || 'Input required' };
}

export function workerActionBody(text, requestId = '') {
  const body = { text };
  if (requestId) body.request_id = requestId;
  return body;
}

function resultIdentity(worker, status, result) {
  const value = result && typeof result === 'object' ? result : {};
  const summary = typeof result === 'string' ? result : value.summary || worker?.last_result_summary || '';
  return {
    resultId: value.id || value.result_id || worker?.result_id || worker?.last_result_id || '',
    workerId: value.worker_id || worker?.id || '',
    workerRef: value.worker_ref || worker?.worker_ref || '',
    turnId: value.turn_id || value.turnId || worker?.last_result_turn_id || worker?.result_turn_id || (typeof result === 'object' && summary ? worker?.current_turn_id : (summary && status === 'idle' ? worker?.current_turn_id : '')) || '',
    summary,
  };
}

function entryResultIdentity(entry) {
  const value = entry?.result && typeof entry.result === 'object' ? entry.result : {};
  return {
    resultId: entry?.result_id || entry?.resultId || value.id || value.result_id || '',
    workerId: entry?.worker_id || entry?.workerId || value.worker_id || value.workerId || '',
    workerRef: entry?.worker_ref || entry?.workerRef || value.worker_ref || value.workerRef || '',
    turnId: entry?.turn_id || entry?.turnId || value.turn_id || value.turnId || '',
    summary: entry?.body ?? value.summary ?? '',
  };
}

function cardResultIdentity(card) {
  if (card?.resultIdentity) return card.resultIdentity;
  const value = card?.result && typeof card.result === 'object' ? card.result : {};
  return {
    resultId: card?.result_id || card?.resultId || value.id || value.result_id || card?.worker?.result_id || card?.worker?.last_result_id || '',
    workerId: card?.worker_id || card?.workerId || value.worker_id || value.workerId || card?.worker?.id || '',
    workerRef: card?.worker_ref || card?.workerRef || value.worker_ref || value.workerRef || card?.worker?.worker_ref || '',
    turnId: card?.turn_id || card?.turnId || value.turn_id || value.turnId || card?.worker?.last_result_turn_id || card?.worker?.result_turn_id || card?.worker?.current_turn_id || '',
    summary: typeof card?.result === 'string' ? card.result : value.summary || '',
  };
}

function resultIsRepresented(entry, card) {
  const entryResult = entryResultIdentity(entry);
  const cardResult = cardResultIdentity(card);
  if (!cardResult.summary || !entryResult.summary || entryResult.summary !== cardResult.summary) return false;
  if (entryResult.resultId && cardResult.resultId) return entryResult.resultId === cardResult.resultId;
  const workerMatches = entryResult.workerRef && cardResult.workerRef
    ? entryResult.workerRef === cardResult.workerRef
    : entryResult.workerId && cardResult.workerId
      ? entryResult.workerId === cardResult.workerId
      : false;
  return Boolean(workerMatches && entryResult.turnId && cardResult.turnId && entryResult.turnId === cardResult.turnId);
}

export function visibleConversationEntries(entries = [], workerCards = []) {
  return entries.filter((entry) => entry?.kind !== 'worker_result' || !workerCards.some((card) => resultIsRepresented(entry, card)));
}

export function workerCard(worker, projects = [], nodes = []) {
  const project = projects.find((item) => item.id === worker?.project_id);
  const node = nodes.find((item) => item.node === worker?.node_id);
  const blocked = Boolean(node?.revoked || node?.draining || worker?.status === 'blocked');
  const offline = Boolean(worker?.status === 'offline' || (node && !node.online));
  const status = blocked ? 'blocked' : offline ? 'offline' : worker?.status;
  const instance = node?.inventory?.instances?.find((item) => item.id === worker?.harness_instance_id);
  const rawResult = worker?.result || worker?.last_result;
  const resultInfo = resultIdentity(worker, status, rawResult || worker?.last_result_summary || '');
  const result = resultInfo.summary;
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
    resultIdentity: resultInfo.summary ? resultInfo : null,
    hasTerminalResult: Boolean(result),
    bindingImmutable: true,
  };
}
