import { test, expect } from 'bun:test';
import { mkdtemp, readFile, rm, readdir } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { fileResponse, filePath, saveFile, validateFiles } from './files';
import { chatMessage } from './chat';

test('сохраняет файлы с безопасными именами и проверяет метаданные', async () => {
  const root = await mkdtemp(join(tmpdir(), 'secretary-files-'));
  try {
    const request = () => new Request('http://localhost/files?name=..%2F..%2Ftest.pdf', {method:'POST', body:'PDF'});
    const first = await saveFile(request(), root);
    const second = await saveFile(request(), root);
    expect(first.name).toBe('test.pdf');
    expect(first.id).not.toBe(second.id);
    expect(await readFile(filePath(first, root), 'utf8')).toBe('PDF');
    expect(validateFiles([first], root)).toEqual([first]);
    expect(() => validateFiles([{...first, name:'../test.pdf'}], root)).toThrow();
    expect(() => validateFiles([{...first, size:4}], root)).toThrow();
    expect(() => validateFiles(Array(5).fill(first), root)).toThrow();
    expect(chatMessage({type:'user', message:{content:''}, images:[], files:[first]})).toEqual({kind:'user', text:'', images:[], files:[first]});
    expect((await saveFile(new Request('http://localhost/files?name=.', {method:'POST', body:'x'}), root)).name).toBe('file');
    const before = await readdir(join(root, 'files'));
    await expect(saveFile(new Request('http://localhost/files?name=big', {method:'POST', body:new Uint8Array(25 * 1024 * 1024 + 1)}), root)).rejects.toThrow();
    expect(await readdir(join(root, 'files'))).toEqual(before);
  } finally { await rm(root, {recursive:true, force:true}); }
});

test('передаёт файл обоим harness и сохраняет его в очереди и истории', async () => {
  const root = await mkdtemp(join(tmpdir(), 'secretary-session-files-'));
  try {
    const script = `
      import { saveFile, filePath } from './files';
      import { openDatabase } from './database';
      import { Sessions } from './sessions';
      import { chatHistory } from './chat';
      import { strict as assert } from 'node:assert';
      const file = await saveFile(new Request('http://localhost/files?name=test.pdf', {method:'POST', body:'PDF'}));
      const db = openDatabase(':memory:');
      const calls = [];
      const turn = (options, text) => new Promise(resolve => calls.push({options, text, resolve}));
      const config = {turns:{claude:turn, codex:turn}, checkModel:async () => {}, onEvent() {}, onChange() {}};
      const sessions = new Sessions(db, config); sessions.load('/tmp/secretary');
      for (const harness of ['claude', 'codex']) {
        const session = await sessions.create({harness, model:'m', reasoning:null, folder:'/tmp'});
        session.send('', undefined, false, false, undefined, [file]);
        const call = calls.at(-1);
        assert.equal(call.text, 'Файл: ' + filePath(file) + ' (test.pdf, 3 байт)');
        const restored = new Sessions(db, config); restored.load('/tmp/secretary');
        assert.deepEqual(restored.get(session.id).queue.list()[0].files, [file]);
        call.options.onDispatching(); call.options.onAccepted(); call.resolve(true);
        assert.deepEqual(chatHistory(db, session.id, 20)[0].files, [file]);
        await Bun.sleep(0);
      }
    `;
    const child = Bun.spawn(['bun', '-e', script], {cwd:import.meta.dir, env:{...process.env, HOME:root}, stdout:'pipe', stderr:'pipe'});
    const [status, stdout, stderr] = await Promise.all([child.exited, new Response(child.stdout).text(), new Response(child.stderr).text()]);
    expect({status, stdout, stderr}).toEqual({status:0, stdout:'', stderr:''});
  } finally { await rm(root, {recursive:true, force:true}); }
});

test('отдаёт сохранённый файл только по его uuid и имени', async () => {
  const root = await mkdtemp(join(tmpdir(), 'secretary-file-response-'));
  try {
    const photo = await saveFile(new Request('http://localhost/files?name=photo.jpg', {method:'POST', headers:{'Content-Type':'image/jpeg'}, body:'JPG'}), root);
    const page = await saveFile(new Request('http://localhost/files?name=page.html', {method:'POST', headers:{'Content-Type':'text/html'}, body:'<script>'}), root);
    const served = await fileResponse(photo.id, 'photo.jpg', root);
    expect(served?.headers.get('content-type')).toBe('image/jpeg');
    expect(served?.headers.get('content-disposition')).toBe('inline');
    expect(await served?.text()).toBe('JPG');
    expect((await fileResponse(page.id, 'page.html', root))?.headers.get('content-disposition')).toBe('attachment');
    expect(await fileResponse(photo.id, '..%2F..%2Fphoto.jpg', root)).toBeNull();
    expect(await fileResponse(photo.id, '..', root)).toBeNull();
    expect(await fileResponse('../' + photo.id, 'photo.jpg', root)).toBeNull();
    expect(await fileResponse(photo.id, 'other.jpg', root)).toBeNull();
    expect(await fileResponse(photo.id, '%E0%A4%A', root)).toBeNull();
  } finally { await rm(root, {recursive:true, force:true}); }
});
