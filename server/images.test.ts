import { expect, test } from 'bun:test';
import { validateImages } from './images';

test('проверяет формат, содержимое, размер и число изображений', () => {
  const png = {type:'image/png', data:Buffer.from([137,80,78,71,13,10,26,10]).toString('base64')};
  expect(validateImages([png])).toEqual([png]);
  const webp = {type:'image/webp', data:Buffer.from('RIFF0000WEBP').toString('base64')};
  expect(validateImages([webp])).toEqual([webp]);
  const frame = {...webp, video:{seconds:60, time:59}};
  expect(validateImages(Array(60).fill(frame))).toHaveLength(60);
  expect(validateImages([frame])[0]).toEqual(frame);
  const note = {...frame, video:{...frame.video, note:true}};
  expect(validateImages([note])[0]).toEqual(note);
  expect(() => validateImages([{...frame, video:{...frame.video, note:'yes'}}])).toThrow();
  expect(() => validateImages([{...webp, video:{seconds:61, time:0}}])).toThrow();
  expect(validateImages(undefined)).toEqual([]);
  expect(() => validateImages([{...png, type:'image/jpeg'}])).toThrow();
  expect(() => validateImages([{type:'image/png', data:'bad!'}])).toThrow();
  expect(() => validateImages(Array(64).fill(png))).toThrow();
  expect(() => validateImages([{...png, data:Buffer.alloc(5 * 1024 * 1024 + 1).toString('base64')}])).toThrow();
});


test('изображения и кадры сохраняют ссылки на разные оригиналы, старые изображения остаются допустимы', () => {
  const data = Buffer.from([255,216,255,1]).toString('base64');
  const first = crypto.randomUUID(), second = crypto.randomUUID();
  const images = [
    {type:'image/jpeg', data, originalId:first},
    {type:'image/jpeg', data, originalId:second, video:{seconds:2, time:0}},
    {type:'image/jpeg', data},
  ];
  expect(validateImages(images)).toEqual(images);
  expect(() => validateImages([{...images[0], originalId:'not-an-id'}])).toThrow();
});
