import { expect, test } from 'bun:test';
import { validateImages } from './images';

test('проверяет формат, содержимое, размер и число изображений', () => {
  const png = {type:'image/png', data:Buffer.from([137,80,78,71,13,10,26,10]).toString('base64')};
  expect(validateImages([png])).toEqual([png]);
  const webp = {type:'image/webp', data:Buffer.from('RIFF0000WEBP').toString('base64')};
  expect(validateImages([webp])).toEqual([webp]);
  expect(validateImages(undefined)).toEqual([]);
  expect(() => validateImages([{...png, type:'image/jpeg'}])).toThrow();
  expect(() => validateImages([{type:'image/png', data:'bad!'}])).toThrow();
  expect(() => validateImages(Array(5).fill(png))).toThrow();
  expect(() => validateImages([{...png, data:Buffer.alloc(5 * 1024 * 1024 + 1).toString('base64')}])).toThrow();
});
