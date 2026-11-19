import { mcpUrl } from './mcp-address';
import type { Image } from './images';
import type { ThreadStartParams } from './protocol/v2/ThreadStartParams';
import type { TurnStartParams } from './protocol/v2/TurnStartParams';

// onAccepted вызывается, когда Codex ответил на turn/start: сообщение принято и ход создан.
type Options = { folder: string; model: string; reasoning: string | null; threadId: string | null; onThread: (id: string) => void; onEvent: (event: any) => void; onProcess?: (process: any) => void; onInterruptable?: (interrupt: () => void) => void; onAccepted?: () => void; onDispatching?: () => void; shouldCancel?: () => boolean; secretary?: boolean };

export async function codexTurn(options: Options, text: string, images: Image[] = []) {
  const args = ['codex', 'app-server'];
  if (options.secretary) args.push('-c', `mcp_servers.secretary.url="${mcpUrl}"`);
  const process = Bun.spawn(globalThis.process.platform === 'linux' ? ['setsid', ...args] : args, { cwd: options.folder, stdin: 'pipe', stdout: 'pipe', stderr: 'inherit' });
  options.onProcess?.(process);
  let id = 0;
  const pending = new Map<number, { resolve: (value: any) => void; reject: (error: Error) => void }>();
  let done!: (value: boolean) => void;
  const completed = new Promise<boolean>(resolve => { done = resolve; });
  const send = (message: object) => { process.stdin.write(JSON.stringify(message) + '\n'); process.stdin.flush(); };
  const request = (method: string, params: unknown): Promise<any> => new Promise((resolve, reject) => {
    const requestId = ++id;
    pending.set(requestId, { resolve, reject });
    send({ jsonrpc: '2.0', id: requestId, method, params });
  });
  const reading = (async () => {
    let buffer = '';
    for await (const chunk of process.stdout) {
      buffer += new TextDecoder().decode(chunk, { stream: true });
      let end: number;
      while ((end = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
        if (!line.trim()) continue;
        const message = JSON.parse(line);
        if (message.id !== undefined && !message.method) {
          const waiter = pending.get(message.id);
          pending.delete(message.id);
          if (message.error) waiter?.reject(new Error(JSON.stringify(message.error)));
          else waiter?.resolve(message.result);
        } else if (message.method) {
          options.onEvent(message);
          if (message.method === 'turn/completed') done(message.params.turn.status === 'completed');
        }
      }
    }
    for (const waiter of pending.values()) waiter.reject(new Error('Codex process exited'));
    done(false);
  })();
  try {
    await request('initialize', { clientInfo: { name: 'secretary', version: '0.1.0' }, capabilities: null });
    send({ jsonrpc: '2.0', method: 'initialized' });
    const start: ThreadStartParams = { cwd: options.folder, approvalPolicy: 'never', sandbox: 'danger-full-access', developerInstructions: options.secretary ? await Bun.file(`${options.folder}/AGENTS.md`).text() : undefined };
    const thread = options.threadId
      ? await request('thread/resume', { threadId: options.threadId, excludeTurns: true })
      : await request('thread/start', start);
    options.onThread(thread.thread.id);
    if (options.shouldCancel?.()) return false;
    const turn: TurnStartParams = { threadId: thread.thread.id, model: options.model, effort: options.reasoning, input: [{ type: 'text', text, text_elements: [] }, ...images.map(image => ({ type: 'image' as const, url: `data:${image.type};base64,${image.data}` }))] };
    options.onDispatching?.();
    const starting = request('turn/start', turn);
    const started = await starting;
    options.onAccepted?.();
    options.onInterruptable?.(() => { request('turn/interrupt', { threadId: thread.thread.id, turnId: started.turn.id }).catch(() => {}); });
    return await completed;
  } finally {
    process.kill();
    await reading;
    await process.exited;
  }
}
