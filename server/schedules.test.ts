import { test, expect } from 'bun:test';
import { openDatabase } from './database';
import { MessageQueue } from './queue';
import { nextCron, Schedules } from './schedules';
import type { Sessions } from './sessions';

test('cron проверяет поля и находит следующее местное время', () => {
  const start = new Date(2026, 0, 1, 10, 0).getTime();
  expect(nextCron('*/15 10-12 * * 1,2,3,4,5', start)).toBe(new Date(2026, 0, 1, 10, 15).getTime());
  expect(() => nextCron('60 * * * *', start)).toThrow();
  expect(() => nextCron('* * *', start)).toThrow();
});

test('простой объединяет пропуски, разовое расписание срабатывает один раз', () => {
  const db = openDatabase(':memory:');
  const queue = new MessageQueue(db, 1, () => {});
  const schedules = new Schedules(db, { get: () => ({ queue, resumePending() {} }) } as unknown as Sessions);
  const start = new Date(2026, 0, 1, 10, 0).getTime();
  schedules.add(1, 'повтор', '* * * * *', undefined, start);
  schedules.add(1, 'разово', undefined, new Date(start + 60000).toISOString(), start);
  schedules.tick(start + 3600000);
  schedules.tick(start + 3600000);
  expect(queue.list()).toHaveLength(2);
  expect(queue.list()[0].text).toContain('[по расписанию]');
  const rows = schedules.list([1]);
  expect(rows[0].next_run).toBe(start + 3660000);
  expect(rows[1].next_run).toBeNull();
  schedules.remove(rows[0].id, [1]);
  expect(schedules.list([1])).toHaveLength(1);
  db.close();
});
