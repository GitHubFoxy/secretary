import { expect, test } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('сессии: удаление, видимость и смена harness', () => {
  const directory = mkdtempSync(join(tmpdir(), 'secretary-session-'));
  try {
    const result = Bun.spawnSync([process.execPath, '--eval', `
      import { expect } from 'bun:test';
      const { Session } = await import(${JSON.stringify(import.meta.dir + '/session.ts')});
      const secretary = Session.create('opus', '.');
      expect(() => secretary.delete()).toThrow('Cannot delete secretary');
      expect(() => secretary.setHidden(true)).toThrow();
      expect(() => Session.create('opus', '/home/user/.secretary/agents').setHidden(true)).toThrow();
      expect(() => secretary.setName('   ')).toThrow();

      const parent = Session.create('opus', '.');
      const child = Session.create('opus', '.', 'claude', parent.id, 'Проверка', 'high');
      child.setHidden(true);
      expect(Session.get(child.id).info.hidden).toBe(1);

      child.onEvent(() => {});
      parent.delete();
      expect(Session.get(parent.id)).toBeNull();
      expect(child.info.parent_id).toBeNull();

      child.configure('claude', 'sonnet', 'low');
      expect(child.info).toMatchObject({ model: 'sonnet', reasoning: 'low' });
    `], { cwd: directory, stdout: 'pipe', stderr: 'pipe' });
    expect({ exitCode: result.exitCode, stderr: result.stderr.toString() }).toEqual({ exitCode: 0, stderr: '' });
  } finally {
    rmSync(directory, { recursive: true, force: true });
  }
});
