import { test, expect } from 'bun:test';
import { mkdtemp, rm, stat } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ExternalMcp } from './external-mcp';
import { Secrets } from './secrets';

test('реестр сохраняет настройки, выбирает роль и подставляет секреты', async () => {
  const root = await mkdtemp(join(tmpdir(), 'secretary-mcp-'));
  try {
    const registry = new ExternalMcp(root);
    await new Secrets(root).set('TOKEN', 'private-value');
    const stdio = { name: 'echo', enabled: true, roles: ['secretary'], type: 'stdio', command: 'bun', args: ['echo.ts'], env: { TOKEN: 'Bearer ${secret:TOKEN}' } };
    const http = { name: 'remote', enabled: true, roles: ['specialist'], type: 'http', url: 'https://example.org/mcp', headers: { Authorization: '${secret:TOKEN}' } };
    registry.save([stdio, http]);
    expect(new ExternalMcp(root).list()).toEqual([stdio, http]);
    expect((await registry.resolve('secretary'))[0].env).toEqual({ TOKEN: 'Bearer private-value' });
    expect((await registry.resolve('specialist'))[0].headers).toEqual({ Authorization: 'private-value' });
    expect((await stat(join(root, 'mcp.json'))).mode & 0o777).toBe(0o600);
    expect(() => registry.save([stdio, stdio])).toThrow();
    expect(() => registry.save([{ ...stdio, name: 'secretary' }])).toThrow();
    expect(registry.list()).toEqual([stdio, http]);
    await expect(new ExternalMcp(root).resolve('secretary')).resolves.toHaveLength(1);
  } finally { await rm(root, { recursive: true, force: true }); }
});
