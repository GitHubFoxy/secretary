export function trimSilence(samples, rate = 16000) {
  const window = Math.round(rate * 0.02), active = [];
  for (let start = 0; start < samples.length; start += window) {
    const end = Math.min(start + window, samples.length);
    let energy = 0;
    for (let i = start; i < end; i++) energy += samples[i] ** 2;
    if (Math.sqrt(energy / (end - start)) >= 0.01) active.push([start, end]);
  }
  if (!active.length) return new Float32Array(0);
  const parts = [], padding = Math.round(rate * 0.15);
  let start = Math.max(0, active[0][0] - padding), end = active[0][1];
  for (const [next, nextEnd] of active.slice(1)) {
    if (next - end > rate * 0.7) {
      parts.push(samples.subarray(start, Math.min(samples.length, end + padding)));
      start = Math.max(0, next - padding);
    }
    end = nextEnd;
  }
  parts.push(samples.subarray(start, Math.min(samples.length, end + padding)));
  const result = new Float32Array(parts.reduce((n, part) => n + part.length, 0));
  let offset = 0;
  for (const part of parts) { result.set(part, offset); offset += part.length; }
  return result;
}

export async function wavFromRecording(chunks, {trim = true, lastSeconds = 0} = {}) {
  const context = new AudioContext();
  try {
    const audio = await context.decodeAudioData(await new Blob(chunks).arrayBuffer());
    const offline = new OfflineAudioContext(1, Math.ceil(audio.duration * 16000), 16000);
    const source = offline.createBufferSource();
    source.buffer = audio;
    source.connect(offline.destination);
    source.start();
    let samples = (await offline.startRendering()).getChannelData(0);
    if (lastSeconds) samples = samples.subarray(Math.max(0, samples.length - lastSeconds * 16000));
    if (trim) samples = trimSilence(samples);
    if (!samples.length) return null;
    const buffer = new ArrayBuffer(44 + samples.length * 2);
    const view = new DataView(buffer);
    const write = (offset, text) => [...text].forEach((char, i) => view.setUint8(offset + i, char.charCodeAt(0)));
    write(0, 'RIFF'); view.setUint32(4, 36 + samples.length * 2, true); write(8, 'WAVE');
    write(12, 'fmt '); view.setUint32(16, 16, true); view.setUint16(20, 1, true);
    view.setUint16(22, 1, true); view.setUint32(24, 16000, true); view.setUint32(28, 32000, true);
    view.setUint16(32, 2, true); view.setUint16(34, 16, true); write(36, 'data'); view.setUint32(40, samples.length * 2, true);
    samples.forEach((sample, i) => view.setInt16(44 + i * 2, Math.max(-1, Math.min(1, sample)) * 32767, true));
    return buffer;
  } finally { await context.close(); }
}

export function duration(seconds) {
  const s = Math.max(0, Math.floor(seconds));
  return `${Math.floor(s / 60)}:${String(s % 60).padStart(2, '0')}`;
}

// Пометка в тексте говорит агенту, что это расшифровка, а интерфейсу — что показать значок микрофона.
export const voiceText = (text, seconds) => `[Голосовое ${duration(seconds)}] ${text}`;
export function parseVoice(text) {
  const match = /^\[Голосовое (\d+:\d\d)\] ([\s\S]*)$/.exec(text);
  return match && {duration: match[1], text: match[2]};
}
