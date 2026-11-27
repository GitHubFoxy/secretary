export function mergeHistory(current, page, lastCursor) {
  const overlap = lastCursor !== undefined && page.some(message => message.cursor === lastCursor);
  const messages = overlap ? current.slice() : page.slice();
  const known = new Set(messages.map(message => message.cursor));
  const additional = overlap ? page : current.filter(message => message.cursor > (page.at(-1)?.cursor ?? 0));
  for (const message of additional) {
    if (!known.has(message.cursor)) messages.push(message);
  }
  messages.sort((a, b) => a.cursor - b.cursor);
  return {messages, overlap};
}

export function insertVoices(history, voices) {
  const messages = history.slice();
  for (const voice of voices) {
    const index = messages.findIndex(message => message.cursor > voice.afterCursor);
    messages.splice(index < 0 ? messages.length : index, 0, voice);
  }
  return messages;
}

export function mergeApprovals(history) {
  const kinds = ['sudo', 'skill', 'secret'];
  return history.filter(m => !kinds.includes(m.kind) || !m.status).map(m => kinds.includes(m.kind)
    ? {...m, status: history.findLast(e => e.kind === m.kind && e.id === m.id && e.status)?.status} : m);
}
