import { homedir } from 'node:os';
import { join } from 'node:path';

const folder = join(homedir(), '.secretary', 'asr');
let process: ReturnType<typeof Bun.spawn> | undefined;
let nextId = 0;
const pending = new Map<number, (complete: boolean) => void>();

function start() {
  const child = Bun.spawn([join(folder, 'smart-turn-venv/bin/python'), '-B', join(import.meta.dir, 'smart_turn.py')], {stdin:'pipe', stdout:'pipe', stderr:'inherit'});
  process = child;
  (async () => {
    let buffer = '';
    for await (const chunk of child.stdout as ReadableStream<Uint8Array>) {
      buffer += new TextDecoder().decode(chunk, {stream:true});
      let end: number;
      while ((end = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
        try {
          const result = JSON.parse(line);
          if (result.error) console.error('Smart Turn:', result.error);
          else console.log('Smart Turn ms', result.ms);
          pending.get(result.id)?.(typeof result.complete === 'boolean' ? result.complete : true);
        } catch (error) { console.error('Smart Turn:', error); }
      }
    }
    if (process === child) process = undefined;
    for (const resolve of pending.values()) resolve(true);
  })().catch(error => { console.error('Smart Turn:', error); if (process === child) process = undefined; for (const resolve of pending.values()) resolve(true); });
  return child;
}

export async function turnComplete(wav: Uint8Array): Promise<boolean> {
  try {
    if (!await Bun.file(join(folder, 'smart-turn-venv/bin/python')).exists() || !await Bun.file(join(folder, 'smart-turn-v3.2-cpu.onnx')).exists()) return true;
    const child = process ?? start();
    const id = ++nextId;
    return await new Promise(resolve => {
      const timer = setTimeout(() => done(true), 2000);
      function done(value: boolean) { clearTimeout(timer); pending.delete(id); resolve(value); }
      pending.set(id, done);
      try { (child.stdin as import('bun').FileSink).write(JSON.stringify({id, wav:Buffer.from(wav).toString('base64')}) + '\n'); }
      catch { done(true); }
    });
  } catch (error) { console.error('Smart Turn:', error); return true; }
}
