import { expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('удаление ждёт завершения процесса и сохраняет сессию при неудачной остановке', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-delete-wait-'));
  try {
    const result = Bun.spawnSync([process.execPath, '--eval', `
      import { expect } from 'bun:test';
      const { Session, db } = await import(${JSON.stringify(import.meta.dir + '/session.ts')});
      Session.deleteDeadlineMs = 200;
      Session.create('opus', '.');
      const s = Session.create('opus', '.');
      const { existsSync, readFileSync } = await import('node:fs');
      const child = Bun.spawn(['sh', '-c', 'sleep 60 & echo $! > descendant.pid; wait']);
      while (!existsSync('descendant.pid')) await Bun.sleep(10);
      const descendant = Number(readFileSync('descendant.pid', 'utf8'));
      const originalKill = process.kill.bind(process);
      try {
        s.recordProcess(child);
        s.running = true;
        const identity = s.info.process_identity;
        s.send('очередь должна сохраниться');
        process.kill = (pid, signal) => pid === descendant && signal === 'SIGKILL' ? true : originalKill(pid, signal);
        const attempt = s.delete();
        expect(s.info.process_identity).toBe(identity);
        expect(() => s.send('нельзя отправить')).toThrow();
        await expect(s.delete()).rejects.toThrow();
        await expect(attempt).rejects.toThrow();
        expect(s.info.process_identity).toBe(identity);
        expect(s.info.id).toBe(s.id);
        expect(s.queued().map(item => item.text)).toEqual(['очередь должна сохраниться']);
        expect(JSON.parse(s.info.deletion_processes)).toContainEqual(expect.stringMatching(new RegExp('^' + descendant + ':')));
        originalKill(descendant, 0);
        const restored = Session.get(s.id);
        expect(restored.queued().map(item => item.text)).toEqual(['очередь должна сохраниться']);
        expect(() => restored.send('рано')).toThrow();
        process.kill = originalKill;
        await restored.delete();
        expect(Session.get(s.id)).toBeNull();
        await child.exited;
      } finally { process.kill = originalKill; try { originalKill(descendant, 'SIGKILL'); } catch {} try { child.kill(); } catch {} await child.exited; db.close(); }
    `], {cwd:dir, stdout:'pipe', stderr:'pipe', timeout:15_000});
    expect({exitCode:result.exitCode, stderr:result.stderr.toString()}).toEqual({exitCode:0, stderr:''});
  } finally { rmSync(dir, {recursive:true, force:true}); }
}, 20_000);
