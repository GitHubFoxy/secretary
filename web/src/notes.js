// Чистая логика заметок без Svelte: заголовки, подзаголовки, область видимости.
// Используется в Notes.svelte и покрыта тестами в notes.test.js.

export function noteTitle(note) {
  const title = note.title?.trim();
  if (title) return title;
  return note.text.split('\n')[0]?.trim() || 'Новая заметка';
}

export function notePreview(note) {
  const lines = note.text.split('\n');
  if (note.title?.trim()) return lines.join(' ').trim();
  return lines.slice(1).join(' ').trim();
}

// Заметка, открытая в отдельном окне поверх списка; null — открыт только список.
export function openedNote(notes, openedId) {
  if (!openedId) return null;
  return notes.find(note => note.id === openedId) ?? null;
}

// Когда выбран Secretary Agent, видны заметки всех Session,
// поэтому список перезагружается при изменении заметок любой Session.
export function shouldReloadNotes(message, selectedId, secretaryView) {
  return message?.type === 'notesChanged' && (secretaryView || message.sessionId === selectedId);
}

// Сохранение и удаление идут в Session-владелицу, а не в выбранную.
export function noteSessionId(note, fallbackId) {
  return note?.session_id ?? fallbackId;
}
