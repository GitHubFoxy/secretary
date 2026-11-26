import type { Database } from 'bun:sqlite';

export function listNotes(db: Database, sessionId: number) {
  return db.query('SELECT id, text, updated_at FROM notes WHERE session_id = ? ORDER BY updated_at DESC').all(sessionId);
}

export function saveNote(db: Database, sessionId: number, id: unknown, text: unknown) {
  if (typeof id !== 'string' || typeof text !== 'string') throw new Error('Некорректная заметка');
  const result = db.query('INSERT INTO notes VALUES (?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET text = excluded.text, updated_at = excluded.updated_at WHERE session_id = excluded.session_id').run(id, sessionId, text, new Date().toISOString());
  if (!result.changes) throw new Error('Заметка принадлежит другой сессии');
}

export function deleteNote(db: Database, sessionId: number, id: unknown) {
  if (typeof id !== 'string') throw new Error('Некорректная заметка');
  db.query('DELETE FROM notes WHERE id = ? AND session_id = ?').run(id, sessionId);
}
