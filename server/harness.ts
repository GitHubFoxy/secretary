import type { Subprocess } from 'bun';
import type { Image } from './images';

// One turn of a harness. Session owns the queue and history; the adapter only speaks the harness protocol.
export type TurnOptions = {
  folder: string; model: string; reasoning: string | null; resumeId: string | null; secretary: boolean;
  onSessionId: (id: string) => void;
  onEvent: (event: any) => void;
  onProcess: (child: Subprocess<'pipe', 'pipe', 'inherit'>) => void;
  onInterruptable: (interrupt: () => void) => void;
  // Called right before the message is handed to the harness: after this a retry could run it twice.
  onDispatching: () => void;
  // The harness has accepted the message.
  onAccepted: () => void;
  shouldCancel: () => boolean;
};
export type Turn = (options: TurnOptions, text: string, images: Image[]) => Promise<boolean>;

// On Linux setsid puts the harness in its own process group, so one signal stops it together with the tools it started.
// If Secretary itself dies, systemd kills whatever is left (secretary.service uses KillMode=control-group).
export function spawnHarness(args: string[], cwd: string) {
  return Bun.spawn(process.platform === 'linux' ? ['setsid', ...args] : args, { cwd, stdin: 'pipe', stdout: 'pipe', stderr: 'inherit' });
}

export function killHarness(child: Subprocess) {
  try {
    if (process.platform === 'linux') process.kill(-child.pid, 'SIGKILL');
    else child.kill('SIGKILL');
  } catch {}
}

// Reads JSON lines until the stream ends or onLine returns false.
export async function readJsonLines(stream: ReadableStream<Uint8Array>, onLine: (message: any) => void | boolean) {
  const decoder = new TextDecoder();
  let buffer = '';
  for await (const chunk of stream) {
    buffer += decoder.decode(chunk, { stream: true });
    let end: number;
    while ((end = buffer.indexOf('\n')) >= 0) {
      const line = buffer.slice(0, end);
      buffer = buffer.slice(end + 1);
      if (line.trim() && onLine(JSON.parse(line)) === false) return;
    }
  }
}
