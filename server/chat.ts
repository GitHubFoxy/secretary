import type { Database } from 'bun:sqlite';
import type { Image } from './images';
import type { FileAttachment } from './files';

// SQLite keeps raw harness events for agents (MCP read). The browser only gets the chat messages built here.
export type ChatMessage =
  | { kind: 'user' | 'cancelled' | 'uncertain'; text: string; images: Image[]; files?: FileAttachment[] }
  | { kind: 'agent'; text: string; files?: FileAttachment[] }
  | { kind: 'error'; text: string }
  | { kind: 'report'; text: string; report: { id: number; harness: string; folder: string } };

type Child = { id: number; harness: string; folder: string };
const reportPattern = /^\[report #(\d+) (\S+) ([^\]\n]+)\] ([\s\S]*)$/;

export function reportText(child: Child, answer: string) {
  return `[report #${child.id} ${child.harness} ${child.folder}] ${answer}`;
}

// The final text of a harness turn: Claude Code `result`, Codex `agentMessage`.
export function agentAnswer(event: any): string | null {
  if (event?.type === 'result' && typeof event.result === 'string') return event.result;
  const item = event?.method === 'item/completed' ? event.params?.item : null;
  return item?.type === 'agentMessage' && typeof item.text === 'string' ? item.text : null;
}

export function chatMessage(event: any): ChatMessage | null {
  if (event?.type === 'result' && event.is_error) {
    const text = event.result === 'Not logged in · Please run /login'
      ? 'Claude Code не вошёл в подписку. На Host выполните: claude auth login'
      : event.result || 'Неизвестная ошибка';
    return { kind: 'error', text };
  }
  if (event?.method === 'turn/completed' && event.params?.turn?.error) {
    const error = event.params.turn.error;
    const text = error.message || 'Неизвестная ошибка';
    return { kind: 'error', text };
  }
  if (event?.type === 'agent_files') return { kind: 'agent', text: event.text, files: event.files };
  const answer = agentAnswer(event);
  if (answer !== null) return answer && !answer.startsWith('NO_REPLY') ? { kind: 'agent', text: answer } : null;
  const images: Image[] = event?.images || [];
  const files: FileAttachment[] = event?.files || [];
  if (event?.type === 'user') {
    const text = typeof event.message?.content === 'string' ? event.message.content : '';
    const report = reportPattern.exec(text);
    if (report) return { kind: 'report', text: report[4], report: { id: Number(report[1]), harness: report[2], folder: report[3] } };
    return text || images.length || files.length ? { kind: 'user', text, images, ...(files.length ? {files} : {}) } : null;
  }
  if (event?.type === 'error') return event.message ? { kind: 'error', text: String(event.message) } : null;
  if (event?.type === 'cancelled' || event?.type === 'delivery_uncertain')
    return event.text || images.length || files.length ? { kind: event.type === 'cancelled' ? 'cancelled' : 'uncertain', text: event.text || '', images, ...(files.length ? {files} : {}) } : null;
  return null;
}

// Newest chat messages, scanning raw events backwards until enough are found.
function recentMessages(db: Database, sessionId: number, limit: number, before = Number.MAX_SAFE_INTEGER) {
  const query = db.query('SELECT rowid, json FROM events WHERE session_id = ? AND rowid < ? ORDER BY rowid DESC LIMIT 200');
  const messages: (ChatMessage & {cursor: number})[] = [];
  let cursor = before;
  while (messages.length < limit) {
    const rows = query.all(sessionId, cursor) as { rowid: number; json: string }[];
    for (const row of rows) {
      cursor = row.rowid;
      const message = chatMessage(JSON.parse(row.json));
      if (message) messages.push({...message, cursor});
      if (messages.length >= limit) break;
    }
    if (rows.length < 200) break;
  }
  return messages.reverse();
}

export function chatHistory(db: Database, sessionId: number, limit: number) {
  return recentMessages(db, sessionId, limit).map(({cursor, ...message}) => message);
}

export function chatPage(db: Database, sessionId: number, before?: number) {
  const messages = recentMessages(db, sessionId, 21, before);
  return {messages: messages.slice(-20), hasMore: messages.length > 20};
}
