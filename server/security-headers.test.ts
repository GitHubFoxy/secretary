import { test, expect } from 'bun:test';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openAuth } from './auth';
import { saveFile } from './files';

test('HTTP responses include security headers', async () => {
  const home = mkdtempSync(join(tmpdir(), 'secretary-headers-'));
  const proc = Bun.spawn([process.execPath, 'index.ts'], { cwd: import.meta.dir, env: { ...process.env, HOME: home, SECRETARY_PORT: '0', SECRETARY_MCP_PORT: '0', SECRETARY_PREVIEW_PORT: '0' }, stdout: 'pipe', stderr: 'pipe' });
  try {
    const reader = proc.stdout.getReader();
    const { value } = await reader.read();
    const url = new TextDecoder().decode(value).match(/http:\/\/[^\s]+/)?.[0];
    expect(url).toBeDefined();
    const auth = openAuth(join(home, '.secretary'));
    const token = auth.pair(auth.createCode(), 'test');
    const file = await saveFile(new Request('http://localhost/files?name=test.html', { method:'POST', body:'<script>1</script>' }), join(home, '.secretary'));
    const path = `files/${file.id}/${file.name}?preview`;
    expect((await fetch(new URL(path, url))).status).toBe(401);
    const preview = await fetch(new URL(path, url), { headers: { cookie:`secretary=${token}` } });
    expect(preview.status).toBe(200);
    expect(preview.headers.get('content-security-policy')).toContain("sandbox allow-scripts");
    expect(preview.headers.get('content-security-policy')).toContain("connect-src 'none'");
    expect(preview.headers.get('x-frame-options')).toBe('SAMEORIGIN');
    expect(preview.headers.get('x-content-type-options')).toBe('nosniff');
    for (const path of ['', 'private']) {
      const response = await fetch(new URL(path, url));
      expect(response.headers.get('content-security-policy')).toContain("frame-ancestors 'none'");
      expect(response.headers.get('x-frame-options')).toBe('DENY');
      expect(response.headers.get('referrer-policy')).toBe('no-referrer');
      expect(response.headers.get('x-content-type-options')).toBe('nosniff');
    }
  } finally { proc.kill(); await proc.exited; rmSync(home, { recursive: true, force: true }); }
});
