import type { Subprocess } from 'bun';
import { agentAnswer } from './chat';
import { claudeTurn } from './claude';
import { codexTurn } from './codex';
import { db } from './database';
import { killHarness, type Turn } from './harness';
import { validateImages, type Image } from './images';
export { db } from './database';

export type SessionRow = { id: number; parent_id: number | null; harness: string; model: string; folder: string; harness_session_id: string | null; status: string; name: string | null; reasoning: string | null; hidden: number };

type Pending = { id: number; text: string; images: Image[]; urgent: boolean };

const turns: Record<string, Turn> = { claude: claudeTurn, codex: codexTurn };
export const harnesses = Object.keys(turns);

// The main Secretary Agent session and sessions inside its folder belong to Secretary itself, not to the owner's list.
export function isInternal(row: Pick<SessionRow, 'id' | 'folder'>) {
  return row.id === 1 || row.folder.split('/').includes('.secretary');
}

export class Session {
  static interruptDeadlineMs = 10_000;
  // Urgent (immediate) messages stand before ordinary ones, in arrival order. `urgent` counts them.
  private queue: Pending[] = [];
  private urgent = 0;
  private running = false;
  private process?: Subprocess;
  private stopped = false;
  private deleted = false;
  private lastAnswer = '';
  // The turn is being interrupted for an urgent message: the queue restarts without idle status or a report to the parent.
  private restarting = false;
  private userInterrupted = false;
  private interruptRequestId?: string;
  private activeItems: Pending[] = [];
  private dispatched = false;
  private interruptCurrent?: () => void;
  onComplete?: (answer: string) => void;
  private listeners: ((event: any) => void)[] = [];

  constructor(private row: SessionRow) {
    const rows = db.query('SELECT id, urgent, text, images FROM pending_messages WHERE session_id = ? ORDER BY urgent DESC, id').all(row.id) as { id: number; urgent: number; text: string; images: string }[];
    this.queue = rows.map(item => ({ id: item.id, text: item.text, images: JSON.parse(item.images), urgent: item.urgent === 1 }));
    this.urgent = this.queue.filter(item => item.urgent).length;
  }

  static create(model: string, folder: string, harness = 'claude', parentId: number | null = null, name: string | null = null, reasoning: string | null = null) {
    const result = db.query("INSERT INTO sessions (harness, model, folder, status, parent_id, name, reasoning) VALUES (?, ?, ?, 'idle', ?, ?, ?)").run(harness, model, folder, parentId, name, reasoning);
    return Session.get(Number(result.lastInsertRowid))!;
  }

  static get(id: number) {
    const row = db.query('SELECT * FROM sessions WHERE id = ?').get(id) as SessionRow | null;
    return row && new Session(row);
  }

  // A crash after a message went to the harness leaves its delivery unknown. Record that instead of sending it twice.
  static recoverUnconfirmed() {
    db.transaction(() => {
      const rows = db.query('SELECT id, session_id, text, images FROM pending_messages WHERE in_flight = 1').all() as { id: number; session_id: number; text: string; images: string }[];
      for (const row of rows) {
        const event = { type: 'delivery_uncertain', text: row.text, images: JSON.parse(row.images) };
        db.query('INSERT INTO events VALUES (?, ?, ?)').run(row.session_id, JSON.stringify(event), new Date().toISOString());
        db.query('DELETE FROM pending_messages WHERE id = ?').run(row.id);
      }
    })();
  }

  get id() { return this.row.id; }
  get info() { return db.query('SELECT * FROM sessions WHERE id = ?').get(this.id) as SessionRow; }
  events() { return (db.query('SELECT json FROM events WHERE session_id = ? ORDER BY rowid').all(this.id) as { json: string }[]).map(row => JSON.parse(row.json)); }
  queued() { return this.queue.map(({ id, text, images, urgent }) => ({ id, text, images, urgent })); }
  onEvent(callback: (event: any) => void) { this.listeners.push(callback); }

  setHidden(hidden: boolean) {
    if (isInternal(this.row) || typeof hidden !== 'boolean') throw new Error('Недопустимое изменение видимости сессии');
    db.query('UPDATE sessions SET hidden = ? WHERE id = ?').run(hidden ? 1 : 0, this.id);
  }

  setName(name: unknown) {
    if (typeof name !== 'string' || !name.trim() || name.trim().length > 100) throw new Error('Название должно содержать от 1 до 100 символов');
    this.row.name = name.trim();
    db.query('UPDATE sessions SET name = ? WHERE id = ?').run(this.row.name, this.id);
  }

