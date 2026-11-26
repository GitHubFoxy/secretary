import { expect, test } from 'bun:test';
import { mkdirSync, mkdtempSync, readFileSync, rmSync, symlinkSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Memory } from './memory';

test('файлы памяти: список, правка, конфликт, удаление и защита пути', () => {
  const root = mkdtempSync(join(tmpdir(), 'secretary-memory-'));
  try {
    const memory = new Memory(root);
    expect(memory.list()).toEqual([]);
    mkdirSync(join(root, 'memory'));
    writeFileSync(join(root, 'memory', 'owner.md'), 'первый факт');
    symlinkSync(join(root, 'memory', 'owner.md'), join(root, 'memory', 'link.md'));
    expect(memory.list()).toEqual(['owner.md']);
    expect(() => memory.read('../owner.md')).toThrow();
    expect(() => memory.read('link.md')).toThrow();
    const old = memory.read('owner.md');
    const updated = memory.write('owner.md', 'новый факт', old.version);
    expect(readFileSync(join(root, 'memory', 'owner.md'), 'utf8')).toBe('новый факт');
    expect(() => memory.write('owner.md', 'потерянная правка', old.version)).toThrow();
    expect(() => memory.delete('owner.md', old.version)).toThrow();
    memory.delete('owner.md', updated.version);
    expect(memory.list()).toEqual([]);
  } finally { rmSync(root, { recursive: true, force: true }); }
});
