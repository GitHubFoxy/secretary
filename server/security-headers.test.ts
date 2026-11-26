import { test, expect } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('HTTP responses include security headers', async () => {
  const home = mkdtempSync(join(tmpdir(), 'secretary-headers-'));
  const proc = Bun.spawn([process.execPath, 'index.ts'], { cwd: import.meta.dir, env: { ...process.env, HOME: home, SECRETARY_PORT: '0', SECRETARY_MCP_PORT: '0' }, stdout: 'pipe', stderr: 'pipe' });
  try {
    const reader = proc.stdout.getReader();
    const { value } = await reader.read();
    const url = new TextDecoder().decode(value).match(/http:\/\/[^\s]+/)?.[0];
    expect(url).toBeDefined();
    for (const path of ['', 'private']) {
      const response = await fetch(new URL(path, url));
      expect(response.headers.get('content-security-policy')).toContain("frame-ancestors 'none'");
      expect(response.headers.get('x-frame-options')).toBe('DENY');
      expect(response.headers.get('referrer-policy')).toBe('no-referrer');
      expect(response.headers.get('x-content-type-options')).toBe('nosniff');
    }
  } finally { proc.kill(); await proc.exited; rmSync(home, { recursive: true, force: true }); }
});
