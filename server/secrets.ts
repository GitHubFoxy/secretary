import { mkdir, readFile, writeFile, rename } from 'node:fs/promises';
import { join } from 'node:path';
import { secretaryFolder } from './config';

export class Secrets {
  private pending = new Map<string, { name: string; finish: (result: string) => void }>();
  private writes = Promise.resolve();
  constructor(private root = secretaryFolder, private emit: (sessionId: number, event: object) => void = () => {}) {}

  private async key() {
    await mkdir(this.root, { recursive: true, mode: 0o700 });
    const path = join(this.root, 'secrets.key');
    try { await writeFile(path, crypto.getRandomValues(new Uint8Array(32)), { mode: 0o600, flag: 'wx' }); } catch (e: any) { if (e.code !== 'EEXIST') throw e; }
    return crypto.subtle.importKey('raw', await readFile(path), 'AES-GCM', false, ['encrypt', 'decrypt']);
  }

  async get(name: string): Promise<string | undefined> {
    let data: Record<string, { iv: number[]; value: number[] }>;
    try { data = JSON.parse(await readFile(join(this.root, 'secrets.json'), 'utf8')); } catch (e: any) { if (e.code === 'ENOENT') return undefined; throw e; }
    if (!Object.hasOwn(data, name)) return undefined;
    return new TextDecoder().decode(await crypto.subtle.decrypt({ name: 'AES-GCM', iv: new Uint8Array(data[name].iv) }, await this.key(), new Uint8Array(data[name].value)));
  }

  async set(name: string, value: string) {
    const work = this.writes.then(async () => {
      const key = await this.key();
      const path = join(this.root, 'secrets.json');
      let data: Record<string, unknown> = Object.create(null);
      try { data = JSON.parse(await readFile(path, 'utf8')); } catch (e: any) { if (e.code !== 'ENOENT') throw e; }
      const iv = crypto.getRandomValues(new Uint8Array(12));
      data[name] = { iv: Array.from(iv), value: Array.from(new Uint8Array(await crypto.subtle.encrypt({ name: 'AES-GCM', iv }, key, new TextEncoder().encode(value)))) };
      const tmp = `${path}.${crypto.randomUUID()}`;
      await writeFile(tmp, JSON.stringify(data), { mode: 0o600, flag: 'wx' });
      await rename(tmp, path);
    });
    this.writes = work.catch(() => {});
    await work;
  }

  async request(sessionId: number, name: string, reason: string) {
    if (typeof name !== 'string' || !/^[A-Za-z_][A-Za-z0-9_]*$/.test(name) || typeof reason !== 'string' || !reason.trim()) throw new Error('Укажите корректные name и reason');
    if (await this.get(name) !== undefined) return `Секрет ${name} уже сохранён`;
    const id = crypto.randomUUID();
    return new Promise<string>(resolve => {
      const timer = setTimeout(() => finish('Время ожидания истекло'), 600_000);
      const finish = (result: string) => { clearTimeout(timer); this.pending.delete(id); this.emit(sessionId, { type: 'secret_result', id, status: result }); resolve(result === 'saved' ? `Секрет ${name} сохранён` : result); };
      this.pending.set(id, { name, finish });
      this.emit(sessionId, { type: 'secret_request', id, name, reason });
    });
  }
  async approve(id: string, value: string) {
    const request = this.pending.get(id);
    if (!request) throw new Error('Запрос уже завершён или не найден');
    if (typeof value !== 'string' || !value) throw new Error('Введите секрет');
    await this.set(request.name, value);
    request.finish('saved');
  }
  reject(id: string) {
    const request = this.pending.get(id);
    if (!request) throw new Error('Запрос уже завершён или не найден');
    request.finish('Отклонено');
  }
}
