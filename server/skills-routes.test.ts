import { test, expect } from 'bun:test';
import { Database } from 'bun:sqlite';
import { createHash } from 'node:crypto';
import { mkdtempSync, mkdirSync, writeFileSync, readFileSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { Secrets } from './secrets';
import { ExternalMcp } from './external-mcp';

test('маршруты показывают skill, восстанавливают выбранную версию и удаляют копии', async () => {
  const home = mkdtempSync(join(tmpdir(), 'skills-http-'));
  const proc = Bun.spawn([process.execPath, 'index.ts'], { cwd: import.meta.dir, env: { ...process.env, HOME: home, SECRETARY_PORT: '0', SECRETARY_MCP_PORT: '0', SECRETARY_PREVIEW_PORT: '0' }, stdout: 'pipe', stderr: 'pipe' });
  try {
    const { value } = await proc.stdout.getReader().read();
    const url = new TextDecoder().decode(value).match(/http:\/\/[^\s]+/)![0];
    const token = 'a'.repeat(64);
    const db = new Database(`${home}/.secretary/auth.sqlite`);
    db.run('INSERT INTO devices VALUES (?)', [createHash('sha256').update(token).digest('hex')]); db.close();
    const root = `${home}/.secretary`, version = '1234567890000-11111111-1111-1111-1111-111111111111.md';
    mkdirSync(`${root}/skills/example/versions`, {recursive:true});
    writeFileSync(`${root}/skills/example/SKILL.md`, 'current');
    writeFileSync(`${root}/skills/example/versions/${version}`, 'old');
    const headers = { cookie: `secretary=${token}`, origin: new URL(url).origin };
    await new Secrets(root).set('VIEW_KEY', 'endpoint-private-value');
    new ExternalMcp(root).save([{name:'test', enabled:true, roles:['secretary'], type:'http', url:'https://example.com', headers:{Authorization:'${secret:VIEW_KEY}'}}]);
    const view = await fetch(new URL('/settings/agent-view?session=1', url), {headers});
    expect(view.status).toBe(200);
    const viewText = await view.text();
    expect(viewText).not.toContain('endpoint-private-value');
    expect(viewText).not.toMatch(/Bearer [a-f0-9]{64}/);
    const builtin = new URL('/settings/skills?name=day-summary', url);
    expect((await (await fetch(builtin, {headers})).json()).builtin).toBe(true);
    expect((await fetch(builtin, {method:'DELETE', headers})).status).toBe(400);
    const endpoint = new URL('/settings/skills?name=example', url);
    expect((await (await fetch(endpoint, {headers})).json()).content).toBe('current');
    expect((await fetch(endpoint, {method:'POST', headers, body:JSON.stringify({version})})).status).toBe(204);
    expect(readFileSync(`${root}/.agents/skills/example/SKILL.md`, 'utf8')).toBe('old');
    expect((await fetch(endpoint, {method:'DELETE', headers})).status).toBe(204);
    expect((await fetch(endpoint, {headers})).status).toBe(400);
  } finally { proc.kill(); await proc.exited; rmSync(home, {recursive:true, force:true}); }
});
