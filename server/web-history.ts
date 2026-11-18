// The browser only renders these event shapes. Keep the full event log in SQLite and MCP.
export function chatHistory(events: any[]) {
  return events.flatMap((event): any[] => {
    if (event?.type === 'user') return [{type:'user', message:{content:event.message?.content}, images:event.images}];
    if (event?.type === 'result') return [{type:'result', result:event.result}];
    if (event?.type === 'error') return [{type:'error', message:event.message}];
    if (event?.type === 'cancelled') return [{type:'cancelled', text:event.text, images:event.images}];
    if (event?.method === 'item/completed' && event?.params?.item?.type === 'agentMessage')
      return [{method:'item/completed', params:{item:{type:'agentMessage', text:event.params.item.text}}}];
    return [];
  });
}
