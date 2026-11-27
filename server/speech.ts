import { speechAnswer, visibleAnswer } from './chat';
import { Agent, request } from 'node:https';
import { join } from 'node:path';
import { secretaryFolder } from './config';
import { speechText, splitSentences } from './speech-text';

const settingsFile = join(secretaryFolder, 'tts-engine');
const keyFile = join(secretaryFolder, 'inworld-key');
const piperFolder = join(secretaryFolder, 'tts');
const agent = new Agent({ keepAlive: true, maxSockets: 1 });
type Engine = 'inworld' | 'piper';

export async function ttsEngine(): Promise<Engine> {
  const file = Bun.file(settingsFile);
  const value = await file.exists() ? (await file.text()).trim() : 'inworld';
  if (value !== 'inworld' && value !== 'piper') throw new Error('Неизвестный движок синтеза речи');
  return value;
}
export async function setTtsEngine(value: string) {
  if (value !== 'inworld' && value !== 'piper') throw new Error('Неизвестный движок синтеза речи');
  await Bun.write(settingsFile, value);
}

async function inworld(path: string, body?: object, signal?: AbortSignal) {
  const key = (await Bun.file(keyFile).text()).trim();
  return new Promise<import('node:http').IncomingMessage>((resolve, reject) => {
    const req = request({hostname:'api.inworld.ai', path, method:body ? 'POST' : 'GET', agent, signal,
      headers:{Authorization:`Basic ${key}`, ...(body ? {'Content-Type':'application/json'} : {})}}, resolve);
    req.on('error', reject);
    req.end(body ? JSON.stringify(body) : undefined);
  });
}
let warming: Promise<void> | undefined;
export function warmSpeech() {
  warming = ttsEngine().then(async engine => {
    if (engine !== 'inworld') return;
    const response = await inworld('/tts/v1/voices?filter=language%3Dru');
    for await (const _ of response) {};
    if (response.statusCode !== 200) throw new Error(`Inworld: HTTP ${response.statusCode}`);
  });
  warming.catch(error => console.error('Прогрев синтеза:', error));
  return warming;
}

async function synthesize(text: string, signal: AbortSignal, send: (data: string, rate: number) => void) {
  const engine = await ttsEngine();
  if (signal.aborted) return;
  if (engine === 'piper') {
    const model = join(piperFolder, 'ru_RU-dmitri-medium.onnx');
    if (!await Bun.file(join(piperFolder, 'piper/piper')).exists() || !await Bun.file(model).exists()) throw new Error('Piper не установлен. Запустите scripts/install-tts.sh на Host');
    const child = Bun.spawn([join(piperFolder, 'piper/piper'), '--model', model, '--output_raw'], {stdin:'pipe', stdout:'pipe', stderr:'pipe'});
    const abort = () => child.kill();
    signal.addEventListener('abort', abort, {once:true});
    try {
      child.stdin.write(text + '\n'); child.stdin.end();
      const errors = new Response(child.stderr).text();
      for await (const chunk of child.stdout) { if (!signal.aborted) send(Buffer.from(chunk).toString('base64'), 22050); }
      const status = await child.exited;
      if (status && !signal.aborted) throw new Error((await errors).trim() || 'Ошибка Piper');
    } finally { signal.removeEventListener('abort', abort); }
    return;
  }
  if (warming) { await warming.catch(() => {}); warming = undefined; }
  if (signal.aborted) return;
  const response = await inworld('/tts/v1/voice:stream', {text, voiceId:'Timofey', modelId:'inworld-tts-2-flash', audioConfig:{audioEncoding:'LINEAR16', sampleRateHertz:24000}}, signal);
  if (response.statusCode !== 200) { response.resume(); throw new Error(`Inworld: HTTP ${response.statusCode}`); }
  let buffer = '';
  const parse = (line: string) => {
    if (!line.trim()) return;
    const message = JSON.parse(line);
    if (message.error) throw new Error(message.error.message || 'Ошибка Inworld');
    if (!message.result?.audioContent) return;
    let bytes = Buffer.from(message.result.audioContent, 'base64');
    if (bytes.subarray(0, 4).toString() === 'RIFF') bytes = bytes.subarray(44);
    if (bytes.length && !signal.aborted) send(bytes.toString('base64'), 24000);
  };
  for await (const chunk of response) {
    buffer += chunk.toString();
    let end: number;
    while ((end = buffer.indexOf('\n')) >= 0) { parse(buffer.slice(0, end)); buffer = buffer.slice(end + 1); }
  }
  parse(buffer);
}

// A turn owns its synthesis queue, independently of the lifetime of the harness process.
export class Speech {
  private controller?: AbortController;
  private queue = Promise.resolve();
  constructor(private publish: (message: object) => void, private speak = synthesize) {}
  stop() { this.controller?.abort(); this.controller = undefined; }
  start() { this.stop(); this.controller = new AbortController(); this.queue = Promise.resolve(); }
  event(event: any) {
    if (!this.controller) return;
    const text = speechAnswer(event);
    if (!visibleAnswer(text)) return;
    const signal = this.controller.signal;
    const sentences = splitSentences(speechText(text));
    for (const sentence of sentences) {
      this.queue = this.queue.then(async () => {
        if (!signal.aborted) await this.speak(sentence, signal, (data, rate) => this.publish({type:'speech_audio', data, rate}));
      }).catch(error => { if (!signal.aborted) { this.publish({type:'speech_error', message:String(error)}); this.stop(); } });
    }
  }
  end(interrupted: boolean) {
    if (interrupted) { this.stop(); this.publish({type:'speech_cancel'}); return; }
    const signal = this.controller?.signal;
    this.queue.then(() => { if (signal && !signal.aborted) this.publish({type:'speech_done'}); });
  }
}
