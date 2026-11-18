import { expect, test } from 'bun:test';
import { mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('удаление запрещено для старых и когда-либо запускавшихся сессий, в том числе после перезапуска', () => {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-delete-safe-'));
  try {
    mkdirSync(join(dir, 'bin'));
    for (const name of ['claude', 'codex']) writeFileSync(join(dir, 'bin', name), '#!/bin/sh\nexec sleep 60\n', {mode:0o755});
    const first = Bun.spawnSync([process.execPath, '--eval', `
      import { expect } from 'bun:test';
      import { Database } from 'bun:sqlite';
      import { writeFileSync } from 'node:fs';
      const old = new Database('secretary.sqlite');
      old.run('CREATE TABLE sessions (id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL, folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL)');
      old.run("INSERT INTO sessions (id,harness,model,folder,status) VALUES (1,'claude','opus','.','idle'), (2,'claude','opus','.','idle')");
      old.close();
      const { Session, db } = await import(${JSON.stringify(import.meta.dir + '/session.ts')});
      const legacy = Session.get(2);
      expect(legacy.info.process_uncontained).toBe(1);
      await expect(legacy.delete()).rejects.toThrow('Не удалось подтвердить');
      const unused = Session.create('opus', '.');
      await unused.delete();
      expect(Session.get(unused.id)).toBeNull();
      const ids = [];
      for (const harness of ['claude', 'codex']) {
        const s = Session.create('test', '.', harness);
        ids.push(s.id);
        s.send('первое');
        expect(s.info.process_uncontained).toBe(1);
        s.send('оставить в очереди');
        const before = {row:s.info, queue:s.queued(), events:s.events()};
        const kill = process.kill.bind(process);
        let signals = 0;
        process.kill = (...args) => { signals++; return kill(...args); };
        try { await expect(s.delete()).rejects.toThrow('Не удалось подтвердить'); }
        finally { process.kill = kill; }
        expect(signals).toBe(0);
        expect({row:s.info, queue:s.queued(), events:s.events()}).toEqual(before);
        const child = s.process;
        s.stopped = true;
        child.kill();
        await child.exited;
        while (s.running) await Bun.sleep(10);
        await expect(s.delete()).rejects.toThrow('Не удалось подтвердить');
      }
      const failed = Session.create('test', '/path/does/not/exist', 'claude');
      ids.push(failed.id);
      failed.send('не запустится');
      while (failed.running) await Bun.sleep(10);
      expect(failed.info.process_uncontained).toBe(1);
      await expect(failed.delete()).rejects.toThrow('Не удалось подтвердить');
      writeFileSync('ids.json', JSON.stringify(ids));
      db.close();
    `], {cwd:dir, env:{...process.env, PATH:join(dir, 'bin') + ':' + process.env.PATH}, stdout:'pipe', stderr:'pipe', timeout:15_000});
    expect({exitCode:first.exitCode, stderr:first.stderr.toString()}).toEqual({exitCode:0, stderr:''});
    const second = Bun.spawnSync([process.execPath, '--eval', `
      import { expect } from 'bun:test';
      import { readFileSync } from 'node:fs';
      const { Session, db } = await import(${JSON.stringify(import.meta.dir + '/session.ts')});
      for (const id of [2, ...JSON.parse(readFileSync('ids.json', 'utf8'))]) {
        const s = Session.get(id);
        expect(s.info.process_uncontained).toBe(1);
        await expect(s.delete()).rejects.toThrow('Не удалось подтвердить');
        expect(Session.get(id)).not.toBeNull();
      }
      db.close();
    `], {cwd:dir, stdout:'pipe', stderr:'pipe', timeout:15_000});
    expect({exitCode:second.exitCode, stderr:second.stderr.toString()}).toEqual({exitCode:0, stderr:''});
  } finally { rmSync(dir, {recursive:true, force:true}); }
}, 35_000);
