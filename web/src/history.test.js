import { expect, test } from 'bun:test';
import { mergeHistory, insertVoices, mergeApprovals } from './history.js';

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


test('история связывает результат подтверждения с запросом по виду и id', () => {
  const history = [
    {kind:'secret', id:'1', reason:'ключ'},
    {kind:'sudo', id:'1', command:'whoami'},
    {kind:'secret', id:'1', status:'saved'},
    {kind:'skill', id:'2', name:'example'},
    {kind:'sudo', id:'1', status:'rejected'},
  ];
  expect(mergeApprovals(history)).toEqual([
    {kind:'secret', id:'1', reason:'ключ', status:'saved'},
    {kind:'sudo', id:'1', command:'whoami', status:'rejected'},
    {kind:'skill', id:'2', name:'example', status:undefined},
  ]);
});
