import { Skills } from './skills';
import { Secrets } from './secrets';
import { Sudo } from './sudo';
import { Schedules } from './schedules';
import { recall } from './database';
import type { Database } from 'bun:sqlite';
import { chatHistory, reportText, usageLimitText, visibleAnswer } from './chat';
import { hostPath, mcpUrl as defaultMcpUrl } from './config';
import type { Turn } from './harness';
import { SECRETARY_PROMPT, specialistPrompt } from './persona';
import { isInternal, isSpecialist, SECRETARY_ID, Session, type SessionRow } from './session';
import { markdownText } from './speech-text';

export type SessionSettings = { harness: string; model: string; reasoning: string | null };
export type NewSession = SessionSettings & { folder: string; branch?: string | null; parentId?: number | null; name?: string | null };

type Options = {
  turns: Record<string, Turn>;
  checkModel: (harness: string, model: string, reasoning: string) => Promise<void>;
  onEvent: (session: Session, event: any, cursor?: number) => void;
  onChange: () => void;
  mcpUrl?: string;
};

export class Sessions {
  readonly schedules: Schedules;
  approvals!: { skills: Skills; secrets: Secrets; sudo: Sudo };
  private all = new Map<number, Session>();
  private folder = '';
  private tokens = new Map<number, string>();

  authorizeMcp(id: number, authorization: string | null) {
    const token = this.tokens.get(id);
    return !!token && authorization === `Bearer ${token}`;
  }

  constructor(private db: Database, private options: Options) { this.schedules = new Schedules(db, this); }

  get harnesses() { return Object.keys(this.options.turns); }

  // Loads every Session. Turns that were running when Secretary died are marked interrupted;
  // their messages already in flight become delivery_uncertain inside each queue.
  load(secretaryFolder: string) {
    this.folder = secretaryFolder;
    const emit = (id: number, event: object) => this.get(id).emit(event);
    this.approvals = { skills: new Skills(secretaryFolder, emit), secrets: new Secrets(secretaryFolder, emit), sudo: new Sudo(emit) };
    const rows = this.db.query('SELECT * FROM sessions ORDER BY id').all() as SessionRow[];
    if (!rows.length) {
      // Первый запуск: дефолт зависит от доступных harness (на hosted с SECRETARY_HARNESSES=opencode это OpenCode).
      const harness = this.harnesses.includes('claude') ? 'claude' : this.harnesses[0];
      this.insert(harness === 'opencode'
        ? { harness, model: 'pag/muse-spark-1.3-contributor', reasoning: 'high', folder: secretaryFolder }
        : { harness, model: 'opus', reasoning: null, folder: secretaryFolder });
    }
    else for (const row of rows) {
      // OpenCode держит MCP-серверы в своей сессии: неудачное рукопожатие (рестарт застал resume)
      // навсегда выкидывает инструменты secretary из её каталога, а resume их не возвращает —
      // лечит только session/new. Поэтому после рестарта opencode-сессии начинают заново,
      // а история подставляется из базы тем же путём, что при смене harness.
      const hadHarnessSession = row.harness_session_id;
      if (row.harness === 'opencode' && row.harness_session_id) {
        row.harness_session_id = null;
        this.db.query('UPDATE sessions SET harness_session_id = NULL WHERE id = ?').run(row.id);
      }
      const session = this.attach(row);
      if (row.status === 'running') this.db.transaction(() => {
        this.db.query("UPDATE sessions SET status = 'interrupted' WHERE id = ?").run(row.id);
        if (hadHarnessSession) session.queue.add('Secretary перезапустился посреди твоего хода. Продолжи работу с того места, где остановился; если всё уже сделано — коротко подведи итог.', [], true, false, undefined, [], true);
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

  setHidden(id: unknown, hidden: boolean) {
    this.get(id).setHidden(hidden);
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
      if (r.id === SECRETARY_ID) return { mcpUrl: `${baseMcp}/${r.id}`, mcpToken: token, mcpRoot: this.folder, mcpRole: 'secretary', systemPrompt: SECRETARY_PROMPT };
      if (isSpecialist(r)) return { mcpUrl: `${baseMcp}/${r.id}`, mcpToken: token, mcpRoot: this.folder, mcpRole: 'specialist', systemPrompt: specialistPrompt(this.folder) };
      return {};
    });
    this.all.set(session.id, session);
    session.onEvent((event, cursor) => {
      this.options.onEvent(session, event, cursor);
      if (event.type === 'status') this.options.onChange();
    });
    session.onUsageLimit = ({ resetsAt }) => {
      const now = Date.now();
      const at = resetsAt !== undefined && resetsAt + 60_000 > now ? resetsAt + 60_000 : now + 30 * 60_000;
      this.schedules.add(session.id, 'Лимит подписки сбросился, продолжи работу с того места, где остановился', undefined, new Date(at).toISOString());
      const info = session.info;
      if (info.parent_id) this.all.get(info.parent_id)?.send(reportText(info, usageLimitText(resetsAt)));
    };
    session.onComplete = answer => {
      const info = session.info;
      if (info.id === SECRETARY_ID && visibleAnswer(answer)) this.options.onEvent(session, {type:'secretary_reply', text:markdownText(answer)});
      if (info.parent_id && visibleAnswer(answer)) this.all.get(info.parent_id)?.send(reportText(info, answer));
    };
    return session;
  }
}
