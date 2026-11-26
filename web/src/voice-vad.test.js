import { expect, test } from 'bun:test';
import { initialVoiceActivity, voiceActivity, VAD } from './voice-vad.js';
function frames(state, rms, count, speaking = false) {
  for (let i = 0; i < count; i++) state = voiceActivity(state, rms, speaking);
  return state;
}
test('реплика заканчивается только после достаточной речи и паузы', () => {
  let state = frames(initialVoiceActivity(), 0.05, 11);
  state = frames(state, 0, 32);
  expect(state.end).toBe(false);
  state = frames(state, 0.05, 1);
  state = frames(state, 0, 31);
  expect(state.end).toBe(false);
  expect(frames(state, 0, 1).end).toBe(true);
});
test('перебивание требует непрерывной речи, порог при озвучке выше', () => {
  let state = frames(initialVoiceActivity(), VAD.floor * 1.5, 20, true);
  expect(state.interrupt).toBe(false);
  state = frames(state, 0.08, 9, true);
  expect(state.interrupt).toBe(false);
  expect(frames(state, 0.08, 1, true).interrupt).toBe(true);
  expect(frames(state, 0, 1, true).continuous).toBe(0);
});
test('уровень шума адаптируется к тихому фону', () => {
  const state = frames(initialVoiceActivity(), 0.01, 300);
  expect(state.noise).toBeGreaterThan(0.009);
  expect(frames(state, 0.02, 40).end).toBe(false);
});
