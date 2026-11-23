import { mkdir, open, rm } from 'node:fs/promises';
import { statSync } from 'node:fs';
import { randomUUID } from 'node:crypto';
import { join } from 'node:path';
import { secretaryFolder } from './config';

const maxSize = 25 * 1024 * 1024;
const fileIdPattern = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
export type FileAttachment = { file: true; id: string; name: string; size: number; type: string };

function safeName(name: string) {
  let safe = name.split(/[\\/]/).pop()!.replace(/\.\./g, '').replace(/[\x00-\x1f\x7f]/g, '').trim();
  while (Buffer.byteLength(safe) > 200) safe = Array.from(safe).slice(0, -1).join('');
  return !safe || safe === '.' ? 'file' : safe;
}

export function filePath(file: FileAttachment, root = secretaryFolder) {
  return join(root, 'files', file.id, file.name);
}

export async function saveFile(request: Request, root = secretaryFolder): Promise<FileAttachment> {
  const url = new URL(request.url);
  const name = safeName(url.searchParams.get('name') || 'file');
  const id = randomUUID();
  const file: FileAttachment = { file:true, id, name, size:0, type:request.headers.get('content-type') || 'application/octet-stream' };
  const declared = request.headers.get('content-length');
  if (declared !== null && (!/^\d+$/.test(declared) || Number(declared) > maxSize)) throw new Error('Файл превышает 25 МБ');
  const dir = join(root, 'files', id);
  await mkdir(join(root, 'files'), { recursive:true });
  await mkdir(dir);
  try {
    const output = await open(filePath(file, root), 'wx');
    try {
      if (request.body) for await (const chunk of request.body) {
        file.size += chunk.length;
        if (file.size > maxSize) throw new Error('Файл превышает 25 МБ');
        await output.writeFile(chunk);
      }
    } finally { await output.close(); }
    return file;
  } catch (error) { await rm(dir, {recursive:true, force:true}); throw error; }
}

export function validateFiles(value: unknown, root = secretaryFolder): FileAttachment[] {
  if (value === undefined) return [];
  if (!Array.isArray(value) || value.length > 4) throw new Error('Можно прикрепить не более 4 файлов');
  return value.map(file => {
    if (!file || file.file !== true || typeof file.id !== 'string' || !fileIdPattern.test(file.id) || typeof file.name !== 'string' || safeName(file.name) !== file.name || typeof file.type !== 'string' || file.type.length > 255 || !Number.isSafeInteger(file.size) || file.size < 0 || file.size > maxSize) throw new Error('Некорректное вложение');
    let stat;
    try { stat = statSync(filePath(file, root)); } catch { throw new Error('Файл не найден'); }
    if (!stat.isFile() || stat.size !== file.size) throw new Error('Некорректный размер файла');
    return {file:true, id:file.id, name:file.name, size:file.size, type:file.type};
  });
}

// Картинки и видео открываются в браузере, остальное скачивается: загруженный HTML или SVG
// не должен исполняться на адресе приложения, где есть вход.
export async function fileResponse(id: string, encodedName: string, root = secretaryFolder): Promise<Response | null> {
  let name: string;
  try { name = decodeURIComponent(encodedName); } catch { return null; }
  if (!fileIdPattern.test(id) || safeName(name) !== name) return null;
  const file = Bun.file(join(root, 'files', id, name));
  if (!(await file.exists())) return null;
  const inline = /^(image\/(?!svg)|video\/)/.test(file.type);
  return new Response(file, { headers: { 'Content-Type': file.type, 'Content-Disposition': inline ? 'inline' : 'attachment', 'X-Content-Type-Options': 'nosniff' } });
}
