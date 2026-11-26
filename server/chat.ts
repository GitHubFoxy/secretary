import type { Database } from 'bun:sqlite';
import type { Image } from './images';
import type { FileAttachment } from './files';

// SQLite keeps raw harness events for agents (MCP read). The browser only gets the chat messages built here.
export type ChatMessage =
  | { kind: 'user' | 'cancelled' | 'uncertain'; text: string; images: Image[]; files?: FileAttachment[] }
  | { kind: 'agent'; text: string; files?: FileAttachment[] }
  | { kind: 'error'; text: string }
  | { kind: 'report'; text: string; report: { id: number; harness: string; folder: string } }
  | { kind: 'switch'; from: AgentSettings; to: AgentSettings };

export type AgentSettings = { harness: string; model: string; reasoning: string | null };

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
  if (event?.type === 'result' && event.terminal_reason === 'aborted_streaming') return null;
  if (event?.type === 'result' && event.is_error) {
    const text = event.result === 'Not logged in · Please run /login'
      ? 'Claude Code не вошёл в подписку. На Host выполните: claude auth login'
      : event.result || 'Неизвестная ошибка';
    return { kind: 'error', text };
  }
  if (event?.method === 'turn/completed' && event.params?.turn?.status === 'interrupted') return null;
  if (event?.method === 'turn/completed' && event.params?.turn?.error) {
    const error = event.params.turn.error;
    const text = error.message || 'Неизвестная ошибка';
    return { kind: 'error', text };
  }
  if (event?.type === 'model_switch') return { kind: 'switch', from: event.from, to: event.to };
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

// Chat messages from newest to oldest, scanning raw events backwards.
function* messagesBefore(db: Database, sessionId: number, before = Number.MAX_SAFE_INTEGER) {
  const query = db.query('SELECT rowid, json FROM events WHERE session_id = ? AND rowid < ? ORDER BY rowid DESC LIMIT 200');
  let cursor = before;
  while (true) {
    const rows = query.all(sessionId, cursor) as { rowid: number; json: string }[];
    for (const row of rows) {
      cursor = row.rowid;
      const message = chatMessage(JSON.parse(row.json));
      if (message) yield {...message, cursor};
    }
    if (rows.length < 200) return;
  }
}

function recentMessages(db: Database, sessionId: number, limit: number, before?: number) {
  const messages: (ChatMessage & {cursor: number})[] = [];
  for (const message of messagesBefore(db, sessionId, before)) {
    messages.push(message);
    if (messages.length >= limit) break;
  }
  return messages.reverse();
}

// About 30k tokens: a token is 3-4 characters of Russian or English text.
const transcriptBudget = 100_000;

// What a new harness session learns of the chat: the owner's messages and the agent's answers, newest within the budget.
export function chatTranscript(db: Database, sessionId: number) {
  const lines: string[] = [];
  let size = 0;
  for (const message of messagesBefore(db, sessionId)) {
    const line = message.kind === 'user' ? `Пользователь: ${message.text}`
      : message.kind === 'agent' ? `Агент: ${message.text}`
      : message.kind === 'report' ? `Отчёт #${message.report.id}: ${message.text}` : null;
    if (!line) continue;
    if (size + line.length > transcriptBudget) break;
    lines.push(line);
    size += line.length;
  }
  if (!lines.length) return '';
  return `Это продолжение чата, который до тебя вёл другой агент. Его последние сообщения:\n<history>\n${lines.reverse().join('\n\n')}\n</history>\n\n`;
}

export function chatHistory(db: Database, sessionId: number, limit: number) {
  return recentMessages(db, sessionId, limit).map(({cursor, ...message}) => message);
}

export function chatPage(db: Database, sessionId: number, before?: number) {
  const messages = recentMessages(db, sessionId, 21, before);
  return {messages: messages.slice(-20), hasMore: messages.length > 20};
}
