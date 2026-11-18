import { codexTurn } from './codex';
import { validateImages, type Image } from './images';
import { Database } from 'bun:sqlite';
import { readFileSync, readdirSync } from 'node:fs';
import { recentChatEvents } from './web-history';
import { processAlive, processState } from './process-identity';

export const db = new Database('secretary.sqlite', { create: true });
db.run(`CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL,
  folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL
)`);
try { db.run('ALTER TABLE sessions ADD COLUMN process_identity TEXT'); } catch {}
try { db.run("ALTER TABLE sessions ADD COLUMN deletion_processes TEXT"); } catch {}
try { db.run('ALTER TABLE sessions ADD COLUMN process_uncontained INTEGER NOT NULL DEFAULT 1'); } catch {}
try { db.run('ALTER TABLE sessions ADD COLUMN name TEXT'); } catch {}
try { db.run('ALTER TABLE sessions ADD COLUMN reasoning TEXT'); } catch {}
try { db.run('ALTER TABLE sessions ADD COLUMN hidden INTEGER NOT NULL DEFAULT 0'); } catch {}
db.run(`CREATE TABLE IF NOT EXISTS events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)`);
db.run('CREATE INDEX IF NOT EXISTS events_session_id ON events(session_id)');
// Подтверждённые сообщения, которые ещё не ушли в ход. Лежат в базе, чтобы пережить перезапуск программы.
db.run(`CREATE TABLE IF NOT EXISTS pending_messages (id INTEGER PRIMARY KEY AUTOINCREMENT, session_id INTEGER NOT NULL, urgent INTEGER NOT NULL, text TEXT NOT NULL, images TEXT NOT NULL)`);
try { db.run('ALTER TABLE pending_messages ADD COLUMN in_flight INTEGER NOT NULL DEFAULT 0'); } catch {}

export type SessionRow = { id: number; parent_id: number | null; harness: string; model: string; folder: string; harness_session_id: string | null; status: string; name: string | null; reasoning: string | null; process_identity?: string | null; deletion_processes?: string | null; process_uncontained: number; hidden: number };

type Pending = { id: number; text: string; images: Image[]; urgent: boolean };

function killTree(pid: number) {
  const children = new Map<number, number[]>();
  for (const name of readdirSync('/proc')) {
    if (!/^\d+$/.test(name)) continue;
    try {
      const stat = readFileSync(`/proc/${name}/stat`, 'utf8');
      const parent = Number(stat.slice(stat.lastIndexOf(')') + 2).split(' ')[1]);
      children.set(parent, [...(children.get(parent) || []), Number(name)]);
    } catch {}
  }
  function kill(pid: number) {
    for (const child of children.get(pid) || []) kill(child);
    try { process.kill(pid, 'SIGKILL'); } catch {}
  }
  kill(pid);
}

export class Session {
  // Сколько ждать ответа адаптера на срочное прерывание, прежде чем убить процесс хода (мс).
  static interruptDeadlineMs = 10_000;
  // Срочные сообщения (urgent) всегда стоят впереди обычных, в порядке прихода. urgent — их число.
  private queue: Pending[] = [];
  private urgent = 0;
  private running = false;
  private process: any;
  private stopped = false;
  private deleted = false;
  private deleting = false;
  private lastAnswer = '';
  // Ход прерван ради срочного сообщения: после прерывания запускаем очередь без статуса idle и без отчёта родителю.
  private restarting = false;
  private userInterrupted = false;
  private interruptRequestId?: string;
  private awaitingExit = false;
  private activeItems: Pending[] = [];
  private dispatched = false;
  // Функция прерывания текущего хода; появляется, когда адаптер уже начал ход.
  private interruptCurrent?: () => void;
  onComplete?: (answer: string) => void;
  private listeners: ((event: unknown) => void)[] = [];

