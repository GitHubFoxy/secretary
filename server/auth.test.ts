import { test, expect } from 'bun:test';
import { mkdtempSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('pairing code is one-use and attempt limit survives a restart', () => {
  const home = mkdtempSync(join(tmpdir(), 'secretary-auth-test-'));
  const run = (script: string) => {
    const result = Bun.spawnSync([process.execPath, '-e', script], { cwd: import.meta.dir, env: {...process.env, HOME: home} });
    expect(result.exitCode).toBe(0);
    return result.stdout.toString().trim();
  };
  const code = run("import {createCode} from './auth'; console.log(createCode())");
  for (let i = 0; i < 10; i++) expect(run("import {pair} from './auth'; console.log(pair('wrong', 'attacker'))")).toBe('null');
  expect(run(`import {pair} from './auth'; console.log(pair(${JSON.stringify(code)}, 'attacker'))`)).toBe('null');
  expect(run(`import {pair} from './auth'; console.log(!!pair(${JSON.stringify(code)}, 'owner'))`)).toBe('true');
  const next = run("import {createCode} from './auth'; console.log(createCode())");
  expect(run(`import {pair} from './auth'; console.log(!!pair(${JSON.stringify(next)}, 'owner'))`)).toBe('true');
  expect(run(`import {pair} from './auth'; console.log(pair(${JSON.stringify(next)}, 'owner'))`)).toBe('null');
});
