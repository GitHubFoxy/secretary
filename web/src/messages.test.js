import { expect, test } from 'bun:test';
import { visibleMessage, userMessage } from './messages.js';

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
