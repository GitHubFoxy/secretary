import { test, expect } from 'bun:test';
import { mkdtemp, readFile, stat, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Secrets } from './secrets';

test('секрет сохраняется зашифрованным и не попадает в события', async () => {
  const root = await mkdtemp(join(tmpdir(), 'secretary-secret-'));
  try {
    const events: any[] = [];
    const secrets = new Secrets(root, (_, event) => events.push(event));
    const result = secrets.request(1, 'TOKEN', 'для доступа');
    await Bun.sleep(10);
    await secrets.approve(events[0].id, 'test-secret-value');
    expect(await result).toBe('Секрет TOKEN сохранён');
    expect(await new Secrets(root).get('TOKEN')).toBe('test-secret-value');
    expect(await secrets.request(1, 'TOKEN', 'доступ')).toBe('Секрет TOKEN уже сохранён');
    expect(JSON.stringify(events)).not.toContain('test-secret-value');
    expect(await readFile(join(root, 'secrets.json'), 'utf8')).not.toContain('test-secret-value');
    expect((await stat(join(root, 'secrets.json'))).mode & 0o777).toBe(0o600);
    expect((await stat(join(root, 'secrets.key'))).mode & 0o777).toBe(0o600);
  } finally { await rm(root, { recursive: true, force: true }); }
});