  constructor(private row: SessionRow) {
    const rows = db.query('SELECT id, urgent, text, images FROM pending_messages WHERE session_id = ? ORDER BY urgent DESC, id').all(row.id) as { id: number; urgent: number; text: string; images: string }[];
    this.queue = rows.map(item => ({ id: item.id, text: item.text, images: JSON.parse(item.images), urgent: item.urgent === 1 }));
    this.urgent = this.queue.filter(item => item.urgent).length;
    const identity = row.process_identity;
    const deletingProcesses = row.deletion_processes ? JSON.parse(row.deletion_processes) as string[] : [];
    if ((identity && processAlive(identity)) || deletingProcesses.some(item => processAlive(item))) {
      this.running = true;
      this.awaitingExit = true;
      const timer = setInterval(() => {
        if (this.deleted) { clearInterval(timer); return; }
        if (deletingProcesses.length && this.info?.deletion_processes !== row.deletion_processes) { clearInterval(timer); return; }
        if ((identity && processAlive(identity)) || deletingProcesses.some(item => processAlive(item)) || this.deleting) return;
        clearInterval(timer);
        this.awaitingExit = false;
        this.running = false;
        db.query('UPDATE sessions SET process_identity = NULL, deletion_processes = NULL WHERE id = ?').run(this.id);
        if (this.stopped) this.finish(false);
        else if (this.queue.length) void this.next();
      }, 250);
      timer.unref();
    } else if (identity || deletingProcesses.length) db.query('UPDATE sessions SET process_identity = NULL, deletion_processes = NULL WHERE id = ?').run(this.id);
  }

  static create(model: string, folder: string, harness = 'claude', parentId: number | null = null, name: string | null = null, reasoning: string | null = null) {
    const result = db.query("INSERT INTO sessions (harness, model, folder, status, parent_id, name, reasoning, process_uncontained) VALUES (?, ?, ?, 'idle', ?, ?, ?, 0)").run(harness, model, folder, parentId, name, reasoning);
    return Session.get(Number(result.lastInsertRowid))!;
  }

  static get(id: number) {
    const row = db.query('SELECT * FROM sessions WHERE id = ?').get(id) as SessionRow | null;
    return row && new Session(row);
  }

