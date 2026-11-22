import { expect, test } from 'bun:test';
import { mkdtemp, mkdir, rm, writeFile, chmod } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { transcribe } from './voice';

test('rejects invalid audio', async () => {
  await expect(transcribe(new Uint8Array(44))).rejects.toThrow('Некорректный WAV');
});

test('returns the recognizer text and reports recognizer failures', async () => {
  const home = await mkdtemp(join(tmpdir(), 'secretary-voice-test-'));
  try {
    const folder = join(home, '.secretary/asr');
    await mkdir(folder, {recursive:true});
    const executable = join(folder, 'sherpa-onnx-offline');
    for (const [name, files] of Object.entries({parakeet:['tokens.txt', 'encoder.onnx', 'decoder.onnx', 'joiner.onnx'], gigaam:['gigaam_v3_e2e_rnnt_tokens.txt', 'gigaam_v3_e2e_rnnt_encoder_int8.onnx', 'gigaam_v3_e2e_rnnt_decoder.onnx', 'gigaam_v3_e2e_rnnt_joint.onnx']})) {
      await mkdir(join(folder, name));
      for (const file of files) await writeFile(join(folder, name, file), '');
    }
    await writeFile(executable, '#!/bin/sh\nprintf \"%s\\n\" \"$@\" > \"$HOME/args\"\necho \"{\\\"text\\\":\\\"Привет\\\"}\"\n');
    await chmod(executable, 0o755);
    const code = `import {transcribe} from './voice'; const wav = new Uint8Array(44); wav.set(new TextEncoder().encode('RIFF'), 0); wav.set(new TextEncoder().encode('WAVE'), 8); try { console.log(await transcribe(wav)); } catch (e) { console.error(e.message); process.exit(1); }`;
    const run = () => Bun.spawn([process.execPath, '-e', code], {cwd:import.meta.dir, env:{...process.env, HOME:home}, stdout:'pipe', stderr:'pipe'});
    let child = run();
    expect(await new Response(child.stdout).text()).toBe('Привет\n');
    expect(await child.exited).toBe(0);
    let args = await Bun.file(join(home, 'args')).text();
    expect(args).toContain('--model-type=gigaam');
    expect(args).toContain('/gigaam/gigaam_v3_e2e_rnnt_encoder_int8.onnx');
    await writeFile(join(home, '.secretary/voice-model'), 'parakeet');
    child = run();
    expect(await new Response(child.stdout).text()).toBe('Привет\n');
    expect(await child.exited).toBe(0);
    args = await Bun.file(join(home, 'args')).text();
    expect(args).toContain('/parakeet/encoder.onnx');
    expect(args).not.toContain('--model-type=');
    await rm(join(folder, 'parakeet/encoder.onnx'));
    child = run();
    expect(await new Response(child.stderr).text()).toContain('install-voice.sh');
    expect(await child.exited).toBe(1);
    await writeFile(join(folder, 'parakeet/encoder.onnx'), '');
    await writeFile(executable, '#!/bin/sh\necho "model unavailable" >&2\nexit 1\n');
    child = run();
    expect(await new Response(child.stderr).text()).toContain('model unavailable');
    expect(await child.exited).toBe(1);
  } finally { await rm(home, {recursive:true, force:true}); }
});
