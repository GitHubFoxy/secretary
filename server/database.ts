import { Database } from 'bun:sqlite';

export const db = new Database('secretary.sqlite', { create: true });
function addColumn(table: string, definition: string) {
  const name = definition.split(' ')[0];
  const columns = db.query(`PRAGMA table_info(${table})`).all() as { name: string }[];
  if (!columns.some(column => column.name === name)) db.run(`ALTER TABLE ${table} ADD COLUMN ${definition}`);
}

db.run(`CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL,
  folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL
)`);
addColumn('sessions', 'process_identity TEXT');
addColumn('sessions', 'deletion_processes TEXT');
addColumn('sessions', 'process_uncontained INTEGER NOT NULL DEFAULT 1');
addColumn('sessions', 'name TEXT');
addColumn('sessions', 'reasoning TEXT');
addColumn('sessions', 'hidden INTEGER NOT NULL DEFAULT 0');
db.run(`CREATE TABLE IF NOT EXISTS events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)`);
db.run('CREATE INDEX IF NOT EXISTS events_session_id ON events(session_id)');
// Подтверждённые сообщения, которые ещё не ушли в ход. Лежат в базе, чтобы пережить перезапуск программы.
db.run(`CREATE TABLE IF NOT EXISTS pending_messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id INTEGER NOT NULL, urgent INTEGER NOT NULL, text TEXT NOT NULL, images TEXT NOT NULL)`);
addColumn('pending_messages', 'in_flight INTEGER NOT NULL DEFAULT 0');

