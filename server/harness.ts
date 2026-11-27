import type { Image } from './images';

// One turn of a harness. Session owns the queue and history; the adapter only speaks the harness protocol.
// Contract, in order: onSessionId and onEvent any time; onDispatching right before the message is written to the
// harness (from then on a retry could run it twice); onAccepted at most once, when the harness has the message.
// Resolves true when the turn completed successfully.
export type TurnOptions = {
  folder: string; model: string; reasoning: string | null; resumeId: string | null; compact?: boolean;
  secretary?: boolean; mcpRoot?: string; mcpRole?: 'secretary' | 'specialist'; mcpUrl?: string; mcpToken?: string; systemPrompt?: string;
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
export function spawnHarness(args: string[], cwd: string, { interrupt, stop }: Pick<TurnOptions, 'interrupt' | 'stop'>, env: NodeJS.ProcessEnv = process.env) {
  const child = Bun.spawn(process.platform === 'linux' ? ['setsid', ...args] : args, { cwd, env, stdin: 'pipe', stdout: 'pipe', stderr: 'pipe' });
  // The journal keeps the whole stderr; the owner sees its end when the harness dies.
  let stderrTail = '';
  const decoder = new TextDecoder();
  const stderr = (async () => { for await (const chunk of child.stderr) { process.stderr.write(chunk); stderrTail = (stderrTail + decoder.decode(chunk, { stream: true })).slice(-2000); } })();
  let closed = false;
  let deadline: Timer | undefined;
  // After the harness exits its process group lives on while any tool it started does, and Linux does not reuse
  // a pid that is still a process group id, so the group kill below still reaches leftovers and nothing else.
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
    async failure(name: string) {
      // A tool the harness started may still hold stderr open, so the tail is read only briefly after exit.
      await child.exited; await Promise.race([stderr, Bun.sleep(200)]);
      return new Error(`${name} завершился с кодом ${child.exitCode}${stderrTail.trim() ? `: ${stderrTail.trim()}` : ''}`);
    },
    async close() {
      clearTimeout(deadline);
      kill();
      closed = true;
      await child.exited;
    },
  };
}

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
