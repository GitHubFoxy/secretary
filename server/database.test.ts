import { expect, test } from 'bun:test';
import { Database } from 'bun:sqlite';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { migrateDatabase, openDatabase, recall, writeEvent } from './database';

test('переносит историю из WAL и не заменяет существующую базу', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-copy-'));
  try {
    const source = join(dir, 'old.sqlite');
    const destination = join(dir, 'new.sqlite');
    const old = new Database(source);
    old.run('PRAGMA journal_mode = WAL');
    old.run('CREATE TABLE history (message TEXT)');
    old.run("INSERT INTO history VALUES ('сохранено')");
    migrateDatabase(source, destination);
    old.run("INSERT INTO history VALUES ('позже')");
    migrateDatabase(source, destination);
    const copy = new Database(destination);
    expect(copy.query('SELECT message FROM history').all()).toEqual([{ message: 'сохранено' }]);
    copy.close();
    old.close();
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test('обновляет старую базу без потери данных и повторно открывает её', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-migration-'));
  const path = join(dir, 'secretary.sqlite');
  try {
    const old = new Database(path);
    old.run("CREATE TABLE sessions (id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL, folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL)");
    old.run("INSERT INTO sessions VALUES (1, NULL, 'claude', 'opus', '/tmp', NULL, 'idle')");
    old.close();
    for (let i = 0; i < 2; i++) {
      const db = openDatabase(path);
      expect(db.query('SELECT model, hidden FROM sessions WHERE id = 1').get()).toEqual({ model: 'opus', hidden: 0 });
      db.close();
    }
  } finally { rmSync(dir, { recursive: true, force: true }); }
});

test('индексирует старые и новые сообщения, исключая инструменты', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-search-'));
  const path = join(dir, 'db');
  try {
    let db = openDatabase(path);
    db.query('INSERT INTO events VALUES (?, ?, ?)').run(1, JSON.stringify({ type: 'user', message: { content: 'Старая договорённость' } }), '2026-01-01');
    writeEvent(db, 1, { type: 'result', result: 'Новая встреча' });
    writeEvent(db, 1, { type: 'assistant', message: { content: [{ type: 'tool_use', name: 'Bash', input: { command: 'секрет' } }] } });
    db.close();
    db = openDatabase(path);
    expect(recall(db, 1, 'договорённость')[0]).toMatchObject({ text: 'Старая договорённость', time: '2026-01-01' });
    expect(recall(db, 1, 'встреча').length).toBe(1);
    expect(recall(db, 1, 'секрет')).toEqual([]);
    db.close();
    db = openDatabase(path);
    expect(recall(db, 1, 'договорённость').length).toBe(1);
    db.close();
  } finally { rmSync(dir, { recursive: true, force: true }); }
});
