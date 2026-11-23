import type { Database } from 'bun:sqlite';
import { writeEvent } from './database';
import type { Image } from './images';
import type { FileAttachment } from './files';

export type Message = { id: number; text: string; images: Image[]; files: FileAttachment[]; urgent: boolean; voice: boolean; speechTarget?: (message: object) => void };
export type Unsent = 'cancelled' | 'delivery_uncertain';

// The messages of one Session that the client got a confirmation for but that are not in history yet.
// A message waits here, may go in flight to a harness, and leaves exactly once: as a `user` event when the harness
// accepted it, as `cancelled` when the harness never saw it, or as `delivery_uncertain` when it may have.
// The row leaves pending_messages in the same transaction that writes the event, so a crash can't keep both or neither.
export class MessageQueue {
  private items: Message[];

  constructor(private db: Database, private sessionId: number, private publish: (event: object, cursor?: number) => void) {
    const rows = db.query('SELECT id, urgent, text, images, in_flight, voice FROM pending_messages WHERE session_id = ? ORDER BY urgent DESC, id').all(sessionId) as { id: number; urgent: number; text: string; images: string; in_flight: number; voice: number }[];
    this.items = rows.map(row => {
      const saved = JSON.parse(row.images);
      return { id: row.id, text: row.text, images: saved.images ?? saved, files: saved.files ?? [], urgent: row.urgent === 1, voice: row.voice === 1 };
    });
    // In flight at load time means Secretary died after handing these to a harness: they must not be sent again.
    const inFlight = new Set(rows.filter(row => row.in_flight).map(row => row.id));
    this.drop(this.items.filter(item => inFlight.has(item.id)), 'delivery_uncertain');
  }

  get length() { return this.items.length; }
  list() { return this.items.map(({ speechTarget, ...item }) => item); }
  has(item: Message) { return this.items.includes(item); }

  // Urgent messages stand before ordinary ones, each group in arrival order.
  add(text: string, images: Image[], urgent: boolean, voice = false, speechTarget?: (message: object) => void, files: FileAttachment[] = []) {
    const saved = this.db.query('INSERT INTO pending_messages (session_id, urgent, text, images, voice) VALUES (?, ?, ?, ?, ?)').run(this.sessionId, urgent ? 1 : 0, text, JSON.stringify({images, files}), voice ? 1 : 0);
    const item: Message = { id: Number(saved.lastInsertRowid), text, images, files, urgent, voice, speechTarget };
    this.items.splice(urgent ? this.items.filter(m => m.urgent).length : this.items.length, 0, item);
    this.publishQueue();
  }

  promote(id: unknown, active: Message[]) {
    const item = this.items.find(item => item.id === id && !active.includes(item));
    if (!item) throw new Error('Сообщение уже отправляется или отсутствует в очереди');
    this.db.query('UPDATE pending_messages SET urgent = 1 WHERE id = ?').run(item.id);
    item.urgent = true;
    this.items.sort((a, b) => Number(b.urgent) - Number(a.urgent) || a.id - b.id);
    this.publishQueue();
  }

  // The next turn: all waiting urgent messages together, so they don't wait for each other; otherwise one ordinary message.
  peek() {
    return this.items[0]?.urgent ? this.items.filter(item => item.urgent) : this.items.slice(0, 1);
  }

  inFlight(batch: Message[]) {
    this.db.transaction(() => { for (const item of batch) this.db.query('UPDATE pending_messages SET in_flight = 1 WHERE id = ?').run(item.id); })();
  }

  accept(batch: Message[]) {
    this.resolve(batch, item => ({ type: 'user', message: { role: 'user', content: item.text }, images: item.images, files: item.files }));
  }

  // Returns how many of the batch were still waiting.
  drop(batch: Message[], outcome: Unsent | ((item: Message) => Unsent)) {
    const outcomeOf = typeof outcome === 'function' ? outcome : () => outcome;
    return this.resolve(batch, item => ({ type: outcomeOf(item), text: item.text, images: item.images, files: item.files }));
  }

  dropAll(outcome: (item: Message) => Unsent) { return this.drop(this.items, outcome); }

  // The Session is being deleted together with its history: no events.
  discard() {
    this.db.query('DELETE FROM pending_messages WHERE session_id = ?').run(this.sessionId);
    this.items = [];
  }

  private resolve(batch: Message[], eventOf: (item: Message) => object) {
    const live = batch.filter(item => this.items.includes(item));
    if (!live.length) return 0;
    const events = live.map(eventOf);
    const time = new Date().toISOString();
    const cursors: number[] = [];
    this.db.transaction(() => {
      live.forEach((item, i) => {
        cursors.push(writeEvent(this.db, this.sessionId, events[i], time));
        this.db.query('DELETE FROM pending_messages WHERE id = ?').run(item.id);
      });
    })();
    this.items = this.items.filter(item => !live.includes(item));
    events.forEach((event, i) => this.publish(event, cursors[i]));
    this.publishQueue();
    return live.length;
  }

  private publishQueue() { this.publish({ type: 'queue', items: this.list() }); }
}
