import { test, expect } from 'bun:test';
import { mkdtemp, mkdir, writeFile, symlink, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { findFiles } from './references';

test('поиск ограничивает результаты и не следует по ссылкам за пределы папки', async () => {
  const root = await mkdtemp(join(tmpdir(), 'references-'));
  const outside = await mkdtemp(join(tmpdir(), 'outside-'));
  try {
    await mkdir(join(root, 'dir'));
    await writeFile(join(root, 'dir', 'needle.txt'), '');
    await writeFile(join(outside, 'secret-needle.txt'), '');
    await symlink(outside, join(root, 'link'));
    for (let i = 0; i < 35; i++) await writeFile(join(root, `item-${i}`), '');
    expect(await findFiles(root, 'needle')).toEqual(['dir/needle.txt']);
    expect((await findFiles(root, 'item-')).length).toBe(30);
    await expect(findFiles(root, '../')).rejects.toThrow();
  } finally { await rm(root, {recursive:true, force:true}); await rm(outside, {recursive:true, force:true}); }
});
