import type { Database } from 'bun:sqlite';
import type { Sessions } from './sessions';

export function nextCron(expression: string, after: number) {
  const parts = expression.trim().split(/\s+/);
  if (parts.length !== 5) throw new Error('Cron должен содержать пять полей');
  const fields = parts.map((field, i) => {
    const [min, max] = [[0,59], [0,23], [1,31], [1,12], [0,7]][i];
    const values = new Set<number>();
    for (const part of field.split(',')) {
      const m = /^(\*|\d+|\d+-\d+)(?:\/(\d+))?$/.exec(part);
      if (!m || (m[2] && m[1] !== '*')) throw new Error('Некорректное поле cron');
      const step = m[2] ? Number(m[2]) : 1;
      const [start, end] = m[1] === '*' ? [min, max] : m[1].split('-').map(Number);
      const last = end ?? start;
      if (!step || start < min || last > max || start > last) throw new Error('Некорректное поле cron');
      for (let n = start; n <= last; n += step) values.add(i === 4 && n === 7 ? 0 : n);
    }
    return values;
  });
  for (let time = Math.floor(after / 60000) * 60000 + 60000; time <= after + 8 * 366 * 86400000; time += 60000) {
    const d = new Date(time);
    const day = fields[2].has(d.getDate()), weekday = fields[4].has(d.getDay());
    const matches = parts[2] === '*' ? weekday : parts[4] === '*' ? day : day || weekday;
    if (fields[0].has(d.getMinutes()) && fields[1].has(d.getHours()) && fields[3].has(d.getMonth() + 1) && matches) return time;
  }
  throw new Error('Не найдено следующее время cron');
}

type Schedule = { id: number; session_id: number; text: string; cron: string | null; next_run: number | null; last_run: number | null };
export class Schedules {
  constructor(private db: Database, private sessions: Sessions) {}
  add(sessionId: number, text: unknown, cron: unknown, at: unknown, now = Date.now()) {
    this.sessions.get(sessionId);
    if (typeof text !== 'string' || !text.trim()) throw new Error('Укажите текст');
    if ((cron === undefined) === (at === undefined)) throw new Error('Укажите cron или at');
    let next: number;
    if (cron !== undefined) {
      if (typeof cron !== 'string') throw new Error('Некорректный cron');
      next = nextCron(cron, now);
    } else {
      if (typeof at !== 'string' || !/(?:Z|[+-]\d\d:\d\d)$/i.test(at)) throw new Error('Укажите время с часовым поясом');
      next = Date.parse(at);
      if (!Number.isFinite(next) || next <= now) throw new Error('Время должно быть в будущем');
    }
    const result = this.db.query('INSERT INTO schedules (session_id, text, cron, next_run) VALUES (?, ?, ?, ?)').run(sessionId, text.trim(), typeof cron === 'string' ? cron : null, next);
    return { id: Number(result.lastInsertRowid), next_run: new Date(next).toISOString() };
  }
  list(ids: number[]) { return (this.db.query('SELECT * FROM schedules ORDER BY id').all() as Schedule[]).filter(row => ids.includes(row.session_id)); }
  remove(id: unknown, ids: number[]) {
    if (!Number.isSafeInteger(id)) throw new Error('Некорректный id');
    const row = this.db.query('SELECT * FROM schedules WHERE id = ?').get(id) as Schedule | null;
    if (!row || !ids.includes(row.session_id)) throw new Error('Расписание не найдено');
    this.db.query('DELETE FROM schedules WHERE id = ?').run(row.id);
    return { ok: true };
  }
  tick(now = Date.now()) {
    const due = this.db.query('SELECT * FROM schedules WHERE next_run <= ? ORDER BY next_run, id').all(now) as Schedule[];
    for (const row of due) {
      const session = this.sessions.get(row.session_id);
      const next = row.cron ? nextCron(row.cron, now) : null;
      session.queue.add(`[по расписанию] ${row.text}\nЗапланировано: ${new Date(row.next_run!).toISOString()}`, [], false, false, undefined, [], false, () => {
        this.db.query('UPDATE schedules SET next_run = ?, last_run = ? WHERE id = ?').run(next, row.next_run, row.id);
      });
      session.resumePending();
    }
  }
}
