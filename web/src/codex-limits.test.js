import { expect, test } from 'bun:test';
import { blockedBySpentWeek } from './codex-limits.js';

const now = 1_000_000_000_000;
const at = hours => now / 1000 + hours * 3600;

test('исчерпанная неделя закрывает свободное 5-часовое окно до своего сброса', () => {
  const [session] = blockedBySpentWeek({ primary: { usedPercent: 0, windowDurationMins: 300, resetsAt: at(5) }, secondary: { usedPercent: 100, windowDurationMins: 10080, resetsAt: at(8) } }, now);
  expect(session).toMatchObject({ usedPercent: 100, resetsAt: at(8) });
});

test('исчерпанное 5-часовое окно, которое сбрасывается позже недели, держит свой сброс', () => {
  const [session] = blockedBySpentWeek({ primary: { usedPercent: 100, windowDurationMins: 300, resetsAt: at(9) }, secondary: { usedPercent: 100, windowDurationMins: 10080, resetsAt: at(8) } }, now);
  expect(session.resetsAt).toBe(at(9));
});

test('без исчерпанной недели окна не меняются', () => {
  const primary = { usedPercent: 10, windowDurationMins: 300, resetsAt: at(5) };
  expect(blockedBySpentWeek({ primary, secondary: { usedPercent: 99, windowDurationMins: 10080, resetsAt: at(8) } }, now)[0]).toBe(primary);
});
