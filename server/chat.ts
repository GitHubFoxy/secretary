import type { Database } from 'bun:sqlite';
import type { Image } from './images';
import type { FileAttachment } from './files';

// SQLite keeps raw harness events for agents (MCP read). The browser only gets the chat messages built here.
export type ChatMessage =
  | { kind: 'user' | 'cancelled' | 'uncertain'; text: string; images: Image[]; files?: FileAttachment[] }
  | { kind: 'agent'; text: string; files?: FileAttachment[]; preview?: { url: string; expires_at: string } }
  | { kind: 'skill'; id: string; name?: string; reason?: string; before?: string; content?: string; status?: string }
  | { kind: 'secret'; id: string; name?: string; reason?: string; status?: string }
  | { kind: 'sudo'; id: string; command?: string; reason?: string; status?: string }
  | { kind: 'error'; text: string }
  | { kind: 'step'; tool: string; summary: string }
  | { kind: 'thinking'; text: string }
  | { kind: 'writing' }
  | { kind: 'report'; text: string; report: { id: number; harness: string; folder: string } }
  | { kind: 'compaction'; time: string; beforeTokens?: number; afterTokens?: number }
  | { kind: 'switch'; from: AgentSettings; to: AgentSettings };

export type AgentSettings = { harness: string; model: string; reasoning: string | null };

