import type { Database } from 'bun:sqlite';

export function getDraft(db: Database, sessionId: number) {
  return (db.query('SELECT text FROM drafts WHERE session_id = ?').get(sessionId) as {text: string} | null)?.text ?? '';
}

export function saveDraft(db: Database, sessionId: number, text: unknown) {
  if (typeof text !== 'string') throw new Error('Некорректный черновик');
  if (!text) db.query('DELETE FROM drafts WHERE session_id = ?').run(sessionId);
  else db.query('INSERT INTO drafts VALUES (?, ?, ?) ON CONFLICT(session_id) DO UPDATE SET text = excluded.text, updated_at = excluded.updated_at').run(sessionId, text, new Date().toISOString());
}
