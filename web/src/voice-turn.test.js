import { test, expect } from 'bun:test';
import { isTurnComplete } from './voice-turn.js';

test('учитывает решение Smart Turn', async () => {
  expect(await isTurnComplete(new Uint8Array(), async () => Response.json({complete:false}))).toBe(false);
  expect(await isTurnComplete(new Uint8Array(), async () => Response.json({complete:true}))).toBe(true);
});

test('при недоступности модели возвращается к правилу тишины', async () => {
  expect(await isTurnComplete(new Uint8Array(), async () => { throw Error('offline'); })).toBe(true);
  expect(await isTurnComplete(new Uint8Array(), async () => new Response('', {status:500}))).toBe(true);
  expect(await isTurnComplete(new Uint8Array(), async () => new Response('invalid'))).toBe(true);
});
