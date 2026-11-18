import { expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('скрытие и восстановление сохраняют данные при перезапуске', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-visibility-'));
  try {
    const source = JSON.stringify(import.meta.dir + '/session.ts');
    const create = Bun.spawnSync([process.execPath, '--eval', `
      import { expect } from 'bun:test';
      const { Session, db } = await import(${source});
      const main = Session.create('opus', '.');
      expect(() => main.setHidden(true)).toThrow();
      const s = Session.create('opus', '.');
      const internal = Session.create('opus', '/home/user/.secretary/agents');
      expect(() => internal.setHidden(true)).toThrow();
      db.query('UPDATE sessions SET process_identity = ? WHERE id = ?').run('unknown:identity', s.id);
      db.query('INSERT INTO pending_messages (session_id,urgent,text,images) VALUES (?,0,?,?)').run(s.id,'queued','[]');
      s.emit({type:'user',message:{content:'history'}});
      const before = s.info;
      s.setHidden(true);
      expect(s.info).toEqual({...before, hidden:1});
      expect(s.events()).toHaveLength(1);
      expect(db.query('SELECT text FROM pending_messages WHERE session_id = ?').get(s.id)).toEqual({text:'queued'});
      expect(() => s.setHidden('false')).toThrow();
      db.close();
    `], {cwd:dir, stdout:'pipe', stderr:'pipe'});
    expect({code:create.exitCode, stderr:create.stderr.toString()}).toEqual({code:0, stderr:''});
    const restore = Bun.spawnSync([process.execPath, '--eval', `
      import { expect } from 'bun:test';
      const { Session, db } = await import(${source});
      const row = db.query('SELECT * FROM sessions WHERE id = 2').get();
      expect(row.hidden).toBe(1);
      expect(row.process_identity).toBe('unknown:identity');
      expect(db.query('SELECT text FROM pending_messages WHERE session_id = 2').get()).toEqual({text:'queued'});
      expect(db.query('SELECT count(*) AS n FROM events WHERE session_id = 2').get().n).toBe(1);
      const s = Session.get(2);
      s.setHidden(false);
      expect(s.info.hidden).toBe(0);
      db.close();
    `], {cwd:dir, stdout:'pipe', stderr:'pipe'});
    expect({code:restore.exitCode, stderr:restore.stderr.toString()}).toEqual({code:0, stderr:''});
  } finally { rmSync(dir, {recursive:true, force:true}); }
});
