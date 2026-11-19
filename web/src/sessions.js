// Сессии из папки .secretary (включая вложенные) принадлежат самому Secretary.
// Их не показываем в боковой панели ни в основном списке, ни среди скрытых.
export function isSecretaryFolder(folder) {
  if (typeof folder !== 'string') return false;
  return folder.split('/').filter(Boolean).includes('.secretary');
}

export function showInSidebar(session) {
  if (session.id === 1) return false;
  return !isSecretaryFolder(session.folder);
}
