import { expect, test } from 'bun:test';
import { Database } from 'bun:sqlite';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('обновляет старую базу без потери данных и повторно открывает её', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-migration-'));
  try {
    const old = new Database(join(dir, 'secretary.sqlite'));
    old.run("CREATE TABLE sessions (id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL, folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL)");
    old.run("INSERT INTO sessions VALUES (1, NULL, 'claude', 'opus', '/tmp', NULL, 'idle')");
    old.close();
    const source = JSON.stringify(import.meta.dir + '/database.ts');
    for (let i = 0; i < 2; i++) {
      const run = Bun.spawnSync([process.execPath, '--eval', `
        const {db} = await import(${source});
        const row = db.query('SELECT * FROM sessions WHERE id = 1').get();
        if (row.model !== 'opus' || row.hidden !== 0 || row.process_uncontained !== 1) throw Error('migration lost data');
        db.close();
      `], {cwd:dir, stdout:'pipe', stderr:'pipe'});
      expect({code:run.exitCode, stderr:run.stderr.toString()}).toEqual({code:0, stderr:''});
    }
  } finally { rmSync(dir, {recursive:true, force:true}); }
});

test('ошибка несовместимой схемы не подавляется', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-migration-error-'));
  try {
    const old = new Database(join(dir, 'secretary.sqlite'));
    old.run('CREATE VIEW sessions AS SELECT 1 AS id');
    old.close();
    const run = Bun.spawnSync([process.execPath, '--eval', `await import(${JSON.stringify(import.meta.dir + '/database.ts')})`], {cwd:dir, stdout:'pipe', stderr:'pipe'});
    expect(run.exitCode).not.toBe(0);
    expect(run.stderr.toString()).toContain('view');
  } finally { rmSync(dir, {recursive:true, force:true}); }
});
