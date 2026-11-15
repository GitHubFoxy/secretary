import { expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('session deletion persists, preserves children, and ignores late callbacks', () => {
  const directory = mkdtempSync(join(tmpdir(), 'secretary-delete-'));
  try {
    const result = Bun.spawnSync([process.execPath, '--eval', `
      import { expect } from 'bun:test';
      const { Session, db } = await import(${JSON.stringify(import.meta.dir + '/session.ts')});
      const secretary = Session.create('opus', '.');
      expect(() => secretary.delete()).toThrow('Cannot delete secretary');
      const parent = Session.create('opus', '.');
      const child = Session.create('opus', '.', 'claude', parent.id);
      parent.emit({type:'user', message:{content:'history'}});
      child.emit({type:'user', message:{content:'keep'}});
      parent.delete();
      expect(Session.get(parent.id)).toBeNull();
      expect(parent.events()).toEqual([]);
      expect(child.info.parent_id).toBeNull();
      expect(child.events()).toHaveLength(1);
      const active = Session.create('opus', '.');
      const worker = Bun.spawn(['sleep', '60']);
      active.recordProcess(worker);
      active.running = true;
      active.queue = ['queued'];
      let reported = false;
      active.onComplete = () => { reported = true; };
      active.delete();
      await worker.exited;
      expect(worker.signalCode).toBe('SIGKILL');
      expect(active.queue).toEqual([]);
      const replacement = Session.create('opus', '.');
      active.delete();
      active.emit({type:'result', result:'late'});
      active.finish(true);
      expect(replacement.info.status).toBe('idle');
      expect(replacement.events()).toEqual([]);
      expect(reported).toBe(false);
      expect(() => active.send('late')).toThrow('Session was deleted');
      db.close();
      const { Database } = await import('bun:sqlite');
      const reopened = new Database('secretary.sqlite');
      expect(reopened.query('SELECT * FROM sessions WHERE id = ?').get(parent.id)).toBeNull();
      expect(reopened.query('SELECT * FROM events WHERE session_id = ?').all(parent.id)).toEqual([]);
      reopened.close();
    `], { cwd: directory, stdout: 'pipe', stderr: 'pipe' });
    expect({ exitCode: result.exitCode, stderr: result.stderr.toString() }).toEqual({ exitCode: 0, stderr: '' });
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
