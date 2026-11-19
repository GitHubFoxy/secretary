import { Database } from 'bun:sqlite';

// Opens the store and brings an older schema up to date. Tests pass ':memory:'.
export function openDatabase(path: string) {
  const db = new Database(path, { create: true });
  const addColumn = (table: string, definition: string) => {
    const name = definition.split(' ')[0];
    const columns = db.query(`PRAGMA table_info(${table})`).all() as { name: string }[];
    if (!columns.some(column => column.name === name)) db.run(`ALTER TABLE ${table} ADD COLUMN ${definition}`);
  };
  db.run(`CREATE TABLE IF NOT EXISTS sessions (
    id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL,
    folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL
  )`);
  addColumn('sessions', 'name TEXT');
  addColumn('sessions', 'reasoning TEXT');
  addColumn('sessions', 'hidden INTEGER NOT NULL DEFAULT 0');
  db.run(`CREATE TABLE IF NOT EXISTS events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)`);
  db.run('CREATE INDEX IF NOT EXISTS events_session_id ON events(session_id)');
  // Messages confirmed to the client that have not reached a harness yet. They survive a restart.
  db.run(`CREATE TABLE IF NOT EXISTS pending_messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id INTEGER NOT NULL, urgent INTEGER NOT NULL, text TEXT NOT NULL, images TEXT NOT NULL)`);
  addColumn('pending_messages', 'in_flight INTEGER NOT NULL DEFAULT 0');
  return db;
}

export function writeEvent(db: Database, sessionId: number, event: unknown, time = new Date().toISOString()) {
  db.query('INSERT INTO events VALUES (?, ?, ?)').run(sessionId, JSON.stringify(event), time);
}
