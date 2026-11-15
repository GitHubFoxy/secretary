export function visibleMessage(entry) {
  return Boolean(entry?.text && (entry.role !== 'Агент' || !entry.text.startsWith('NO_REPLY')));
}
