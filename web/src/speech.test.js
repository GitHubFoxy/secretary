import { expect, test } from 'bun:test';
import { createSpeechPlayer } from './speech.js';

test('планирует PCM по порядку, сохраняет неполный сэмпл и останавливает звук', () => {
  const played = [], states = [];
  class FakeAudioContext {
    state = 'running'; currentTime = 1; destination = {};
    createBuffer(_, count, rate) { return {duration:count / rate, getChannelData:() => new Float32Array(count)}; }
    createBufferSource() {
      const source = {connect() {}, start:time => played.push({time, source}), stop() { source.stopped = true; }};
      return source;
    }
  }
  const player = createSpeechPlayer(value => states.push(value), FakeAudioContext);
  player.start();
  player.audio(btoa('\x00\x00\x01'), 2);
  player.audio(btoa('\x00\x00\x00'), 2);
  expect(played.map(item => item.time)).toEqual([1, 1.5]);
  player.done();
  expect(states.at(-1)).toBe(true);
  player.stop();
  expect(played.every(item => item.source.stopped)).toBe(true);
  expect(states.at(-1)).toBe(false);
  player.audio(btoa('\x00\x00'), 2);
  expect(played).toHaveLength(2);
});
