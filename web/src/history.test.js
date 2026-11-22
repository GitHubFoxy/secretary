import { expect, test } from 'bun:test';
import { mergeHistory } from './history.js';

const messages = (...ids) => ids.map(cursor => ({cursor, text:`сообщение ${cursor}`}));

test('при пересечении сохраняет открытую историю и добавляет пропущенные сообщения', () => {
  expect(mergeHistory(messages(1, 2, 3), messages(2, 3, 4), 3)).toEqual({messages:messages(1, 2, 3, 4), overlap:true});
});

test('после большого пропуска показывает новую страницу и сохраняет события, пришедшие во время запроса', () => {
  expect(mergeHistory(messages(1, 2, 8), messages(5, 6, 7), 2)).toEqual({messages:messages(5, 6, 7, 8), overlap:false});
});
