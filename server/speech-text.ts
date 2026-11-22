// Converts only the spoken copy; chat retains the original Markdown.
export function speechText(text: string) {
  const inline: string[] = [];
  const clean = text
    .replace(/(^|\n)[ \t]*(```+|~~~+)[^\n]*\n[\s\S]*?\n[ \t]*\2[^\n]*(?=\n|$)/g, '$1код в чате')
    .replace(/(^|\n)[^\n]*\|[^\n]*\n[ \t]*\|?[ \t]*:?-{3,}[^\n]*(?:\n[^\n]*\|[^\n]*)*/g, '$1таблица в чате')
    .replace(/`([^`\n]+)`/g, (_, code) => { inline.push(code); return `\u0000${inline.length - 1}\u0000`; })
    .replace(/!?\[([^\]]+)\]\([^\n)]*\)/g, '$1')
    .replace(/https?:\/\/[^\s<>]+/g, 'ссылка в чате')
    .replace(/^[ \t]*(?:#{1,6}\s+|>\s*|[-+*]\s+|\d+[.)]\s+)/gm, '')
    .replace(/[*_~]/g, '')
    .replace(/\s+/g, ' ').trim();
  return clean.replace(/\u0000(\d+)\u0000/g, (_, index) => inline[Number(index)]);
}

// Returns only complete sentences until flush, leaving the unfinished suffix for the next chunk.
export function splitSentences(buffer: string, flush = false): { sentences: string[]; rest: string } {
  const sentences: string[] = [];
  const end = /[.!?…]+(?:[»”"')\]]*)(?=\s|$)/g;
  let start = 0, match: RegExpExecArray | null;
  while ((match = end.exec(buffer))) {
    if (!flush && end.lastIndex === buffer.length) break;
    const sentence = buffer.slice(start, end.lastIndex).trim();
    if (sentence) sentences.push(sentence);
    start = end.lastIndex;
  }
  const rest = buffer.slice(start);
  if (flush && rest.trim()) sentences.push(rest.trim());
  return { sentences, rest: flush ? '' : rest };
}
