export const harnesses = [
  { value: 'claude', title: 'Claude Code', accent: 'text-[#D97757]' },
  { value: 'codex', title: 'Codex', accent: 'text-neutral-100' },
];
export const harnessInfo = value => harnesses.find(h => h.value === value) ?? { value, title: value, accent: '' };

// claude-opus-5-5 → Opus 5.5, gpt-6.2-sol → GPT 6.2 Sol: a word without vowels is an acronym. Dated snapshots drop the date.
export function modelLabel(id) {
  const parts = String(id).replace(/^claude-/, '').split('-').filter(part => !/^\d{8}$/.test(part));
  const words = [];
  for (const part of parts) {
    if (/^\d+$/.test(part) && /^[\d.]+$/.test(words.at(-1) ?? '')) words[words.length - 1] += `.${part}`;
    else words.push(/^[b-df-hj-np-tv-xz]+$/.test(part) ? part.toUpperCase() : part.charAt(0).toUpperCase() + part.slice(1));
  }
  return words.join(' ');
}

export function mergeSwitches(messages) {
  const merged = [];
  for (const message of messages) {
    const last = merged.at(-1);
    if (message.kind === 'switch' && last?.kind === 'switch') merged[merged.length - 1] = { ...message, from: last.from };
    else merged.push(message);
  }
  const same = (a, b) => a.harness === b.harness && a.model === b.model && a.reasoning === b.reasoning;
  return merged.filter(message => message.kind !== 'switch' || !same(message.from, message.to));
}
