import { codexTurn } from './codex';
import { Database } from 'bun:sqlite';

const db = new Database('secretary.sqlite', { create: true });
db.run(`CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL,
  folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL
)`);
db.run(`CREATE TABLE IF NOT EXISTS events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)`);

type SessionRow = { id: number; harness: string; model: string; folder: string; harness_session_id: string | null; status: string };

export class Session {
  private queue: string[] = [];
  private running = false;
  private listeners: ((event: unknown) => void)[] = [];

  constructor(private row: SessionRow) {}

  static create(model: string, folder: string, harness = 'claude') {
    const result = db.query("INSERT INTO sessions (harness, model, folder, status) VALUES (?, ?, ?, 'idle')").run(harness, model, folder);
    return Session.get(Number(result.lastInsertRowid))!;
  }

  static get(id: number) {
    const row = db.query('SELECT * FROM sessions WHERE id = ?').get(id) as SessionRow | null;
    return row && new Session(row);
  }

  get id() { return this.row.id; }

  setModel(model: string) { this.row.model = model; db.query('UPDATE sessions SET model = ? WHERE id = ?').run(model, this.row.id); }

  onEvent(callback: (event: unknown) => void) { this.listeners.push(callback); }

  private emit(event: unknown) {
    db.query('INSERT INTO events VALUES (?, ?, ?)').run(this.row.id, JSON.stringify(event), new Date().toISOString());
    for (const listener of this.listeners) listener(event);
  }

  send(text: string) {
    this.queue.push(text);
    if (!this.running) void this.next();
  }

  private async next() {
    const text = this.queue.shift();
    if (text === undefined) return;
    this.running = true;
    db.query("UPDATE sessions SET status = 'running' WHERE id = ?").run(this.row.id);
    this.emit({ type: 'user', message: { role: 'user', content: text } });
    if (this.row.harness === 'codex') {
      const completed = await codexTurn({ folder: this.row.folder, model: this.row.model, threadId: this.row.harness_session_id,
        onThread: id => { this.row.harness_session_id = id; db.query('UPDATE sessions SET harness_session_id = ? WHERE id = ?').run(id, this.row.id); },
        onEvent: event => this.emit(event) }, text);
      this.finish(completed);
      return;
    }
    const args = ['claude', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose',
      '--dangerously-skip-permissions', '--model', this.row.model];
    if (this.row.harness_session_id) args.push('--resume', this.row.harness_session_id);
    const process = Bun.spawn(args, { cwd: this.row.folder, stdin: 'pipe', stdout: 'pipe', stderr: 'inherit' });
    process.stdin.write(JSON.stringify({ type: 'user', message: { role: 'user', content: text } }) + '\n');
    process.stdin.flush();
    let buffer = '';
    let completed = false;
    for await (const chunk of process.stdout) {
      buffer += new TextDecoder().decode(chunk, { stream: true });
      let end: number;
      while ((end = buffer.indexOf('\n')) >= 0) {
        const line = buffer.slice(0, end); buffer = buffer.slice(end + 1);
        if (!line.trim()) continue;
        const event = JSON.parse(line);
        if (event.session_id && event.session_id !== this.row.harness_session_id) {
          this.row.harness_session_id = event.session_id;
          db.query('UPDATE sessions SET harness_session_id = ? WHERE id = ?').run(event.session_id, this.row.id);
        }
        this.emit(event);
        if (event.type === 'result') { completed = true; process.kill(); }
      }
    }
    await process.exited;
    this.finish(completed);
  }

  private finish(completed: boolean) {
    db.query('UPDATE sessions SET status = ? WHERE id = ?').run(completed ? 'idle' : 'interrupted', this.row.id);
    this.emit({ type: 'status', status: completed ? 'idle' : 'interrupted' });
    this.running = false;
    if (completed) void this.next();
  }
}
