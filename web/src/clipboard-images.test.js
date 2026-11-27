import { expect, test } from 'bun:test';
import { clipboardImages } from './clipboard-images.js';

test('reads images from files or file items without duplicating them', () => {
  const image = new File(['image'], 'image.png', {type:'image/png'});
  const item = {kind:'file', type:'image/png', getAsFile:() => image};
  expect(clipboardImages({files:[image], items:[item]})).toEqual([image]);
  expect(clipboardImages({files:[], items:[item]})).toEqual([image]);
  expect(clipboardImages({files:[], items:[{...item, getAsFile:() => null}, {kind:'string', type:'text/plain'}]})).toEqual([]);
  expect(clipboardImages(null)).toEqual([]);
});
