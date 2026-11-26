import { test, expect } from 'bun:test';
import { sleepState } from './sleep-timer.js';
test('таймер использует абсолютное время и затухает за 10 секунд', () => {
  expect(sleepState(100000, 80000)).toEqual({ expired: false, volume: 1 });
  expect(sleepState(100000, 95000)).toEqual({ expired: false, volume: 0.5 });
  expect(sleepState(100000, 110000)).toEqual({ expired: true, volume: 0 });
  expect(sleepState(null, 110000)).toEqual({ expired: false, volume: 1 });
});
