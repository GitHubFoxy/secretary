// PCM chunks are scheduled against one audio clock, so consecutive chunks play in order.
export function createSpeechPlayer(onChange, AudioContextImpl = AudioContext, onPlaying = () => {}) {
  let context, next = 0, sources = new Set(), carry = null, active = false;
  function changed() { onChange(active || sources.size > 0); onPlaying(sources.size > 0); }
  function unlock() {
    context ||= new AudioContextImpl();
    if (context.state === 'suspended') context.resume();
  }
  function stop() {
    active = false; carry = null; next = 0;
    for (const source of sources) source.stop();
    sources.clear(); changed();
  }
  return {
    unlock, stop,
    start() { stop(); active = true; changed(); },
    done() { active = false; changed(); },
    audio(data, rate) {
      if (!active) return;
      unlock();
      const raw = Uint8Array.from(atob(data), c => c.charCodeAt(0));
      const bytes = new Uint8Array(raw.length + (carry === null ? 0 : 1));
      if (carry !== null) bytes[0] = carry;
      bytes.set(raw, carry === null ? 0 : 1);
      carry = bytes.length % 2 ? bytes[bytes.length - 1] : null;
      const count = Math.floor(bytes.length / 2);
      if (!count) return;
      const buffer = context.createBuffer(1, count, rate), samples = buffer.getChannelData(0), view = new DataView(bytes.buffer);
      for (let i = 0; i < count; i++) samples[i] = view.getInt16(i * 2, true) / 32768;
      const source = context.createBufferSource();
      source.buffer = buffer; source.connect(context.destination);
      source.onended = () => { sources.delete(source); changed(); };
      sources.add(source);
      next = Math.max(next, context.currentTime);
      source.start(next); next += buffer.duration; changed();
      return true;
    },
  };
}