  get id() { return this.row.id; }
  setHidden(hidden: boolean) {
    if (this.id === 1 || typeof hidden !== 'boolean') throw new Error('Недопустимое изменение видимости сессии');
    db.query('UPDATE sessions SET hidden = ? WHERE id = ?').run(hidden ? 1 : 0, this.id);
  }
  private recordProcess(child: any) {
    this.process = child;
    db.query('UPDATE sessions SET process_uncontained = 1 WHERE id = ?').run(this.id);
    if (process.platform === 'linux') {
      const stat = readFileSync(`/proc/${child.pid}/stat`, 'utf8');
      const start = stat.slice(stat.lastIndexOf(')') + 2).split(' ')[19];
      db.query('UPDATE sessions SET process_identity = ? WHERE id = ?').run(`${child.pid}:${start}`, this.id);
    }
  }
  static cleanInterrupted() {
    if (process.platform !== 'linux') return;
    const rows = db.query('SELECT process_identity FROM sessions WHERE process_identity IS NOT NULL').all() as {process_identity:string}[];
    for (const row of rows) {
      const [pid] = row.process_identity.split(':');
      if (processState(row.process_identity) === 'matching') killTree(Number(pid));
    }
  }
  static recoverUnconfirmed() {
    db.transaction(() => {
      const rows = db.query('SELECT id, session_id, text, images FROM pending_messages WHERE in_flight = 1').all() as {id:number, session_id:number, text:string, images:string}[];
      for (const row of rows) {
        const event = {type:'delivery_uncertain', text:row.text, images:JSON.parse(row.images)};
        db.query('INSERT INTO events VALUES (?, ?, ?)').run(row.session_id, JSON.stringify(event), new Date().toISOString());
        db.query('DELETE FROM pending_messages WHERE id = ?').run(row.id);
      }
    })();
  }
  get info() { return db.query('SELECT * FROM sessions WHERE id = ?').get(this.id) as SessionRow; }
  events() { return (db.query('SELECT json FROM events WHERE session_id = ? ORDER BY rowid').all(this.id) as { json: string }[]).map(row => JSON.parse(row.json)); }
  recentChatEvents(limit = 10) { return recentChatEvents(db, this.id, limit); }
  private killProcess() { if (!this.process) return; try { if (process.platform === 'linux') killTree(this.process.pid); else this.process.kill(); } catch { this.process.kill(); } }
  interrupt(requestId?: string) {
    if (!this.running || this.deleting || this.userInterrupted || this.awaitingExit) return false;
    this.userInterrupted = true;
    this.interruptRequestId = requestId;
    this.interruptTurn();
    return true;
  }
  private finishInterruptedItems() {
    const unresolved = this.activeItems.filter(item => this.queue.includes(item));
    if (unresolved.length) {
      const time = new Date().toISOString();
      const type = this.dispatched ? 'delivery_uncertain' : 'cancelled';
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
    }
    return unresolved.length;
  }
  // Остановка отменяет сообщения, которые ещё не дошли до адаптера. Они остаются в истории как «Не отправлено», а не исчезают молча.
  stop() {
    this.stopped = true;
    const cancelled = this.queue;
    this.queue = [];
    this.urgent = 0;
    const time = new Date().toISOString();
    db.transaction(() => {
      db.query('DELETE FROM pending_messages WHERE session_id = ?').run(this.id);
      for (const item of cancelled) this.write({ type: this.dispatched && this.activeItems.includes(item) ? 'delivery_uncertain' : 'cancelled', text: item.text, images: item.images }, time);
    })();
    for (const item of cancelled) this.publish({ type: this.dispatched && this.activeItems.includes(item) ? 'delivery_uncertain' : 'cancelled', text: item.text, images: item.images });
    this.publishQueue();
    this.killProcess();
    const identity = this.awaitingExit && !this.process ? this.info.process_identity : null;
    if (identity && processState(identity) === 'matching') killTree(Number(identity.split(':')[0]));
    if (!this.running && !this.process && !this.info.process_identity) this.finish(false);
  }
  async delete() {
    if (this.deleted) return;
    if (this.id === 1) throw new Error('Cannot delete secretary');
    if (this.deleting) throw new Error('Удаление сессии уже выполняется');
    this.deleting = true;
    try {
      db.transaction(() => {
        const row = this.info;
        // shortcut: without launch-time containment, even an exited root can have surviving descendants.
        if (!row || row.process_uncontained !== 0 || row.process_identity || row.deletion_processes || this.running || this.process)
          throw new Error('Не удалось подтвердить завершение всех процессов; сессия не удалена');
        db.query('UPDATE sessions SET parent_id = NULL WHERE parent_id = ?').run(this.id);
        db.query('DELETE FROM pending_messages WHERE session_id = ?').run(this.id);
        db.query('DELETE FROM events WHERE session_id = ?').run(this.id);
        db.query('DELETE FROM sessions WHERE id = ?').run(this.id);
      })();
      this.deleted = true;
      this.queue = [];
      this.listeners = [];
      this.onComplete = undefined;
    } finally { this.deleting = false; }
  }
  switchHarness(harness: string, model: string) { this.row.harness = harness; this.row.model = model; this.row.reasoning = null; this.row.harness_session_id = null; db.query('UPDATE sessions SET harness = ?, model = ?, harness_session_id = NULL, reasoning = NULL WHERE id = ?').run(harness, model, this.id); }

  setModel(model: string) { this.row.model = model; db.query('UPDATE sessions SET model = ? WHERE id = ?').run(model, this.row.id); }

  setName(name: unknown) {
    if (typeof name !== 'string' || !name.trim() || name.trim().length > 100) throw new Error('Название должно содержать от 1 до 100 символов');
    this.row.name = name.trim();
    db.query('UPDATE sessions SET name = ? WHERE id = ?').run(this.row.name, this.id);
  }

  setReasoning(reasoning: string | null) { this.row.reasoning = reasoning; db.query('UPDATE sessions SET reasoning = ? WHERE id = ?').run(reasoning, this.id); }

  onEvent(callback: (event: unknown) => void) { this.listeners.push(callback); }