  // Switching harness starts a new harness session: the old context cannot be resumed by the other harness.
  configure(harness: string, model: string, reasoning: string | null) {
    if (this.running) throw new Error('Session is running');
    if (harness !== this.row.harness) this.row.harness_session_id = null;
    Object.assign(this.row, { harness, model, reasoning });
    db.query('UPDATE sessions SET harness = ?, model = ?, reasoning = ?, harness_session_id = ? WHERE id = ?').run(harness, model, reasoning, this.row.harness_session_id, this.id);
  }

  delete() {
    if (this.id === 1) throw new Error('Cannot delete secretary');
    if (this.running) throw new Error('Сначала остановите сессию');
    db.transaction(() => {
      db.query('UPDATE sessions SET parent_id = NULL WHERE parent_id = ?').run(this.id);
      db.query('DELETE FROM pending_messages WHERE session_id = ?').run(this.id);
      db.query('DELETE FROM events WHERE session_id = ?').run(this.id);
      db.query('DELETE FROM sessions WHERE id = ?').run(this.id);
    })();
    this.deleted = true;
    this.queue = [];
    this.listeners = [];
    this.onComplete = undefined;
  }

  // Events go to history and to listeners. Queue updates and interrupt results are only published.
  private write(event: unknown, time: string) { db.query('INSERT INTO events VALUES (?, ?, ?)').run(this.id, JSON.stringify(event), time); }
  private publish(event: unknown) { for (const listener of this.listeners) listener(event); }
  private emit(event: unknown) {
    this.write(event, new Date().toISOString());
    this.publish(event);
  }
  private publishQueue() { this.publish({ type: 'queue', items: this.queued() }); }
  private killProcess() { if (this.process) killHarness(this.process); }

  // An ordinary message waits for the turn to end. An immediate one interrupts the running turn and goes next.
  // The message is saved before returning: the client only gets a confirmation for a stored message.
  send(text: string, input?: unknown, immediate = false) {
    if (typeof text !== 'string') throw new Error('Некорректный текст сообщения');
    const images = validateImages(input);
    if (!text.trim() && !images.length) throw new Error('Сообщение пустое');
    if (this.deleted) throw new Error('Session was deleted');
    this.stopped = false;
    immediate = immediate && this.running;
    const saved = db.query('INSERT INTO pending_messages (session_id, urgent, text, images) VALUES (?, ?, ?, ?)').run(this.id, immediate ? 1 : 0, text, JSON.stringify(images));
    const item: Pending = { id: Number(saved.lastInsertRowid), text, images, urgent: immediate };
    if (immediate) { this.queue.splice(this.urgent, 0, item); this.urgent++; }
    else this.queue.push(item);
    this.publishQueue();
    if (!this.running) void this.next();
    else if (immediate) this.interruptTurn();
  }

  interrupt(requestId?: string) {
    if (!this.running || this.userInterrupted) return false;
    this.userInterrupted = true;
    this.interruptRequestId = requestId;
    this.interruptTurn();
    return true;
  }

  // Stopping cancels messages that have not reached the harness. They stay in history as not sent.
  stop() {
    this.stopped = true;
    const cancelled = this.queue;
    this.queue = [];
    this.urgent = 0;
    const events = cancelled.map(item => ({ type: this.dispatched && this.activeItems.includes(item) ? 'delivery_uncertain' : 'cancelled', text: item.text, images: item.images }));
    const time = new Date().toISOString();
    db.transaction(() => {
      db.query('DELETE FROM pending_messages WHERE session_id = ?').run(this.id);
      for (const event of events) this.write(event, time);
    })();
    for (const event of events) this.publish(event);
    this.publishQueue();
    this.killProcess();
    if (!this.running) this.finish(false);
  }

  resumePending() {
    if (!this.running && !this.deleted && this.queue.length) void this.next();
  }

  // One interrupt per turn. Later urgent messages wait in the queue; the deadline bounds the wait if the harness does not respond.
  private interruptTurn() {
    if (this.restarting) return;
    this.restarting = true;
    const child = this.process;
    this.interruptCurrent?.();
    setTimeout(() => { if (this.restarting && this.process === child) this.killProcess(); }, Session.interruptDeadlineMs);
  }

  // The harness may become interruptable after the interrupt was requested (Codex answers turn/start later).
  private armInterrupt(interrupt: () => void) {
    this.interruptCurrent = interrupt;
    if (this.restarting) interrupt();
  }

  // All urgent messages already waiting go as one turn, so two urgent messages don't wait for each other.
  private peek(): Pending[] {
    return this.queue.slice(0, this.queue[0]?.urgent ? this.urgent : 1);
  }

