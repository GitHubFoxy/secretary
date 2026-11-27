import { createHash, randomUUID } from 'node:crypto';
import { lstatSync, mkdirSync, readFileSync, readdirSync, renameSync, unlinkSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';

const version = (text: string) => createHash('sha256').update(text).digest('hex');

export class Memory {
  private dir: string;
  constructor(folder: string) { this.dir = join(folder, 'memory'); }

  private path(name: string) {
    if (typeof name !== 'string' || !name.endsWith('.md') || name === '.md' || name.includes('..') || /[\\/\0]/.test(name)) throw new Error('Некорректное имя файла');
    const path = join(this.dir, name);
    try { if (!lstatSync(path).isFile()) throw new Error('Необычный файл памяти'); }
    catch (error: any) { if (error.code !== 'ENOENT') throw error; }
    return path;
  }

  list() {
    try { return readdirSync(this.dir, { withFileTypes: true }).filter(entry => entry.isFile() && entry.name.endsWith('.md') && !entry.name.includes('..')).map(entry => entry.name).sort(); }
    catch (error: any) { if (error.code === 'ENOENT') return []; throw error; }
  }

  read(name: string) {
    const path = this.path(name);
    let text: string;
    try { text = readFileSync(path, 'utf8'); }
    catch (error: any) { if (error.code !== 'ENOENT') throw error; text = ''; }
    return { text, version: version(text) };
  }

  write(name: string, text: string, expected: string) {
    const path = this.path(name);
    if (typeof text !== 'string' || typeof expected !== 'string') throw new Error('Некорректная запись');
    const current = this.read(name);
    if (current.version !== expected) throw new Error('Файл изменился. Откройте его заново.');
    mkdirSync(this.dir, { recursive: true });
    const temp = join(this.dir, `.${randomUUID()}.tmp`);
    try { writeFileSync(temp, text, { mode: 0o600, flag: 'wx' }); renameSync(temp, path); }
    finally { try { unlinkSync(temp); } catch (error: any) { if (error.code !== 'ENOENT') throw error; } }
    return { text, version: version(text) };
  }

  delete(name: string, expected: string) {
    const path = this.path(name);
    if (this.read(name).version !== expected) throw new Error('Файл изменился. Откройте его заново.');
    unlinkSync(path);
  }
}

export function memorySettings(request: Request, url: URL, memory: Memory) {
  try {
    const name = url.searchParams.get('name');
    if (request.method === 'GET') return Response.json(name === null ? memory.list() : memory.read(name));
    if (request.method === 'PUT' || request.method === 'DELETE') return request.json().then(data => {
      if (!name) throw new Error('Укажите имя файла');
      if (request.method === 'PUT') return Response.json(memory.write(name, data.text, data.version));
      memory.delete(name, data.version);
      return new Response(null, {status: 204});
    }).catch(error => new Response(String(error), {status: 400}));
    return new Response(null, {status: 405});
  } catch (error) { return new Response(String(error), {status: 400}); }
}
