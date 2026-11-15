import { expect, test } from 'bun:test';
import { visibleMessage } from './messages.js';

test('скрывает пустые ответы и ответы агента с NO_REPLY', () => {
  expect(visibleMessage({role:'Агент', text:''})).toBe(false);
  expect(visibleMessage({role:'Агент', text:'NO_REPLY причина'})).toBe(false);
  expect(visibleMessage({role:'Агент', text:'Важный отчёт'})).toBe(true);
  expect(visibleMessage({role:'Вы', text:'NO_REPLY'})).toBe(true);
});
