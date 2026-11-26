export function activityStatus(status, history) {
  if (status !== 'running') return status;
  const item = history.at(-1);
  if (item?.kind === 'writing' || item?.kind === 'agent') return 'Пишет ответ…';
  if (item?.kind === 'thinking') return 'Думает…';
  if (item?.kind === 'step') {
    const summary = item.summary?.replace(/\s+/g, ' ').trim();
    if (item.tool === 'Read') return `Читает ${summary || 'файл'}`;
    return [item.tool, summary].filter(Boolean).join(': ');
  }
  return 'Думает…';
}
