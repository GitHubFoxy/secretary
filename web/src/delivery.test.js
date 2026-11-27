import { test, expect } from 'bun:test';
import { createDelivery } from './delivery.js';

test('сохраняет порядок внутри Session и независимость разных Session', async () => {
  let release;
  const ready = new Promise(resolve => { release = resolve; });
  const requests = [];
  const delivery = createDelivery({request: async value => requests.push(value), fetch: async () => {}, onChange: () => {}, revoke: () => {}});
  const first = delivery.send({id:'1', sessionId:'a', text:'first', images:[{ready}]});
  const second = delivery.send({id:'2', sessionId:'a', text:'second', images:[], immediate:true});
  const third = delivery.send({id:'3', sessionId:'b', text:'third', images:[]});
  await third;
  expect(requests.map(r => r.text)).toEqual(['third']);
  release({frames:[]});
  await Promise.all([first, second]);
  expect(requests.map(r => r.text)).toEqual(['third', 'first', 'second']);
  expect(requests[2].immediate).toBe(true);
});

test('связывает кадры с оригиналами и сохраняет ошибку разрыва для восстановления', async () => {
  const changes = [], revoked = [], requests = [];
  const delivery = createDelivery({request: async value => { requests.push(value); if (value.text === 'failed') throw Object.assign(new Error('lost'), {disconnected:true}); }, fetch: async (url, options) => ({ok:true, json:async () => ({id:options.body.name})}), onChange: items => changes.push(items), revoke: url => revoked.push(url)});
  const images = ['a', 'b'].map(name => ({url:name, ready:Promise.resolve({file:{name}, frames:[{data:name}], description:'Кадры по порядку:'})}));
  await delivery.send({id:'1', sessionId:'a', text:'ok', images});
  expect(requests[0].images.map(f => f.originalId)).toEqual(['a', 'b']);
  expect(requests[0].text).toContain('начиная с изображения 2');
  expect(revoked).toEqual(['a', 'b']);
  await delivery.send({id:'2', sessionId:'a', text:'failed', images:[]});
  expect(changes.at(-1)[0].error).toContain('Проверьте историю');
  expect(delivery.restore('2').text).toBe('failed');
  expect(changes.at(-1)).toEqual([]);
});
