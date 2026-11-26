import { recall } from './database';
import type { Database } from 'bun:sqlite';
import { chatHistory, chatMessage, reportText } from './chat';
import { hostPath, mcpUrl as defaultMcpUrl } from './config';
import type { Turn } from './harness';
import { SECRETARY_PROMPT, specialistPrompt } from './persona';
import { isInternal, isSpecialist, SECRETARY_ID, Session, type SessionRow } from './session';
import { markdownText } from './speech-text';

export type SessionSettings = { harness: string; model: string; reasoning: string | null };
export type NewSession = SessionSettings & { folder: string; branch?: string | null; parentId?: number | null; name?: string | null };

type Options = {
  turns: Record<string, Turn>;
  // Rejects a model or reasoning level the harness does not offer.
  checkModel: (harness: string, model: string, reasoning: string) => Promise<void>;
  // Every Session event, raw. Adapters decide what to show.
  onEvent: (session: Session, event: any, cursor?: number) => void;
  onChange: () => void;
  mcpUrl?: string;
};

// All Sessions of the Host and the rules for them. The browser and the Secretary Agent (through MCP) are two adapters over this.
export class Sessions {
  private all = new Map<number, Session>();
  private folder = '';
  private tokens = new Map<number, string>();

  authorizeMcp(id: number, authorization: string | null) {
    const token = this.tokens.get(id);
    return !!token && authorization === `Bearer ${token}`;
  }

  constructor(private db: Database, private options: Options) {}

  get harnesses() { return Object.keys(this.options.turns); }

  // Loads every Session. Turns that were running when Secretary died are marked interrupted;
  // their messages already in flight become delivery_uncertain inside each queue.
  load(secretaryFolder: string) {
    this.folder = secretaryFolder;
    const rows = this.db.query('SELECT * FROM sessions ORDER BY id').all() as SessionRow[];
    if (!rows.length) this.insert({ harness: 'claude', model: 'opus', reasoning: null, folder: secretaryFolder });
    else for (const row of rows) {
      const session = this.attach(row);
      if (row.status === 'running') this.db.transaction(() => {
        this.db.query("UPDATE sessions SET status = 'interrupted' WHERE id = ?").run(row.id);
        if (row.harness_session_id) session.queue.add('Secretary перезапустился посреди твоего хода. Продолжи работу с того места, где остановился; если всё уже сделано — коротко подведи итог.', [], true, false, undefined, [], true);
      })();
    }
  }

  // Messages confirmed before a restart go now. Call once adapters listen.
  resumePending() { for (const session of this.all.values()) session.resumePending(); }

  get(id: unknown) {
    const session = this.all.get(Number(id));
    if (!session) throw new Error('Сессия не найдена');
    return session;
  }

  list() { return [...this.all.values()].map(s => { const info = s.info; return { ...info, internal: isInternal(info), secretary: info.id === SECRETARY_ID }; }); }

  children(parentId: number) { return [...this.all.values()].filter(s => s.info.parent_id === parentId); }

  recall(id: number, query: string) { return recall(this.db, id, query); }
  daySummary(date?: unknown) {
    const now = new Date();
    const day = date === undefined ? [now.getFullYear(), String(now.getMonth() + 1).padStart(2, '0'), String(now.getDate()).padStart(2, '0')].join('-') : date;
    if (typeof day !== 'string' || !/^\d{4}-\d{2}-\d{2}$/.test(day)) throw new Error('Неверная дата');
    const [y, m, d] = day.split('-').map(Number);
    const start = new Date(0);
    start.setFullYear(y, m - 1, d); start.setHours(0, 0, 0, 0);
    if (start.getFullYear() !== y || start.getMonth() !== m - 1 || start.getDate() !== d) throw new Error('Неверная дата');
    const end = new Date(start); end.setDate(end.getDate() + 1);
    const rows = this.db.query('SELECT e.session_id, e.time, e.json, s.name, s.parent_id FROM events e JOIN sessions s ON s.id = e.session_id WHERE e.time >= ? AND e.time < ? ORDER BY e.time, e.rowid').all(start.toISOString(), end.toISOString()) as any[];
    const groups = new Map<number, any>();
    for (const row of rows) {
      const message = chatMessage(JSON.parse(row.json));
      if (!message || !['user', 'agent', 'report', 'switch', 'error'].includes(message.kind)) continue;
      let group = groups.get(row.session_id);
      if (!group) groups.set(row.session_id, group = { id: row.session_id, name: row.name, parent_id: row.parent_id, turns: 0, events: [] });
      if (message.kind === 'user') group.turns++;
      if ('text' in message) message.text = message.text.slice(0, 2000);
      if ('images' in message) delete (message as any).images;
      if ('files' in message && message.files) message.files = message.files.map(file => ({ name: file.name } as typeof file));
      group.events.push({ time: row.time, ...message });
    }
    return { date: day, timezone: Intl.DateTimeFormat().resolvedOptions().timeZone, sessions: [...groups.values()] };
  }
  chatHistory(id: unknown, limit: number) { return chatHistory(this.db, this.get(id).id, limit); }

