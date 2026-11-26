import { constants } from 'node:fs';
import { mkdir, open, readdir, rm, readFile, writeFile, lstat } from 'node:fs/promises';
import { randomBytes } from 'node:crypto';
import { basename, join } from 'node:path';
import { secretaryFolder, previewPort, previewUrl } from './config';

const lifetime = 24 * 60 * 60 * 1000;
const idPattern = /^[a-f0-9]{32}$/;
const maxSize = 100 * 1024 * 1024;

export async function publishHtml(path: string, root = secretaryFolder, base = previewUrl) {
  if (!base) throw new Error('Публикация не настроена');
  const url = new URL(base);
  if (url.protocol !== 'https:' || url.username || url.password || url.pathname !== '/' || url.search || url.hash || url.hostname === 'app.scrtrbot.ru') throw new Error('SECRETARY_PREVIEW_URL должен быть отдельным HTTPS origin');
  const id = randomBytes(16).toString('hex');
  const folder = join(root, 'preview');
  const temp = join(folder, `.${id}`);
  const dir = join(folder, id);
  let count = 0, size = 0;
  async function copy(source: string, target: string) {
    const handle = await open(source, constants.O_RDONLY | constants.O_NOFOLLOW | constants.O_NONBLOCK);
    try {
      const info = await handle.stat();
      if (info.isDirectory()) {
        await mkdir(target);
        // Read children through the open directory descriptor so a replaced source path cannot escape it.
        for (const name of await readdir(`/proc/self/fd/${handle.fd}`)) {
          if (name.startsWith('.')) throw new Error('Скрытые файлы и каталоги запрещены');
          await copy(`/proc/self/fd/${handle.fd}/${name}`, join(target, name));
        }
      } else if (info.isFile()) {
        if (++count > 1000 || (size += info.size) > maxSize) throw new Error('Превышен лимит: 1000 файлов или 100 МиБ');
        const data = await handle.readFile();
        if (data.length !== info.size) throw new Error('Файл изменился при копировании');
        await writeFile(target, data, { flag: 'wx' });
      } else throw new Error('Разрешены только обычные файлы и каталоги');
    } finally { await handle.close(); }
  }
  await mkdir(folder, { recursive: true });
  try {
    const info = await lstat(path);
    if (info.isDirectory()) {
      await copy(path, temp);
      if (!(await lstat(join(temp, 'index.html')).catch(() => null))?.isFile()) throw new Error('В каталоге нужен index.html');
    } else {
      if (!basename(path).toLowerCase().endsWith('.html')) throw new Error('Нужен HTML-файл');
      await mkdir(temp);
      await copy(path, join(temp, 'index.html'));
    }
    const expiresAt = new Date(Date.now() + lifetime).toISOString();
    await writeFile(join(temp, '.expires'), expiresAt, { flag: 'wx' });
    const { rename } = await import('node:fs/promises');
    await rename(temp, dir);
    return { url: `${url.origin}/${id}/`, expires_at: expiresAt };
  } catch (error) { await rm(temp, { recursive: true, force: true }); throw error; }
}

export async function cleanPreviews(root = secretaryFolder) {
  const folder = join(root, 'preview');
  await mkdir(folder, { recursive: true });
  for (const id of await readdir(folder)) {
    if (!idPattern.test(id)) continue;
    const expiry = Date.parse((await readFile(join(folder, id, '.expires'), 'utf8').catch(() => '')).trim());
    if (!Number.isFinite(expiry) || expiry <= Date.now()) await rm(join(folder, id), { recursive: true, force: true });
  }
}

export async function previewResponse(req: Request, root = secretaryFolder) {
  const missing = () => new Response('Not found', { status: 404 });
  if (req.method !== 'GET' && req.method !== 'HEAD') return missing();
  // Inspect the raw URL before URL normalisation removes dot segments.
  const raw = req.url.split('?')[0].replace(/^https?:\/\/[^/]+/, '');
  if (/%(?:2e|2f|5c)/i.test(raw)) return missing();
  let parts: string[];
  try { parts = raw.split('/').slice(1).map(decodeURIComponent); } catch { return missing(); }
  const id = parts.shift() || '';
  if (!idPattern.test(id) || parts.some(p => p.startsWith('.') || p.includes('/') || p.includes('\\'))) return missing();
  if (parts.at(-1) === '') parts[parts.length - 1] = 'index.html';
  if (!parts.length || parts.some(p => !p)) return missing();
  try {
    const folder = join(root, 'preview', id);
    const expiry = Date.parse((await readFile(join(folder, '.expires'), 'utf8')).trim());
    if (!Number.isFinite(expiry) || expiry <= Date.now()) return missing();
    let current = folder;
    for (const part of parts) {
      current = join(current, part);
      if ((await lstat(current)).isSymbolicLink()) return missing();
    }
    if (!(await lstat(current)).isFile()) return missing();
    const file = Bun.file(current);
    return new Response(req.method === 'HEAD' ? null : file, { headers: { 'Content-Type': file.type || 'application/octet-stream', 'X-Content-Type-Options': 'nosniff', 'Cache-Control': 'no-store', 'Referrer-Policy': 'no-referrer', 'Content-Security-Policy': "default-src 'self' https: data: blob: 'unsafe-inline' 'unsafe-eval'; frame-ancestors 'none'; base-uri 'self'" } });
  } catch { return missing(); }
}

export function servePreview(root = secretaryFolder, port = previewPort) {
  void cleanPreviews(root).catch(console.error);
  const timer = setInterval(() => void cleanPreviews(root).catch(console.error), 60 * 60 * 1000);
  timer.unref();
  return Bun.serve({ hostname: '127.0.0.1', port, fetch: req => previewResponse(req, root) });
}
