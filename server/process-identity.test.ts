import { expect, test } from 'bun:test';
import { processAlive, processState } from './process-identity';

function missing(code: string) { return () => { throw Object.assign(new Error(code), {code}); }; }

test('не считает неопределённое состояние /proc подтверждённой смертью процесса', () => {
  expect(processAlive('123:456', missing('EACCES'), () => true, missing('ESRCH'))).toBe(true);
  expect(processState('123:456', missing('EACCES'), () => true, missing('ESRCH'))).toBe('unknown');
  expect(processAlive('123:456', missing('ENOENT'), () => false, missing('ESRCH'))).toBe(true);
  expect(processAlive('123:456', missing('ENOENT'), () => true, () => {})).toBe(true);
  expect(processAlive('123:456', missing('ENOENT'), () => true, missing('EPERM'))).toBe(true);
  expect(processAlive('123:456', missing('ENOENT'), missing('ESRCH'), missing('ESRCH'))).toBe(true);
  expect(processAlive('123:456', missing('ENOENT'), () => true, missing('ESRCH'))).toBe(false);
  expect(processState('123:456', missing('ENOENT'), () => true, missing('ESRCH'))).toBe('gone');
});

test('учитывает время запуска и состояние процесса', () => {
  const stat = (state: string, start: string) => () => `123 (agent) ${[state, ...Array(18).fill('0'), start].join(' ')}`;
  expect(processAlive('123:456', stat('S', '456'))).toBe(true);
  expect(processState('123:456', stat('S', '456'))).toBe('matching');
  expect(processAlive('123:456', stat('Z', '456'))).toBe(false);
  expect(processAlive('123:456', stat('S', '789'))).toBe(false);
  expect(processAlive('invalid')).toBe(true);
  expect(processAlive('0:456')).toBe(true);
  expect(processAlive('123:456', () => 'invalid stat')).toBe(true);
});