  stop(id: unknown) {
    const session = this.get(id);
    session.stop();
    for (const child of this.children(session.id)) this.stop(child.id);
  }

  async create(settings: NewSession) {
    if (typeof settings.folder !== 'string' || !settings.folder) throw new Error('Не указана папка');
    await this.check(settings);
    const session = this.insert({ ...settings, folder: hostPath(settings.folder) });
    this.options.onChange();
    return session;
  }

  async configure(id: unknown, settings: SessionSettings) {
    const session = this.get(id);
    await this.check(settings);
    session.configure(settings.harness, settings.model, settings.reasoning);
    this.options.onChange();
  }

  delete(id: unknown) {
    const session = this.get(id);
    session.delete();
    this.all.delete(session.id);
    this.tokens.delete(session.id);
    this.options.onChange();
  }

  async check({ harness, model, reasoning }: SessionSettings) {
    if (!this.harnesses.includes(harness) || typeof model !== 'string' || !model) throw new Error('Некорректные настройки сессии');
    if (reasoning !== null) await this.options.checkModel(harness, model, reasoning);
  }

  private insert(s: NewSession) {
    const result = this.db.query("INSERT INTO sessions (harness, model, folder, status, parent_id, name, reasoning, branch) VALUES (?, ?, ?, 'idle', ?, ?, ?, ?)").run(s.harness, s.model, s.folder, s.parentId ?? null, s.name ?? null, s.reasoning, s.branch ?? null);
    return this.attach(this.db.query('SELECT * FROM sessions WHERE id = ?').get(Number(result.lastInsertRowid)) as SessionRow);
  }

  private attach(row: SessionRow) {
    const baseMcp = this.options.mcpUrl ?? defaultMcpUrl;
    const token = Buffer.from(crypto.getRandomValues(new Uint8Array(32))).toString('hex');
    if (row.id === SECRETARY_ID || isSpecialist(row)) this.tokens.set(row.id, token);
    const session = new Session(this.db, row, this.options.turns, r => {
      if (r.id === SECRETARY_ID) return { mcpUrl: `${baseMcp}/${r.id}`, mcpToken: token, systemPrompt: SECRETARY_PROMPT };
      if (isSpecialist(r)) return { mcpUrl: `${baseMcp}/${r.id}`, mcpToken: token, systemPrompt: specialistPrompt(this.folder) };
      return {};
    });
    this.all.set(session.id, session);
    session.onEvent((event, cursor) => {
      this.options.onEvent(session, event, cursor);
      if (event.type === 'status') this.options.onChange();
    });
    // A child's answer goes to its parent as a report unless it explicitly chose NO_REPLY.
    session.onComplete = answer => {
      const info = session.info;
      if (info.id === SECRETARY_ID && answer && !answer.startsWith('NO_REPLY')) this.options.onEvent(session, {type:'secretary_reply', text:markdownText(answer)});
      if (info.parent_id && answer && !answer.startsWith('NO_REPLY')) this.all.get(info.parent_id)?.send(reportText(info, answer));
    };
    return session;
  }
}
