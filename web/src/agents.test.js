import { expect, test } from 'bun:test';
import { mergeSwitches, modelLabel } from './agents.js';

test('названия моделей читаются как у людей', () => {
  expect(modelLabel('claude-opus-5-5')).toBe('Opus 5.5');
  expect(modelLabel('claude-haiku-4-5-20251001')).toBe('Haiku 4.5');
  expect(modelLabel('opus')).toBe('Opus');
  expect(modelLabel('gpt-6.2-sol')).toBe('GPT 6.2 Sol');
});

test('переключения подряд склеиваются, а возврат к тем же настройкам исчезает', () => {
  const a = { harness: 'claude', model: 'opus', reasoning: null }, b = { harness: 'codex', model: 'gpt', reasoning: null }, c = { ...b, reasoning: 'high' };
  const user = { kind: 'user', text: 'x' };
  expect(mergeSwitches([{ kind: 'switch', from: a, to: b }, { kind: 'switch', from: b, to: c }, user])).toEqual([{ kind: 'switch', from: a, to: c }, user]);
  expect(mergeSwitches([user, { kind: 'switch', from: a, to: b }, { kind: 'switch', from: b, to: a }])).toEqual([user]);
});