  // Событие пишется в историю и рассылается слушателям. Событие очереди (type 'queue') только рассылается: в историю оно не попадает.
  private write(event: unknown, time: string) { db.query('INSERT INTO events VALUES (?, ?, ?)').run(this.row.id, JSON.stringify(event), time); }
  private publish(event: unknown) { if (this.deleted) return; for (const listener of this.listeners) listener(event); }
  private emit(event: unknown) {
    if (this.deleted) return;
    this.write(event, new Date().toISOString());
    this.publish(event);
  }
  private publishQueue() { this.publish({ type: 'queue', items: this.queued() }); }
  // Сообщения, которые ещё не дошли до адаптера, по порядку отправки (срочные впереди).
  queued() { return this.queue.map(({ id, text, images, urgent }) => ({ id, text, images, urgent })); }

  private deletionBlocked() {
    const processes = this.info.deletion_processes;
    if (!processes) return false;
    if ((JSON.parse(processes) as string[]).some(item => processAlive(item))) return true;
    db.query('UPDATE sessions SET process_identity = NULL, deletion_processes = NULL WHERE id = ?').run(this.id);
    return false;
  }

  // Обычное сообщение ждёт конца хода. Срочное (immediate) прерывает идущий ход и запускается сразу, в той же сессии.
  // Сообщение пишется в базу до возврата: клиент получает подтверждение только для сохранённого сообщения.
  send(text: string, input?: unknown, immediate = false) {
    if (typeof text !== 'string') throw new Error('Некорректный текст сообщения');
    const images = validateImages(input);
    if (!text.trim() && !images.length) throw new Error('Сообщение пустое');
    if (this.deleted) throw new Error('Session was deleted');
    if (this.deleting) throw new Error('Удаление сессии уже выполняется');
    if (this.deletionBlocked()) throw new Error('Ожидается завершение процессов сессии');
    this.stopped = false;
    immediate = immediate && this.running;
    const saved = db.query('INSERT INTO pending_messages (session_id, urgent, text, images) VALUES (?, ?, ?, ?)').run(this.id, immediate ? 1 : 0, text, JSON.stringify(images));
    const item: Pending = { id: Number(saved.lastInsertRowid), text, images, urgent: immediate };
    if (immediate) { this.queue.splice(this.urgent, 0, item); this.urgent++; }
    else this.queue.push(item);
    this.publishQueue();
    if (!this.running) void this.next();
    else if (immediate && !this.awaitingExit) this.interruptTurn();
  }

  // Срочное сообщение прерывает ход один раз. Следующие срочные сообщения не посылают новое прерывание: они ждут в очереди,
  // а дедлайн гарантирует, что ожидание не превысит interruptDeadlineMs, даже если адаптер не отвечает.
  private interruptTurn() {
    if (this.restarting) return;
    this.restarting = true;
    const child = this.process;
    this.interruptCurrent?.();
    setTimeout(() => { if (this.restarting && this.process === child) this.killProcess(); }, Session.interruptDeadlineMs);
  }

  // Адаптер может подключиться к ходу позже (например, Codex отвечает на turn/start не сразу). Если прерывание уже запрошено, отправляем его здесь.
  private armInterrupt(interrupt: () => void) {
    this.interruptCurrent = interrupt;
    if (this.restarting) interrupt();
  }

  // Берёт следующий ход. Все срочные сообщения, которые уже стоят в очереди, уходят одним ходом:
  // два срочных сообщения во время прерывания не ждут, пока пройдёт следующий ход.
  // Следующий ход берётся из начала очереди. Сообщения остаются в очереди, пока адаптер их не принял (см. commit).
  private peek(): Pending[] {
    return this.queue.slice(0, this.queue[0]?.urgent ? this.urgent : 1);
  }

