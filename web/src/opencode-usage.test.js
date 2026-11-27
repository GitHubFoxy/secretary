import { expect, test } from 'bun:test';
import { formatTokens } from './opencode-usage.js';

test('токены форматируются коротко по-русски', () => {
  expect(formatTokens(0)).toBe('0');
  expect(formatTokens(-5)).toBe('0');
  expect(formatTokens(950)).toBe('950');
  expect(formatTokens(1500)).toBe('1,5 тыс.');
  expect(formatTokens(2_500_000)).toBe('2,5 млн');
});
