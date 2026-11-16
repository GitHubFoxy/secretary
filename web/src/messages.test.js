import { expect, test } from 'bun:test';
import { visibleMessage, userMessage, queuedMessage, keyedMessages } from './messages.js';

test('сообщение из очереди помечается и сохраняет срочность и изображения', () => {
  expect(queuedMessage({id:1, text:'жду', images:[], urgent:true})).toEqual({role:'В очереди', text:'жду', images:[], urgent:true});
  expect(queuedMessage({id:2, text:'', images:[{type:'image/png', data:'abc'}], urgent:false})).toMatchObject({role:'В очереди', images:[{type:'image/png', data:'abc'}], urgent:false});
  expect(visibleMessage(queuedMessage({id:3, text:'жду', images:[], urgent:false}))).toBe(true);
});

test('скрывает пустые ответы и ответы агента с NO_REPLY', () => {
  expect(visibleMessage({role:'Агент', text:''})).toBe(false);
  expect(visibleMessage({role:'Агент', text:'NO_REPLY причина'})).toBe(false);
  expect(visibleMessage({role:'Агент', text:'Важный отчёт'})).toBe(true);
  expect(visibleMessage({role:'Вы', text:'NO_REPLY'})).toBe(true);
});

test('распознаёт отчёт и сохраняет полный многострочный текст', () => {
  expect(userMessage('[report #5 codex /path with spaces] первая строка\nвторая')).toEqual({role:'Отчёт', id:5, harness:'codex', folder:'/path with spaces', text:'первая строка\nвторая'});
  expect(userMessage('обычное сообщение')).toEqual({role:'Вы', text:'обычное сообщение'});
});

test('показывает сообщение только с изображением', () => {
  expect(visibleMessage({role:'Вы', text:'', images:[{type:'image/png', data:'abc'}]})).toBe(true);
});

test('ключи истории устойчивы при добавлении событий и различаются для чатов и очереди', () => {
  const a = { text: 'первый' }, b = { text: 'второй' }, hidden = { text: '' };
  const convert = event => ({ role: 'Вы', text: event.text });
  const original = keyedMessages([hidden, a], [{id: 10, text:'жду'}, {id: 11, text:'жду'}], 1, convert);
  const updated = keyedMessages([hidden, a, b], [{id: 11, text:'жду'}, {id: 10, text:'жду'}], 1, convert);
  expect(original[0].key).toBe('1:event:1');
  expect(updated[0].key).toBe(original[0].key);
  expect(updated[2].key).toBe(original[2].key);
  expect(updated[3].key).toBe(original[1].key);
  expect(keyedMessages([hidden, a], [], 2, convert)[0].key).not.toBe(original[0].key);
});