  // Accepted messages move from the queue to history in one transaction, so a crash can't leave a message in both.
  private commit(items: Pending[]) {
    const live = items.filter(item => this.queue.includes(item));
    if (!live.length) return;
    const time = new Date().toISOString();
    const userEvent = (item: Pending) => ({ type: 'user', message: { role: 'user', content: item.text }, images: item.images });
    db.transaction(() => {
      for (const item of live) {
        this.write(userEvent(item), time);
        db.query('DELETE FROM pending_messages WHERE id = ?').run(item.id);
      }
    })();
    this.queue = this.queue.filter(item => !live.includes(item));
    this.urgent = this.queue.filter(item => item.urgent).length;
    for (const item of live) this.publish(userEvent(item));
    this.publishQueue();
  }

  private settleInterruptedItems() {
    const unresolved = this.activeItems.filter(item => this.queue.includes(item));
    if (!unresolved.length) return 0;
    const type = this.dispatched ? 'delivery_uncertain' : 'cancelled';
    const time = new Date().toISOString();
    db.transaction(() => {
      for (const item of unresolved) {
        db.query('DELETE FROM pending_messages WHERE id = ?').run(item.id);
        this.write({ type, text: item.text, images: item.images }, time);
      }
    })();
    this.queue = this.queue.filter(item => !unresolved.includes(item));
    this.urgent = this.queue.filter(item => item.urgent).length;
    for (const item of unresolved) this.publish({ type, text: item.text, images: item.images });
    this.publishQueue();
    return unresolved.length;
  }

  private async next() {
    const items = this.peek();
    if (!items.length) return;
    this.activeItems = items;
    this.dispatched = false;
    this.running = true;
    this.lastAnswer = '';
    if (this.info.status !== 'running') {
      db.query("UPDATE sessions SET status = 'running' WHERE id = ?").run(this.id);
      this.emit({ type: 'status', status: 'running' });
    }
    // Several urgent messages go as one turn but stay separate in history.
    const text = items.map(item => item.text).filter(Boolean).join('\n\n');
    const images = items.flatMap(item => item.images);
    let delivered = false;
    try {
      const completed = await turns[this.row.harness]({
        folder: this.row.folder, model: this.row.model, reasoning: this.row.reasoning, resumeId: this.row.harness_session_id, secretary: this.id === 1,
        onSessionId: id => {
          if (id === this.row.harness_session_id) return;
          this.row.harness_session_id = id;
          db.query('UPDATE sessions SET harness_session_id = ? WHERE id = ?').run(id, this.id);
        },
        onProcess: child => { this.process = child; },
        onInterruptable: interrupt => this.armInterrupt(interrupt),
        onDispatching: () => {
          db.transaction(() => { for (const item of items) db.query('UPDATE pending_messages SET in_flight = 1 WHERE id = ?').run(item.id); })();
          this.dispatched = true;
        },
        onAccepted: () => { if (!delivered) { delivered = true; this.commit(items); } },
        shouldCancel: () => this.userInterrupted,
        onEvent: event => { this.lastAnswer = agentAnswer(event) ?? this.lastAnswer; this.emit(event); },
      }, text, images);
      if (!delivered && !completed && !this.restarting && !this.stopped && !this.userInterrupted) this.emit({ type: 'error', message: 'Агент завершился без подтверждения сообщения' });
      this.finish(completed, delivered);
    } catch (error) {
      // An interrupted turn restarts at once, so its error is not shown.
      if (!this.restarting) this.emit({ type: 'error', message: String(error) });
      this.killProcess();
      await this.process?.exited;
      this.finish(false, delivered);
    }
  }

  private finish(completed: boolean, delivered = false) {
    const unresolved = this.userInterrupted || (this.dispatched && !delivered) ? this.settleInterruptedItems() : 0;
    // A turn interrupted for an urgent message is not finished: no report to the parent, status stays running.
    const restart = !this.stopped && this.restarting && !completed && this.queue.length > 0;
    const userInterrupted = this.userInterrupted;
    const interruptRequestId = this.interruptRequestId;
    this.userInterrupted = false;
    this.interruptRequestId = undefined;
    this.activeItems = [];
    this.restarting = false;
    this.interruptCurrent = undefined;
    this.process = undefined;
    const reportInterrupt = () => { if (interruptRequestId) this.publish({ type: 'interrupt_result', requestId: interruptRequestId, outcome: completed ? 'completed' : 'ended' }); };
    if (restart) { reportInterrupt(); this.running = false; void this.next(); return; }
    const status = this.stopped ? 'stopped' : completed || userInterrupted ? 'idle' : 'interrupted';
    db.query('UPDATE sessions SET status = ? WHERE id = ?').run(status, this.id);
    this.emit({ type: 'status', status });
    reportInterrupt();
    if (completed && !userInterrupted && this.row.parent_id) this.onComplete?.(this.lastAnswer);
    this.running = false;
    // After an uncertain delivery the queue goes on without repeating that message.
    // A failure before dispatch keeps the message queued, so a broken launch does not loop.
    if (!this.stopped && (completed || delivered || (this.dispatched && unresolved)) && this.queue.length) void this.next();
  }
}
