import { expect, test } from 'bun:test';
import { parseOpencodeLimits } from './opencode-limits';

test('лимиты Go разбираются в три окна', () => {
  expect(parseOpencodeLimits({ usage: {
    rolling: { status: 'ok', percent: 62, resetsAt: '2026-11-27T11:54:01.000Z' },
    weekly: { status: 'ok', percent: 25, resetsAt: '2026-11-30T00:00:00.000Z' },
    monthly: { status: 'rate-limited', percent: 100, resetsAt: '2026-12-27T06:48:20.000Z' },
  } })).toEqual({ windows: [
    { id: 'rolling', percent: 62, resetsAt: '2026-11-27T11:54:01.000Z', limited: false },
    { id: 'weekly', percent: 25, resetsAt: '2026-11-30T00:00:00.000Z', limited: false },
    { id: 'monthly', percent: 100, resetsAt: '2026-12-27T06:48:20.000Z', limited: true },
  ] });
});

test('битый ответ отклоняется', () => {
  expect(() => parseOpencodeLimits({})).toThrow();
  expect(() => parseOpencodeLimits({ usage: { rolling: { status: 'ok' } } })).toThrow();
});
