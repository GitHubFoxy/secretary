// The browser only renders these event shapes. Keep the full event log in SQLite and MCP.
export function chatHistory(events: any[]) {
  return events.flatMap((event): any[] => {
    if (event?.type === 'user' && (typeof event.message?.content === 'string' && event.message.content || event.images?.length)) return [{type:'user', message:{content:event.message?.content ?? ''}, images:event.images}];
    if (event?.type === 'result' && typeof event.result === 'string' && event.result && !event.result.startsWith('NO_REPLY')) return [{type:'result', result:event.result}];
    if (event?.type === 'error' && event.message) return [{type:'error', message:event.message}];
    if (event?.type === 'cancelled' && (event.text || event.images?.length)) return [{type:'cancelled', text:event.text, images:event.images}];
    if (event?.type === 'delivery_uncertain' && (event.text || event.images?.length)) return [{type:'delivery_uncertain', text:event.text, images:event.images}];
    if (event?.method === 'item/completed' && event?.params?.item?.type === 'agentMessage' && typeof event.params.item.text === 'string' && event.params.item.text && !event.params.item.text.startsWith('NO_REPLY'))
      return [{method:'item/completed', params:{item:{type:'agentMessage', text:event.params.item.text}}}];
    return [];
  });
}

// Scan backwards in small batches until ten displayable messages are found.
// The full history remains untouched for agents and later pagination.
export function recentChatEvents(db: any, sessionId: number, limit = 10) {
  const query = db.query('SELECT rowid, json FROM events WHERE session_id = ? AND rowid < ? ORDER BY rowid DESC LIMIT 200');
  const messages: any[] = [];
  let cursor = Number.MAX_SAFE_INTEGER;
  while (messages.length < limit) {
    const rows = query.all(sessionId, cursor) as {rowid:number, json:string}[];
    if (!rows.length) break;
    for (const row of rows) {
      cursor = row.rowid;
      try { messages.push(...chatHistory([JSON.parse(row.json)])); } catch { /* skip invalid historic event */ }
      if (messages.length >= limit) break;
    }
    if (rows.length < 200) break;
  }
  return messages.reverse();
}
