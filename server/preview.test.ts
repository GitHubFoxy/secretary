import { test, expect } from 'bun:test';
import { mkdtemp, mkdir, readFile, rm, symlink, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { cleanPreviews, previewResponse, publishHtml } from './preview';

test('публикация сохраняет снимок и отдаёт ресурсы до истечения срока', async () => {
  const root = await mkdtemp(join(tmpdir(), 'preview-test-'));
  try {
    const source = join(root, 'source');
    await mkdir(source);
    await writeFile(join(source, 'index.html'), '<script src="app.js"></script>');
    await writeFile(join(source, 'app.js'), 'console.log(1)');
    const result = await publishHtml(source, root, 'https://preview.example.com');
    const id = new URL(result.url).pathname.split('/')[1];
    await writeFile(join(source, 'app.js'), 'changed');
    const page = await previewResponse(new Request(result.url), root);
    expect(page.status).toBe(200);
    expect(page.headers.get('content-security-policy')).toContain("'unsafe-inline'");
    expect(page.headers.get('set-cookie')).toBeNull();
    expect(await (await previewResponse(new Request(result.url + 'app.js'), root)).text()).toBe('console.log(1)');
    expect((await previewResponse(new Request(result.url + '.expires'), root)).status).toBe(404);
    expect((await previewResponse(new Request(result.url, { method: 'POST' }), root)).status).toBe(404);
    await writeFile(join(root, 'preview', id, '.expires'), new Date(0).toISOString());
    expect((await previewResponse(new Request(result.url), root)).status).toBe(404);
    await cleanPreviews(root);
    expect(await readFile(join(root, 'preview', id, '.expires')).catch(() => null)).toBeNull();
  } finally { await rm(root, { recursive: true, force: true }); }
});

test('публикация отклоняет ненастроенный адрес и символические ссылки', async () => {
  const root = await mkdtemp(join(tmpdir(), 'preview-test-'));
  try {
    const html = join(root, 'page.html');
    await writeFile(html, 'hello');
    await expect(publishHtml(html, root, '')).rejects.toThrow('Публикация не настроена');
    const result = await publishHtml(html, root, 'https://preview.example.com');
    expect(await (await previewResponse(new Request(result.url), root)).text()).toBe('hello');
    const source = join(root, 'source');
    await mkdir(source);
    await symlink(html, join(source, 'index.html'));
    await expect(publishHtml(source, root, 'https://preview.example.com')).rejects.toThrow();
  } finally { await rm(root, { recursive: true, force: true }); }
});
