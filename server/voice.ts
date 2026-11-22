import { mkdtemp, rm } from 'node:fs/promises';
import { availableParallelism, tmpdir, homedir } from 'node:os';
import { join } from 'node:path';

const folder = join(homedir(), '.secretary', 'asr');
const settingsFile = join(homedir(), '.secretary', 'voice-model');
const models = {
  gigaam: { files: ['gigaam_v3_e2e_rnnt_tokens.txt', 'gigaam_v3_e2e_rnnt_encoder_int8.onnx', 'gigaam_v3_e2e_rnnt_decoder.onnx', 'gigaam_v3_e2e_rnnt_joint.onnx'], args: ['--model-type=gigaam'] },
  parakeet: { files: ['tokens.txt', 'encoder.onnx', 'decoder.onnx', 'joiner.onnx'], args: [] },
};
type VoiceModel = keyof typeof models;

export async function voiceModel(): Promise<VoiceModel> {
  const file = Bun.file(settingsFile);
  if (!await file.exists()) return 'gigaam';
  const value = await file.text();
  if (!Object.hasOwn(models, value)) throw new Error('Неизвестная модель распознавания речи');
  return value as VoiceModel;
}

export async function setVoiceModel(value: string) {
  if (!Object.hasOwn(models, value)) throw new Error('Неизвестная модель распознавания речи');
  await Bun.write(settingsFile, value);
}
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
    const name = await voiceModel();
    const model = models[name];
    const paths = model.files.map(file => join(folder, name, file));
    if (!(await Promise.all([join(folder, 'sherpa-onnx-offline'), ...paths].map(path => Bun.file(path).exists()))).every(Boolean)) throw new Error(`Модель ${name} не установлена. Запустите bash ~/.secretary/current/scripts/install-voice.sh`);
    const process = Bun.spawn([join(folder, 'sherpa-onnx-offline'), ...paths.map((path, i) => `--${['tokens', 'encoder', 'decoder', 'joiner'][i]}=${path}`), ...model.args, `--num-threads=${Math.min(4, availableParallelism())}`, '--debug=false', file], { stdout: 'pipe', stderr: 'pipe' });
    const [stdout, stderr, status] = await Promise.all([new Response(process.stdout).text(), new Response(process.stderr).text(), process.exited]);
    if (status !== 0) throw new Error(stderr.trim() || 'Ошибка sherpa-onnx');
    const text = JSON.parse(stdout.trim()).text?.trim();
    if (!text) throw new Error('Речь не распознана');
    return text;
  } finally { await rm(dir, {recursive:true, force:true}); }
}
