export const VAD = { window:25, speech:300, interrupt:250, silence:800, floor:0.012, noiseFactor:3, echoFactor:2, noiseRate:0.02 };

export function voiceActivity(state, rms, speaking) {
  const threshold = Math.max(VAD.floor, state.noise * VAD.noiseFactor) * (speaking ? VAD.echoFactor : 1);
  const active = rms > threshold;
  const noise = active || speaking ? state.noise : state.noise + (rms - state.noise) * VAD.noiseRate;
  const speech = active ? state.speech + VAD.window : state.speech;
  const continuous = active ? (speaking === state.speaking ? state.continuous : 0) + VAD.window : 0;
  const silence = active ? 0 : state.silence + VAD.window;
  return {noise, speech, continuous, silence, speaking, interrupt:speaking && continuous >= VAD.interrupt, end:speech >= VAD.speech && silence >= VAD.silence};
}

export const initialVoiceActivity = () => ({noise:VAD.floor / VAD.noiseFactor, speech:0, continuous:0, silence:0, speaking:false});
