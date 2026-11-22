import { expect, test } from 'bun:test';

const source = await Bun.file(new URL('./prepare-image-worker.js', import.meta.url)).text();

function worker({width = 6000, height = 4000, fail = false} = {}) {
  let output, dimensions, closed = false;
  const bitmap = {width, height, close:() => { closed = true; }};
  class Canvas {
    constructor(width, height) { dimensions = [width, height]; }
    getContext() { return {drawImage() {}}; }
    async convertToBlob() { return {type:'image/webp', size:12}; }
  }
  class Reader {
    readAsDataURL() { this.result = 'data:image/webp;base64,dGVzdA=='; this.onload(); }
  }
  const run = new Function('createImageBitmap', 'OffscreenCanvas', 'FileReader', 'postMessage', `let onmessage; ${source}; return onmessage;`)(
    async () => { if (fail) throw new Error('decode'); return bitmap; }, Canvas, Reader, data => { output = data; },
  );
  return {run, result:() => ({output, dimensions, closed})};
}

test('сжимает большое изображение в WebP и освобождает bitmap', async () => {
  const image = worker();
  await image.run({data:{}});
  expect(image.result()).toEqual({output:{type:'image/webp', data:'dGVzdA=='}, dimensions:[2048, 1365], closed:true});
});

test('сообщает об ошибке, если браузер не может прочитать изображение', async () => {
  const image = worker({fail:true});
  await image.run({data:{}});
  expect(image.result().output.error).toContain('Не удалось обработать изображение');
});
