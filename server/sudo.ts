type Result = { status: string; exitCode?: number; output?: string };
type Runner = (command: string, password: string, cwd: string) => Promise<{ exitCode: number; output: string }>;

async function run(command: string, password: string, cwd: string) {
  const process = Bun.spawn(['sudo', '-S', '-k', '-p', '', 'sh', '-c', command], { cwd, stdin: 'pipe', stdout: 'pipe', stderr: 'pipe' });
  process.stdin.write(password + '\n');
  process.stdin.end();
  const read = async (stream: ReadableStream<Uint8Array>) => {
    const reader = stream.getReader();
    const decoder = new TextDecoder();
    let text = '';
    while (true) {
      const { value, done } = await reader.read();
      if (done) break;
      if (text.length < 16000) text += decoder.decode(value, { stream: true }).slice(0, 16000 - text.length);
    }
    return text;
  };
  const [stdout, stderr, exitCode] = await Promise.all([read(process.stdout), read(process.stderr), process.exited]);
  return { exitCode, output: (stdout + stderr).slice(0, 16000).split(password).join('[скрыто]') };
}

export class Sudo {
  private pending = new Map<string, { command: string; cwd: string; timer: ReturnType<typeof setTimeout>; finish: (result: Result) => void }>();
  constructor(private emit: (sessionId: number, event: object) => void, private runner: Runner = run, private timeout = 600_000) {}

  request(sessionId: number, command: string, reason: string, cwd: string): Promise<Result> {
    const id = crypto.randomUUID();
    return new Promise(resolve => {
      const finish = (result: Result) => {
        clearTimeout(timer);
        this.pending.delete(id);
        this.emit(sessionId, { type: 'sudo_result', id, status: result.status });
        resolve(result);
      };
      const timer = setTimeout(() => { if (this.pending.has(id)) finish({ status: 'timeout' }); }, this.timeout);
      this.pending.set(id, { command, cwd, timer, finish });
      this.emit(sessionId, { type: 'sudo_request', id, command, reason });
    });
  }

  async approve(id: string, password: string) {
    const request = this.pending.get(id);
    if (!request) throw new Error('Запрос уже завершён или не найден');
    if (typeof password !== 'string' || !password || /[\r\n]/.test(password)) throw new Error('Некорректный пароль');
    this.pending.delete(id);
    clearTimeout(request.timer);
    try {
      const result = await this.runner(request.command, password, request.cwd);
      request.finish({ status: 'completed', exitCode: result.exitCode, output: result.output.slice(0, 16000).split(password).join('[скрыто]') });
    } catch {
      request.finish({ status: 'error' });
    }
  }

  reject(id: string) {
    const request = this.pending.get(id);
    if (!request) throw new Error('Запрос уже завершён или не найден');
    request.finish({ status: 'rejected' });
  }
}
