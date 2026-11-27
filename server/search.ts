import type { Database } from 'bun:sqlite';
import { chatMessage } from './chat';

const projectionVersion = 1;

export function prepareSearch(db: Database) {
  db.run("CREATE VIRTUAL TABLE IF NOT EXISTS message_search USING fts5(text, session_id UNINDEXED, time UNINDEXED, event_id UNINDEXED)");
  db.run('CREATE TABLE IF NOT EXISTS search_projection (version INTEGER NOT NULL)');
  db.transaction(() => {
    const version = db.query('SELECT version FROM search_projection').get() as {version: number} | null;
    if (version?.version !== projectionVersion) {
      db.run('DELETE FROM message_search');
      db.run('DELETE FROM search_projection');
      db.query('INSERT INTO search_projection VALUES (?)').run(projectionVersion);
    }
    const existing = new Set((db.query('SELECT event_id FROM message_search').all() as {event_id: number}[]).map(row => row.event_id));
    const rows = db.query('SELECT rowid, session_id, json, time FROM events').all() as {rowid: number; session_id: number; json: string; time: string}[];
    for (const row of rows) if (!existing.has(row.rowid)) indexEvent(db, row.rowid, row.session_id, JSON.parse(row.json), row.time);
  })();
}

export function indexEvent(db: Database, id: number, sessionId: number, event: unknown, time: string) {
  const message = chatMessage(event);
  if (message && ['user', 'agent', 'report'].includes(message.kind) && 'text' in message && message.text)
    db.query('INSERT INTO message_search VALUES (?, ?, ?, ?)').run(message.text, sessionId, time, id);
}
export function recall(db: Database, sessionId: number, query: string) {
  const words = query.slice(0, 500).match(/[\p{L}\p{N}_]+/gu);
  if (!words) return [];
  return db.query(`SELECT session_id, time, event_id, snippet(message_search, 0, '', '', '…', 40) AS text FROM message_search WHERE message_search MATCH ? AND session_id = ? ORDER BY rank LIMIT 10`).all(words.map(word => `"${word}"`).join(' AND '), sessionId);
}
