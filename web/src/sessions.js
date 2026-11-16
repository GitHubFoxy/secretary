// Secretary's internal agent threads live in the .secretary directory.
export function showInSidebar(session) {
  if (session.id === 1) return false;
  if (typeof session.folder !== 'string') return true;
  const parts = session.folder.split('/').filter(Boolean);
  return !parts.includes('.secretary');
}
