import { expect, test } from 'bun:test';
import { parseVoice, voiceText } from './voice.js';

test('voice transcript keeps its duration and text', () => {
  expect(parseVoice(voiceText('Запусти тесты', 65.4))).toEqual({duration: '1:05', text: 'Запусти тесты'});
  expect(parseVoice('Обычное сообщение')).toBe(null);
});
