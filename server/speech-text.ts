// Converts only the spoken copy; chat retains the original Markdown.
export function speechText(text: string) {
  const inline: string[] = [];
  const clean = text
    .replace(/(^|\n)[ \t]*(```+|~~~+)[^\n]*\n[\s\S]*?^[ \t]*\2[^\n]*(?=\n|$)/gm, '$1код в чате')
    .replace(/(^|\n)[^\n]*\|[^\n]*\n[ \t]*\|?[ \t]*:?-{3,}[^\n]*(?:\n[^\n]*\|[^\n]*)*/g, '$1таблица в чате')
    .replace(/`([^`\n]+)`/g, (_, code) => { inline.push(code); return `\u0000${inline.length - 1}\u0000`; })
    .replace(/!?\[([^\]]+)\]\([^\n)]*\)/g, '$1')
    .replace(/https?:\/\/[^\s<>]+?([.,!?;:]*)(?=\s|$)/g, 'ссылка в чате$1')
    .replace(/^[ \t]*(?:#{1,6}\s+|>\s*|[-+*]\s+|\d+[.)]\s+)/gm, '')
    .replace(/[*_~]/g, '')
    .replace(/\s+/g, ' ').trim();
  return clean.replace(/\u0000(\d+)\u0000/g, (_, index) => inline[Number(index)]);
}

// Each agent message is complete; synthesis can start with its first sentence.
export function splitSentences(text: string): string[] {
  return text.match(/[^.!?…]+(?:[.!?…]+[»”"')\]]*|$)/g)?.map(sentence => sentence.trim()).filter(Boolean) ?? [];
}
