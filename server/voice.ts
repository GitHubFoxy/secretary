import { mkdtemp, rm } from 'node:fs/promises';
import { availableParallelism, tmpdir, homedir } from 'node:os';
import { join } from 'node:path';

const folder = join(homedir(), '.secretary', 'asr');
let previous = Promise.resolve();

export function transcribe(wav: Uint8Array): Promise<string> {
  const work = previous.then(() => recognize(wav));
  previous = work.then(() => {}, () => {});
  return work;
}

async function recognize(wav: Uint8Array): Promise<string> {
  if (wav.length < 44 || Buffer.from(wav.subarray(0, 4)).toString() !== 'RIFF' || Buffer.from(wav.subarray(8, 12)).toString() !== 'WAVE') throw new Error('Некорректный WAV');
  const dir = await mkdtemp(join(tmpdir(), 'secretary-voice-'));
  try {
    const file = join(dir, 'voice.wav');
    await Bun.write(file, wav);
    const process = Bun.spawn([join(folder, 'sherpa-onnx-offline'), `--tokens=${folder}/tokens.txt`, `--encoder=${folder}/encoder.onnx`, `--decoder=${folder}/decoder.onnx`, `--joiner=${folder}/joiner.onnx`, `--num-threads=${Math.min(4, availableParallelism())}`, '--debug=false', file], { stdout: 'pipe', stderr: 'pipe' });
    const [stdout, stderr, status] = await Promise.all([new Response(process.stdout).text(), new Response(process.stderr).text(), process.exited]);
    if (status !== 0) throw new Error(stderr.trim() || 'Ошибка sherpa-onnx');
    const text = JSON.parse(stdout.trim()).text?.trim();
    if (!text) throw new Error('Речь не распознана');
    return text;
  } finally { await rm(dir, {recursive:true, force:true}); }
}
