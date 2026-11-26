import { test, expect } from 'bun:test';
import { trimSilence } from './voice.js';
test('trims silence around and between tones', () => {
  const rate = 16000, samples = new Float32Array(rate * 5);
  for (const start of [1, 3]) for (let i = start * rate; i < (start + 1) * rate; i++) samples[i] = 0.2 * Math.sin(i * 0.1);
  expect(trimSilence(samples).length / rate).toBeCloseTo(2.6, 1);
  expect(trimSilence(new Float32Array(rate)).length).toBe(0);
});
