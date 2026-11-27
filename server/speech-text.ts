// Plain text for a notification: list items keep a bullet, and only emphasis markers go, not `_` inside snake_case.
export function markdownText(text: string) {
  const inline: string[] = [];
  return text
    .replace(/^[ \t]*(?:`{3,}|~{3,})[^\n]*$/gm, '')
    .replace(/`([^`\n]+)`/g, (_, code) => { inline.push(code); return `\u0000${inline.length - 1}\u0000`; })
    .replace(/!?\[([^\]]+)\]\([^\n)]*\)/g, '$1')
    .replace(/^[ \t]*(?:#{1,6}\s+|>\s*)/gm, '')
    .replace(/^[ \t]*[-+*]\s+/gm, '• ')
    .replace(/(?<![\p{L}\p{N}])[*_~]+|[*_~]+(?![\p{L}\p{N}])/gu, '')
    .replace(/\u0000(\d+)\u0000/g, (_, index) => inline[Number(index)]).trim();
}

export function speechText(text: string) {
  const inline: string[] = [];
  const clean = text
    .replace(/(^|\n)[ \t]*(```+|~~~+)[^\n]*\n[\s\S]*?^[ \t]*\2[^\n]*(?=\n|$)/gm, '$1код в чате')
    .replace(/(^|\n)[^\n]*\|[^\n]*\n[ \t]*\|?[ \t]*:?-{3,}[^\n]*(?:\n[^\n]*\|[^\n]*)*/g, '$1таблица в чате')
    .replace(/`([^`\n]+)`/g, (_, code) => { inline.push(code); return `\u0001${inline.length - 1}\u0001`; });
  return markdownText(clean)
    .replace(/^• /gm, '')
    .replace(/https?:\/\/[^\s<>]+?([.,!?;:]*)(?=\s|$)/g, 'ссылка в чате$1')
    .replace(/\u0001(\d+)\u0001/g, (_, index) => inline[Number(index)])
    .replace(/\s+/g, ' ').trim();
}

// Each agent message is complete; synthesis can start with its first sentence.
export function splitSentences(text: string): string[] {
  return text.match(/[^.!?…]+(?:[.!?…]+[»”"')\]]*|$)/g)?.map(sentence => sentence.trim()).filter(Boolean) ?? [];
}
