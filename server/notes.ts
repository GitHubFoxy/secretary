import type { Database } from 'bun:sqlite';

export function listNotes(db: Database, sessionId: number) {
  return db.query('SELECT id, text, title, updated_at FROM notes WHERE session_id = ? ORDER BY updated_at DESC').all(sessionId);
}

export function listAllNotes(db: Database) {
  return db.query(`SELECT notes.id, notes.session_id, notes.text, notes.title, notes.updated_at,
    COALESCE(sessions.name, 'Сессия #' || notes.session_id) AS session_name
    FROM notes LEFT JOIN sessions ON sessions.id = notes.session_id ORDER BY notes.updated_at DESC`).all();
}

export function saveNote(db: Database, sessionId: number, id: unknown, text: unknown, title: unknown = '') {
  if (typeof id !== 'string' || typeof text !== 'string' || typeof title !== 'string') throw new Error('Некорректная заметка');
  if (title.length > 200) throw new Error('Заголовок заметки слишком длинный');
  const result = db.query('INSERT INTO notes (id, session_id, text, title, updated_at) VALUES (?, ?, ?, ?, ?) ON CONFLICT(id) DO UPDATE SET text = excluded.text, title = excluded.title, updated_at = excluded.updated_at WHERE session_id = excluded.session_id').run(id, sessionId, text, title, new Date().toISOString());
  if (!result.changes) throw new Error('Заметка принадлежит другой сессии');
}

export function deleteNote(db: Database, sessionId: number, id: unknown) {
  if (typeof id !== 'string') throw new Error('Некорректная заметка');
  db.query('DELETE FROM notes WHERE id = ? AND session_id = ?').run(id, sessionId);
}
