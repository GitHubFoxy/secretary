import { expect, test } from 'bun:test';
import { mkdtemp, mkdir, rm, writeFile, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

test('использует ответ модели и при её отсутствии выбирает правило тишины', async () => {
  const home = await mkdtemp(join(tmpdir(), 'secretary-turn-test-'));
  try {
    const folder = join(home, '.secretary/asr');
    await mkdir(join(folder, 'smart-turn-venv/bin'), {recursive:true});
    const code = `import {turnComplete} from './smart-turn'; console.log('RESULT', await turnComplete(new Uint8Array([1]))); console.log('RESULT', await turnComplete(new Uint8Array([2]))); process.exit(0);`;
    const run = () => Bun.spawn([process.execPath, '-e', code], {cwd:import.meta.dir, env:{...process.env, HOME:home}, stdout:'pipe', stderr:'pipe'});
    let child = run();
    expect(await new Response(child.stdout).text()).toContain('RESULT true\nRESULT true');
    expect(await child.exited).toBe(0);
    await writeFile(join(folder, 'smart-turn-v3.2-cpu.onnx'), '');
    const python = join(folder, 'smart-turn-venv/bin/python');
    await writeFile(python, `#!/usr/bin/env python3\nimport sys, json\nfor line in sys.stdin:\n request = json.loads(line)\n print(json.dumps({'id': request['id'], 'complete': False, 'ms': 1}), flush=True)\n`);
    await chmod(python, 0o755);
    child = run();
    const output = await new Response(child.stdout).text();
    expect(output.match(/RESULT false/g)).toHaveLength(2);
    expect(await child.exited).toBe(0);
  } finally { await rm(home, {recursive:true, force:true}); }
});
