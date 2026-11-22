import { expect, test } from 'bun:test';
import { Database } from 'bun:sqlite';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { migrateDatabase, openDatabase } from './database';

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