  // Подтверждение доставки: запись в историю и удаление из очереди и базы идут одной транзакцией.
  // Сообщение не может попасть в историю и остаться в очереди одновременно; при сбое до этого момента оно остаётся в очереди.
  private commit(items: Pending[]) {
    if (this.deleted) return;
    // Сообщение, отменённое stop() во время запуска, уже не в очереди и в историю не попадает.
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

  private async next() {
    const items = this.peek();
    if (!items.length) return;
    this.activeItems = items;
    this.dispatched = false;
    // Несколько срочных сообщений склеиваются в один ход, но в истории остаются отдельными сообщениями.
    const text = items.map(item => item.text).filter(Boolean).join('\n\n');
    const images = items.flatMap(item => item.images);
    this.running = true;
    this.lastAnswer = '';
    const alreadyRunning = this.info.status === 'running';
    // This must be durable before either adapter can spawn a process.
    const marked = db.query("UPDATE sessions SET status = 'running', process_uncontained = 1 WHERE id = ?").run(this.row.id);
    if (marked.changes !== 1) { this.running = false; return; }
    if (!alreadyRunning) this.emit({ type: 'status', status: 'running' });
    // delivered: адаптер принял сообщения. Пока false, при сбое они остаются в очереди.
    let delivered = false;
    const deliver = () => { if (delivered) return; delivered = true; this.commit(items); };
    // Записываем до передачи в stdin: после сбоя повтор опасен даже без подтверждения приёма.
    const dispatch = () => {
      db.transaction(() => { for (const item of items) db.query('UPDATE pending_messages SET in_flight = 1 WHERE id = ?').run(item.id); })();
      this.dispatched = true;
    };
    try {
    if (this.row.harness === 'codex') {
      const completed = await codexTurn({ folder: this.row.folder, model: this.row.model, reasoning: this.row.reasoning, threadId: this.row.harness_session_id,
        onThread: id => { if (this.deleted) return; this.row.harness_session_id = id; db.query('UPDATE sessions SET harness_session_id = ? WHERE id = ?').run(id, this.row.id); },
        secretary: this.row.id === 1, onProcess: process => this.recordProcess(process), onInterruptable: interrupt => this.armInterrupt(interrupt), onAccepted: deliver, onDispatching: dispatch, shouldCancel: () => this.userInterrupted,
        onEvent: event => { if (event.method === 'item/completed' && event.params.item.type === 'agentMessage') this.lastAnswer = event.params.item.text; this.emit(event); } }, text, images);
      this.finish(completed, delivered);
      return;
    }
    const args = ['claude', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose',
      '--dangerously-skip-permissions', '--model', this.row.model];
    if (this.row.reasoning) args.push('--effort', this.row.reasoning);
    if (this.row.harness_session_id) args.push('--resume', this.row.harness_session_id);
    if (this.row.id === 1) args.push('--mcp-config', JSON.stringify({mcpServers:{secretary:{type:'http',url:'http://127.0.0.1:3001/mcp'}}}));
    const process = Bun.spawn(globalThis.process.platform === 'linux' ? ['setsid', ...args] : args, { cwd: this.row.folder, stdin: 'pipe', stdout: 'pipe', stderr: 'inherit' });
    this.recordProcess(process);
    dispatch();
    process.stdin.write(JSON.stringify({ type: 'user', message: { role: 'user', content: images.length ? [{type:'text', text}, ...images.map(image => ({type:'image', source:{type:'base64', media_type:image.type, data:image.data}}))] : text } }) + '\n');
    process.stdin.flush();
    this.armInterrupt(() => {
      try { process.stdin.write(JSON.stringify({ type: 'control_request', request_id: crypto.randomUUID(), request: { subtype: 'interrupt' } }) + '\n'); process.stdin.flush(); } catch {}
    });
    let buffer = '';
    let completed = false;
    for await (const chunk of process.stdout) {
      buffer += new TextDecoder().decode(chunk, { stream: true });
      let end: number;
      while ((end = buffer.indexOf('\n')) >= 0) {
        if (this.deleted) break;
        const line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
        if (!line.trim()) continue;
        const event = JSON.parse(line);
        if (event.session_id && event.session_id !== this.row.harness_session_id) {
          this.row.harness_session_id = event.session_id;
          db.query('UPDATE sessions SET harness_session_id = ? WHERE id = ?').run(event.session_id, this.row.id);
        }
        // Claude не присылает отдельного подтверждения приёма. Первое событие процесса после записи в stdin
        // означает, что сообщение прочитано, поэтому считаем его принятым до записи этого события в историю.
        deliver();
        this.emit(event);
        if (event.type === 'result') { this.lastAnswer = event.result || ''; completed = !event.is_error; process.kill(); }
      }
    }
    await process.exited;
    // Если агент завершился без подтверждения, показываем ошибку и неопределённый статус доставки.
    if (!delivered && !completed && !this.restarting && !this.stopped) this.emit({ type: 'error', message: 'Агент завершился без подтверждения сообщения' });
    this.finish(completed, delivered);
    } catch (error) {
      // Ошибку прерывания (например, процесс убит по дедлайну) не показываем: ход запускается заново.
      if (!this.restarting) this.emit({ type: 'error', message: String(error) });
      const child = this.process;
      try { this.killProcess(); } catch {}
      let timer: ReturnType<typeof setTimeout> | undefined;
      const terminationConfirmed = !child || await Promise.race([
        child.exited.then(() => true, () => false),
        new Promise<boolean>(resolve => { timer = setTimeout(() => resolve(false), 1000); }),
      ]);
      if (timer) clearTimeout(timer);
      this.finish(false, delivered, terminationConfirmed);
    }
  }

  // После перезапуска программы подтверждённые, но ещё не начатые сообщения запускаются сразу.
  resumePending() {
    if (!this.running && !this.deleted && !this.deletionBlocked() && this.queue.length) void this.next();
  }

  // delivered: сообщения хода дошли до адаптера (даже если ход завершился ошибкой).
  private finish(completed: boolean, delivered = false, terminationConfirmed = true) {
    const unresolved = this.userInterrupted || (this.dispatched && !delivered) ? this.finishInterruptedItems() : 0;
    // Прерванный ради срочного сообщения ход не считается завершённым: отчёт родителю не уходит, статус остаётся running.
    const restart = !this.stopped && !this.deleting && this.restarting && !completed && this.queue.length > 0;
    const userInterrupted = this.userInterrupted;
    const interruptRequestId = this.interruptRequestId;
    this.userInterrupted = false;
    this.interruptRequestId = undefined;
    this.activeItems = [];
    this.restarting = false;
    this.interruptCurrent = undefined;
    if (this.deleted) { this.running = false; return; }
    const reportInterrupt = () => { if (interruptRequestId) this.publish({type:'interrupt_result', requestId:interruptRequestId, outcome:!terminationConfirmed ? 'unknown' : completed ? 'completed' : 'ended'}); };
    const child = this.process;
    if (!terminationConfirmed && child) {
      this.awaitingExit = true;
      db.query("UPDATE sessions SET status = 'interrupted' WHERE id = ?").run(this.id);
      this.emit({type:'status', status:'interrupted'});
      reportInterrupt();
      child.exited.then(() => {
        if (this.deleted || this.process !== child) return;
        this.awaitingExit = false;
        this.process = undefined;
        this.running = false;
        if (!this.deleting && !this.info.deletion_processes) db.query('UPDATE sessions SET process_identity = NULL WHERE id = ?').run(this.id);
        if (this.stopped) this.finish(false);
        else if (this.queue.length) void this.next();
      }).catch(() => {});
      return;
    }
    this.process = undefined;
    if (!this.deleting && !this.info.deletion_processes) db.query('UPDATE sessions SET process_identity = NULL WHERE id = ?').run(this.id);
    if (restart) { reportInterrupt(); this.running = false; void this.next(); return; }
    const status = this.stopped ? 'stopped' : completed || (userInterrupted && terminationConfirmed) ? 'idle' : 'interrupted';
    db.query('UPDATE sessions SET status = ? WHERE id = ?').run(status, this.row.id);
    this.emit({ type: 'status', status });
    reportInterrupt();
    if (completed && !userInterrupted && this.row.parent_id) this.onComplete?.(this.lastAnswer);
    this.running = false;
    // После неопределённой доставки продолжаем очередь, но не повторяем само сообщение.
    // Сбой до отправки оставляет его в очереди, чтобы не повторять неудачный запуск по кругу.
    if (!this.stopped && !this.deleting && (completed || delivered || (this.dispatched && unresolved)) && this.queue.length) void this.next();
  }
}
