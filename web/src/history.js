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
