import { test, expect } from 'bun:test';
import { activityStatus } from './activity-status.js';

test('статус отражает последнее событие только во время хода', () => {
  const history = [{kind:'step', tool:'Bash', summary:'bun test\n--watch'}, {kind:'thinking', text:'мысль'}];
  expect(activityStatus('running', history)).toBe('Думает…');
  expect(activityStatus('running', history.slice(0, 1))).toBe('Bash: bun test --watch');
  expect(activityStatus('running', [{kind:'step', tool:'Read', summary:'server/chat.ts'}])).toBe('Читает server/chat.ts');
  expect(activityStatus('running', [...history, {kind:'writing'}])).toBe('Пишет ответ…');
  expect(activityStatus('idle', history)).toBe('idle');
  expect(activityStatus('running', [{kind:'user'}])).toBe('Думает…');
});