type Child = { id: number; harness: string; folder: string };
const reportPattern = /^\[report #(\d+) (\S+) ([^\]\n]+)\] ([\s\S]*)$/;

export function reportText(child: Child, answer: string) {
  return `[report #${child.id} ${child.harness} ${child.folder}] ${answer}`;
}

export function agentAnswer(event: any): string | null {
  if (event?.type === 'result' && typeof event.result === 'string') return event.result;
  if (event?.type === 'opencode_result' && typeof event.text === 'string') return event.text;
  const item = event?.method === 'item/completed' ? event.params?.item : null;
  return item?.type === 'agentMessage' && typeof item.text === 'string' ? item.text : null;
}

export function visibleAnswer(text: string | null | undefined): text is string {
  return !!text && !text.startsWith('NO_REPLY');
}

export function speechAnswer(event: any) {
  return event?.type === 'opencode_result' && typeof event.text === 'string' ? event.text
    : event?.type === 'assistant'
    ? (event.message?.content || []).filter((block: any) => block.type === 'text').map((block: any) => block.text).join('\n')
    : event?.method === 'item/completed' && event.params?.item?.type === 'agentMessage' ? event.params.item.text : '';
}

// The harness reports reset timestamps in seconds or in its local time in the error text.
export function usageLimit(event: any): { resetsAt?: number } | null {
  const retry = event?.params?.update?._meta?.['opencode/retry'];
  if (retry && /rate.?limit/i.test(`${retry.error?.type ?? ''} ${retry.error?.message ?? ''}`)) {
    const reset = Date.parse(retry.nextRetryAt);
    return Number.isFinite(reset) ? { resetsAt: reset } : {};
  }
  const error = event?.method === 'turn/completed' ? event.params?.turn?.error
    : event?.method === 'error' && !event.params?.willRetry ? event.params?.error : null;
  const claude = event?.type === 'result' && event.is_error && typeof event.result === 'string' ? event.result : '';
  if (error?.codexErrorInfo !== 'usageLimitExceeded' && !/\b(?:usage|rate)[ _-]limit\b/i.test(claude)) return null;
  const value = error?.resetsAt ?? event?.resetsAt;
  let reset = typeof value === 'number' ? (value < 1e12 ? value * 1000 : value)
    : typeof value === 'string' ? Date.parse(value) : NaN;
  const message = error?.message ?? claude;
  if (!Number.isFinite(reset) && typeof message === 'string') {
    const match = /(?:resets? (?:at|on)|try again at)\s+([^\n]+)/i.exec(message);
    if (match) {
      const text = match[1].replace(/\.$/, '').trim();
      reset = Date.parse(text.replace(/(\d)(?:st|nd|rd|th)\b/g, '$1'));
      if (!Number.isFinite(reset)) {
        const time = /^(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\b/i.exec(text);
        if (time) {
          let hour = Number(time[1]);
          const minute = Number(time[2] ?? 0);
          if (time[3]) hour = hour % 12 + (time[3].toLowerCase() === 'pm' ? 12 : 0);
          if (hour < 24 && minute < 60) {
            const date = new Date(event.emittedAtMs ?? Date.now());
            date.setHours(hour, minute, 0, 0);
            if (date.getTime() < (event.emittedAtMs ?? Date.now())) date.setDate(date.getDate() + 1);
            reset = date.getTime();
          }
        }
      }
    }
  }
  return Number.isFinite(reset) ? { resetsAt: reset } : {};
}

export function usageLimitText(resetsAt?: number) {
  return 'Упёрся в лимит подписки' + (resetsAt === undefined ? ', время сброса неизвестно' : `, сброс в ${new Date(resetsAt).toLocaleTimeString('ru-RU', {hour: '2-digit', minute: '2-digit'})}`);
}

// Raw harness and infrastructure errors never reach the owner as-is: the event keeps them for logs and MCP,
// the chat only shows a short Russian hint (what happened and what to do).
export function friendlyErrorMessage(raw: unknown): string {
  const text = raw instanceof Error ? raw.message : String(raw ?? '');
  const clean = text.replace(/^(Error|SQLiteError)\s*:\s*/i, '').trim();
  const lower = clean.toLowerCase();
  if (!clean) return 'Неожиданная ошибка — попробуйте повторить. Если повторяется, загляните в журнал службы на Host.';
  // Already actionable: keep author's hint, just drop the leading "Error:".
  if (/на host выполните/i.test(clean)) return clean;
  if (/агент завершился без подтверждения|не удалось сжать контекст/i.test(clean)) return clean;
  if (/database is locked|database is busy|sqlite.*locked/.test(lower)) return 'База данных временно занята — попробуйте повторить через пару секунд.';
  if (/session_folder_missing|posix_spawn|enoent|no such file or directory|executable not found/.test(lower)) return 'Не удалось запустить агента: нет папки Session или программы на Host. Проверьте папку и установку, затем повторите.';
  const harness = /завершился с кодом|exit code|exited|harness|app-server/.test(lower);
  if (harness) {
    if (/not logged in|please run \/login|invalid api key|authentication|unauthorized|auth_required/.test(lower)) {
      if (/claude/.test(lower)) return 'Claude Code не вошёл в подписку. На Host выполните: claude auth login';
      if (/codex/.test(lower)) return 'Codex не вошёл в подписку. На Host выполните: codex login --device-auth';
      if (/opencode/.test(lower)) return 'OpenCode требует входа. На Host выполните: opencode auth login';
      return 'Агент не вошёл в подписку. Проверьте вход на Host и повторите.';
    }
    return 'Агент неожиданно завершился. Подробности — в журнале службы на Host, попробуйте повторить.';
  }
  if (/network|fetch failed|econnreset|etimedout|eai_again|enotfound|socket hang up|connection (reset|refused|timed out|aborted)|timed out|getaddrinfo|tls|ssl|\b50[234]\b|service unavailable|gateway|offline|dns/.test(lower)) return 'Похоже, пропала связь с провайдером. Проверьте интернет на Host и попробуйте повторить.';
  return 'Неожиданная ошибка — попробуйте повторить. Если повторяется, загляните в журнал службы на Host.';
}

export function chatMessage(event: any, time = ''): ChatMessage | null {
  if (usageLimit(event)) return null;
  if (event?.type === 'usage_limit') return { kind: 'error', text: usageLimitText(event.resetsAt) };
  if (event?.type === 'opencode_result' && event.stopReason !== 'end_turn') return null;
  if ((event?.type === 'system' && event.subtype === 'compact_boundary') ||
      (event?.method === 'item/completed' && event.params?.item?.type === 'contextCompaction') ||
      (event?.params?.update?.sessionUpdate === 'compaction_update' && ['completed', 'failed'].includes(event.params.update.status)) ||
      (event?.params?.update?.sessionUpdate === 'session_info_update' && ['completed', 'failed'].includes(event.params.update._meta?.['opencode/compaction']?.status))) {
    const metadata = event.compact_metadata;
    const valid = (n: unknown): n is number => typeof n === 'number' && Number.isFinite(n) && n >= 0;
    return { kind: 'compaction', time,
      ...(valid(metadata?.pre_tokens) ? { beforeTokens: metadata.pre_tokens } : {}),
      ...(valid(metadata?.post_tokens) ? { afterTokens: metadata.post_tokens } : {}) };
  }
  const approval = event?.type?.match?.(/^(skill|secret|sudo)_(request|result)$/);
  if (approval) {
    const kind = approval[1] as 'skill' | 'secret' | 'sudo';
    if (approval[2] === 'result') return { kind, id: event.id, status: event.status };
    if (kind === 'skill') return { kind, id: event.id, name: event.name, reason: event.reason, before: event.before, content: event.content };
    if (kind === 'secret') return { kind, id: event.id, name: event.name, reason: event.reason };
    return { kind, id: event.id, command: event.command, reason: event.reason };
  }
  if (event?.type === 'assistant' && Array.isArray(event.message?.content)) {
    const block = event.message.content.find((b: any) => b.type === 'tool_use' || b.type === 'thinking' || b.type === 'text');
    if (block?.type === 'text') return { kind: 'writing' };
    if (block?.type === 'thinking' && typeof block.thinking === 'string') return { kind: 'thinking', text: block.thinking.slice(0, 2000) };
    if (block?.type === 'tool_use') {
      const input = block.input || {};
      const value = block.name === 'Bash' ? input.command
        : ['Read', 'Edit', 'Write'].includes(block.name) ? input.file_path
        : ['Grep', 'Glob'].includes(block.name) ? input.pattern
        : block.name === 'mcp__secretary__send' ? `→ ${input.name ?? '#' + input.id}` : '';
      return { kind: 'step', tool: block.name, summary: String(value ?? '').slice(0, 120) };
    }
  }
  const item = event?.params?.item;
  if (event?.method === 'item/started' && item?.type === 'agentMessage') return { kind: 'writing' };
  if (event?.method === 'item/started') {
    const tool = item?.type === 'commandExecution' ? 'Bash' : item?.type === 'fileChange' ? 'Edit' : item?.type === 'mcpToolCall' ? item.tool : null;
    const summary = item?.type === 'commandExecution' ? item.command : item?.type === 'fileChange' ? item.changes?.[0]?.path : '';
    if (tool) return { kind: 'step', tool, summary: String(summary ?? '').slice(0, 120) };
  }
  if (event?.method === 'item/completed' && item?.type === 'reasoning' && Array.isArray(item.summary))
    return { kind: 'thinking', text: item.summary.join('\n').slice(0, 2000) };
  const sessionUpdate = event?.params?.update?.sessionUpdate;
  const content = event?.params?.update?.content;
  if (sessionUpdate === 'agent_message_chunk') return { kind: 'writing' };
  if (sessionUpdate === 'agent_thought_chunk' && typeof content?.text === 'string') return { kind: 'thinking', text: content.text.slice(0, 2000) };
  if (sessionUpdate === 'tool_call') {
    const raw = event.params.update.rawInput ?? {};
    return { kind: 'step', tool: String(event.params.update.name ?? event.params.update.kind ?? 'tool'), summary: String(raw.command ?? raw.path ?? raw.file ?? '').slice(0, 120) };
  }
  if (event?.type === 'result' && event.terminal_reason === 'aborted_streaming') return null;
  if (event?.type === 'result' && event.is_error) {
    if (event.result === 'Not logged in · Please run /login') return { kind: 'error', text: 'Claude Code не вошёл в подписку. На Host выполните: claude auth login' };
    return { kind: 'error', text: friendlyErrorMessage(event.result || 'Неизвестная ошибка') };
  }
  if (event?.method === 'turn/completed' && event.params?.turn?.status === 'interrupted') return null;
  if (event?.method === 'turn/completed' && event.params?.turn?.error) {
    return { kind: 'error', text: friendlyErrorMessage(event.params.turn.error.message || 'Неизвестная ошибка') };
  }
  if (event?.type === 'model_switch') return { kind: 'switch', from: event.from, to: event.to };
  if (event?.type === 'agent_preview') return { kind: 'agent', text: '', preview: { url: event.url, expires_at: event.expires_at } };
  if (event?.type === 'agent_files') return { kind: 'agent', text: event.text, files: event.files };
  const answer = agentAnswer(event);
  if (answer !== null) return visibleAnswer(answer) ? { kind: 'agent', text: answer } : null;
  const images: Image[] = event?.images || [];
  const files: FileAttachment[] = event?.files || [];
  if (event?.type === 'user') {
    const text = typeof event.message?.content === 'string' ? event.message.content : '';
    const report = reportPattern.exec(text);
    if (report) return { kind: 'report', text: report[4], report: { id: Number(report[1]), harness: report[2], folder: report[3] } };
    return text || images.length || files.length ? { kind: 'user', text, images, ...(files.length ? {files} : {}) } : null;
  }
  if (event?.type === 'error') return event.message ? { kind: 'error', text: friendlyErrorMessage(event.message) } : null;
  if (event?.type === 'cancelled' || event?.type === 'delivery_uncertain')
    return event.text || images.length || files.length ? { kind: event.type === 'cancelled' ? 'cancelled' : 'uncertain', text: event.text || '', images, ...(files.length ? {files} : {}) } : null;
  return null;
}

function* messagesBefore(db: Database, sessionId: number, before = Number.MAX_SAFE_INTEGER) {
  const query = db.query('SELECT rowid, json, time FROM events WHERE session_id = ? AND rowid < ? ORDER BY rowid DESC LIMIT 200');
  let cursor = before;
  while (true) {
    const rows = query.all(sessionId, cursor) as { rowid: number; json: string; time: string }[];
    for (const row of rows) {
      cursor = row.rowid;
      const message = chatMessage(JSON.parse(row.json), row.time);
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
