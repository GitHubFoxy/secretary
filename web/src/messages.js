export function visibleMessage(entry) {
  return Boolean((entry?.text || entry?.images?.length) && (entry.role !== 'Агент' || !entry.text.startsWith('NO_REPLY')));
}

// Сообщение из очереди: уже отправлено пользователем, но агент ещё его не принял.
export function queuedMessage(item) {
  return {role:'В очереди', text:item.text, images:item.images || [], urgent:Boolean(item.urgent)};
}

export function keyedMessages(events, queue, sessionId, text) {
  return [
    ...events.map((event, index) => ({ message: text(event), key: `${sessionId}:event:${index}` }))
      .filter(item => visibleMessage(item.message))
      .map(({message, key}) => ({ ...message, key })),
    ...queue.map(item => ({ ...queuedMessage(item), key: `${sessionId}:queue:${item.id}` })),
  ];
}

export function userMessage(text) {
  const match = /^\[report #(\d+) (\S+) ([^\]\n]+)\] ([\s\S]*)$/.exec(text);
  if (!match) return {role:'Вы', text};
  return {role:'Отчёт', id:Number(match[1]), harness:match[2], folder:match[3], text:match[4]};
}
