import { expect, test } from 'bun:test';
import { Database } from 'bun:sqlite';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openDatabase } from './database';

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
