import type { Image } from './images';

// One turn of a harness. Session owns the queue and history; the adapter only speaks the harness protocol.
// Contract, in order: onSessionId and onEvent any time; onDispatching right before the message is written to the
// harness (from then on a retry could run it twice); onAccepted at most once, when the harness has the message.
// Resolves true when the turn completed successfully.
export type TurnOptions = {
  folder: string; model: string; reasoning: string | null; resumeId: string | null; secretary: boolean;
  // Aborted when the owner wants the turn to end. Before dispatch the adapter returns without sending;
  // after it, the adapter asks the harness to stop and kills it if it doesn't within the deadline.
  interrupt: AbortSignal;
  // Aborted to kill the harness at once.
  stop: AbortSignal;
  onSessionId: (id: string) => void;
  onEvent: (event: any) => void;
  onDispatching: () => void;
  onAccepted: () => void;
};
export type Turn = (options: TurnOptions, text: string, images: Image[]) => Promise<boolean>;

export const harnessTiming = { interruptDeadlineMs: 10_000 };

export function onAbort(signal: AbortSignal, callback: () => void) {
  if (signal.aborted) callback();
  else signal.addEventListener('abort', callback, { once: true });
}

// On Linux setsid puts the harness in its own process group, so one signal stops it together with the tools it started.
// If Secretary itself dies, systemd kills whatever is left (secretary.service uses KillMode=control-group).
export function spawnHarness(args: string[], cwd: string, { interrupt, stop }: Pick<TurnOptions, 'interrupt' | 'stop'>) {
  const child = Bun.spawn(process.platform === 'linux' ? ['setsid', ...args] : args, { cwd, env: process.env, stdin: 'pipe', stdout: 'pipe', stderr: 'inherit' });
  let closed = false;
  let deadline: Timer | undefined;
  const kill = () => {
    if (closed) return;
    try {
      if (process.platform === 'linux') process.kill(-child.pid, 'SIGKILL');
      else child.kill('SIGKILL');
    } catch {}
  };
  onAbort(stop, kill);
  onAbort(interrupt, () => { deadline = setTimeout(kill, harnessTiming.interruptDeadlineMs); });
  return {
    child,
    write(message: object) { try { child.stdin.write(JSON.stringify(message) + '\n'); child.stdin.flush(); } catch {} },
    async close() {
      clearTimeout(deadline);
      kill();
      closed = true;
      await child.exited;
    },
  };
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
