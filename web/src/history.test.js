import { expect, test } from 'bun:test';
import { mergeHistory, insertVoices } from './history.js';

const messages = (...ids) => ids.map(cursor => ({cursor, text:`сообщение ${cursor}`}));

test('при пересечении сохраняет открытую историю и добавляет пропущенные сообщения', () => {
  expect(mergeHistory(messages(1, 2, 3), messages(2, 3, 4), 3)).toEqual({messages:messages(1, 2, 3, 4), overlap:true});
});

test('после большого пропуска показывает новую страницу и сохраняет события, пришедшие во время запроса', () => {
  expect(mergeHistory(messages(1, 2, 8), messages(5, 6, 7), 2)).toEqual({messages:messages(5, 6, 7, 8), overlap:false});
});

test('голосовые сохраняют место при поступлении и подгрузке сообщений', () => {
  const voices = [{key:'v1', afterCursor:2}, {key:'v2', afterCursor:2}];
  expect(insertVoices(messages(1, 2, 3, 4), voices).map(m => m.key ?? m.cursor)).toEqual([1, 2, 'v1', 'v2', 3, 4]);
  expect(insertVoices(messages(3, 4), voices).map(m => m.key ?? m.cursor)).toEqual(['v1', 'v2', 3, 4]);
});
