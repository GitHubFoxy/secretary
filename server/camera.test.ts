import { expect, test } from 'bun:test';
import { mkdtemp, readFile, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { cameraRequests } from './camera';

test('кадр приходит только от клиента с камерой и сохраняется на Host', async () => {
  const root = await mkdtemp(join(tmpdir(), 'camera-'));
  try {
    const camera = cameraRequests(root, 100);
    let id = '';
    const client = (message: any) => { id = message.id; };
    const other = () => {};
    expect(() => camera.request()).toThrow('Камера не включена');
    camera.set(client, true);
    const result = camera.request();
    const data = Buffer.from([255,216,255,1,2,3]).toString('base64');
    await camera.reply(other, id, {type:'image/jpeg', data}, null);
    await camera.reply(client, id, {type:'image/jpeg', data}, null);
    expect(await readFile(await result)).toEqual(Buffer.from(data, 'base64'));
    const interrupted = camera.request();
    camera.remove(client);
    await expect(interrupted).rejects.toThrow('Камера не включена');
  } finally { await rm(root, {recursive:true, force:true}); }
});
