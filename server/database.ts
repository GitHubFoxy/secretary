import { Database } from 'bun:sqlite';
import { chatMessage } from './chat';
import { existsSync, linkSync, unlinkSync } from 'node:fs';
import { randomUUID } from 'node:crypto';

export function migrateDatabase(source: string, destination: string) {
  if (!existsSync(source) || existsSync(destination)) return;
  const temporary = `${destination}.${randomUUID()}`;
  const old = new Database(source, { readonly: true });
  try {
    if ((old.query('PRAGMA integrity_check').get() as { integrity_check: string }).integrity_check !== 'ok') throw new Error('Повреждена старая база данных');
    old.query('VACUUM INTO ?').run(temporary);
    linkSync(temporary, destination);
  } finally {
    old.close();
    if (existsSync(temporary)) unlinkSync(temporary);
  }
}

// Opens the store and brings an older schema up to date. Tests pass ':memory:'.
export function openDatabase(path: string) {
  const db = new Database(path, { create: true });
  db.run('PRAGMA busy_timeout = 5000');
  db.run('PRAGMA journal_mode = WAL');
  const addColumn = (table: string, definition: string) => {
    const name = definition.split(' ')[0];
    const columns = db.query(`PRAGMA table_info(${table})`).all() as { name: string }[];
    if (!columns.some(column => column.name === name)) db.run(`ALTER TABLE ${table} ADD COLUMN ${definition}`);
  };
  db.run(`CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL,
    folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL
  )`);
  addColumn('sessions', 'branch TEXT');
  addColumn('sessions', 'name TEXT');
  addColumn('sessions', 'reasoning TEXT');
  addColumn('sessions', 'hidden INTEGER NOT NULL DEFAULT 0');
  db.run(`CREATE TABLE IF NOT EXISTS events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)`);
  db.run('CREATE INDEX IF NOT EXISTS events_session_id ON events(session_id)');
  // Messages confirmed to the client that have not reached a harness yet. They survive a restart.
  db.run(`CREATE TABLE IF NOT EXISTS pending_messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id INTEGER NOT NULL, urgent INTEGER NOT NULL, text TEXT NOT NULL, images TEXT NOT NULL)`);
  addColumn('pending_messages', 'in_flight INTEGER NOT NULL DEFAULT 0');
  addColumn('pending_messages', 'voice INTEGER NOT NULL DEFAULT 0');
  db.run(`CREATE TABLE IF NOT EXISTS schedules (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id INTEGER NOT NULL, text TEXT NOT NULL, cron TEXT, next_run INTEGER, last_run INTEGER)`);
  db.run(`CREATE TABLE IF NOT EXISTS notes (id TEXT PRIMARY KEY, session_id INTEGER NOT NULL, text TEXT NOT NULL, updated_at TEXT NOT NULL)`);
  db.run(`CREATE TABLE IF NOT EXISTS drafts (session_id INTEGER PRIMARY KEY, text TEXT NOT NULL, updated_at TEXT NOT NULL)`);
  db.run("CREATE VIRTUAL TABLE IF NOT EXISTS message_search USING fts5(text, session_id UNINDEXED, time UNINDEXED, event_id UNINDEXED)");
  const existing = new Set((db.query('SELECT event_id FROM message_search').all() as {event_id: number}[]).map(row => row.event_id));
  const rows = db.query('SELECT rowid, session_id, json, time FROM events').all() as {rowid: number; session_id: number; json: string; time: string}[];
  db.transaction(() => { for (const row of rows) if (!existing.has(row.rowid)) indexEvent(db, row.rowid, row.session_id, JSON.parse(row.json), row.time); })();
  return db;
}

export function writeEvent(db: Database, sessionId: number, event: unknown, time = new Date().toISOString()) {
  return db.transaction(() => {
    const id = Number(db.query('INSERT INTO events VALUES (?, ?, ?)').run(sessionId, JSON.stringify(event), time).lastInsertRowid);
    indexEvent(db, id, sessionId, event, time);
    return id;
  })();
}

function indexEvent(db: Database, id: number, sessionId: number, event: unknown, time: string) {
  const message = chatMessage(event);
  if (message && ['user', 'agent', 'report'].includes(message.kind) && 'text' in message && message.text)
    db.query('INSERT INTO message_search VALUES (?, ?, ?, ?)').run(message.text, sessionId, time, id);
}
export function recall(db: Database, sessionId: number, query: string) {
  const words = query.slice(0, 500).match(/[\p{L}\p{N}_]+/gu);
  if (!words) return [];
  return db.query(`SELECT session_id, time, event_id, snippet(message_search, 0, '', '', '…', 40) AS text FROM message_search WHERE message_search MATCH ? AND session_id = ? ORDER BY rank LIMIT 10`).all(words.map(word => `"${word}"`).join(' AND '), sessionId);
}
