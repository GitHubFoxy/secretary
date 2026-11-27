import { test, expect } from 'bun:test';
import { receiveImeImage } from './ime-image.js';

test('передаёт изображение в текущий Composer и освобождает временный файл', async () => {
  const added = [], done = [];
  const options = {fetchFile: async () => new Response(new Blob(['image'])), isFocused: () => true, add: file => added.push(file), done: id => done.push(id)};
  const data = JSON.stringify({id:'id', path:'/tmp/image', type:'image/png'});
  await receiveImeImage(data, options);
  expect(added).toHaveLength(1);
  expect(added[0].type).toBe('image/png');
  expect(done).toEqual(['id']);
  options.isFocused = () => false;
  await receiveImeImage(data, options);
  expect(added).toHaveLength(1);
  expect(done).toEqual(['id', 'id']);
  options.fetchFile = async () => { throw new Error('read failed'); };
  await expect(receiveImeImage(data, options)).rejects.toThrow('read failed');
  expect(done).toEqual(['id', 'id', 'id']);
});
