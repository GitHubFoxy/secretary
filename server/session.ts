import { codexTurn } from './codex';
import { Database } from 'bun:sqlite';
import { readFileSync, readdirSync } from 'node:fs';

export const db = new Database('secretary.sqlite', { create: true });
db.run(`CREATE TABLE IF NOT EXISTS sessions (
  id INTEGER PRIMARY KEY, parent_id INTEGER, harness TEXT NOT NULL, model TEXT NOT NULL,
  folder TEXT NOT NULL, harness_session_id TEXT, status TEXT NOT NULL
)`);
try { db.run('ALTER TABLE sessions ADD COLUMN process_identity TEXT'); } catch {}
try { db.run('ALTER TABLE sessions ADD COLUMN name TEXT'); } catch {}
try { db.run('ALTER TABLE sessions ADD COLUMN reasoning TEXT'); } catch {}
db.run(`CREATE TABLE IF NOT EXISTS events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)`);

export type SessionRow = { id: number; parent_id: number | null; harness: string; model: string; folder: string; harness_session_id: string | null; status: string; name: string | null; reasoning: string | null };

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
  private queue: string[] = [];
  private running = false;
  private process: any;
  private stopped = false;
  private deleted = false;
  private lastAnswer = '';
  onComplete?: (answer: string) => void;
  private listeners: ((event: unknown) => void)[] = [];

  constructor(private row: SessionRow) {}

  static create(model: string, folder: string, harness = 'claude', parentId: number | null = null, name: string | null = null, reasoning: string | null = null) {
    const result = db.query("INSERT INTO sessions (harness, model, folder, status, parent_id, name, reasoning) VALUES (?, ?, ?, 'idle', ?, ?, ?)").run(harness, model, folder, parentId, name, reasoning);
    return Session.get(Number(result.lastInsertRowid))!;
  }

  static get(id: number) {
    const row = db.query('SELECT * FROM sessions WHERE id = ?').get(id) as SessionRow | null;
    return row && new Session(row);
  }

  get id() { return this.row.id; }
  private recordProcess(child: any) {
    this.process = child;
    if (process.platform === 'linux') {
      const stat = readFileSync(`/proc/${child.pid}/stat`, 'utf8');
      const start = stat.slice(stat.lastIndexOf(')') + 2).split(' ')[19];
      db.query('UPDATE sessions SET process_identity = ? WHERE id = ?').run(`${child.pid}:${start}`, this.id);
    }
  }
  static cleanInterrupted() {
    if (process.platform !== 'linux') return;
    const rows = db.query("SELECT process_identity FROM sessions WHERE status = 'running' AND process_identity IS NOT NULL").all() as {process_identity:string}[];
    for (const row of rows) {
      const [pid, start] = row.process_identity.split(':');
      try {
        const stat = readFileSync(`/proc/${pid}/stat`, 'utf8');
        if (stat.slice(stat.lastIndexOf(')') + 2).split(' ')[19] === start) {
          killTree(Number(pid));
        }
      } catch {}
    }
  }
  get info() { return db.query('SELECT * FROM sessions WHERE id = ?').get(this.id) as SessionRow; }
  events() { return (db.query('SELECT json FROM events WHERE session_id = ? ORDER BY rowid').all(this.id) as { json: string }[]).map(row => JSON.parse(row.json)); }
  stop() { this.stopped = true; this.queue = []; if (this.process) { try { if (process.platform === 'linux') killTree(this.process.pid); else this.process.kill(); } catch { this.process.kill(); } } if (!this.running) this.finish(false); }
  delete() {
    if (this.deleted) return;
    if (this.id === 1) throw new Error('Cannot delete secretary');
    this.stop();
    db.transaction(() => {
      db.query('UPDATE sessions SET parent_id = NULL WHERE parent_id = ?').run(this.id);
      db.query('DELETE FROM events WHERE session_id = ?').run(this.id);
      db.query('DELETE FROM sessions WHERE id = ?').run(this.id);
    })();
    this.deleted = true;
    this.listeners = [];
    this.onComplete = undefined;
  }
  switchHarness(harness: string, model: string) { this.row.harness = harness; this.row.model = model; this.row.reasoning = null; this.row.harness_session_id = null; db.query('UPDATE sessions SET harness = ?, model = ?, harness_session_id = NULL, reasoning = NULL WHERE id = ?').run(harness, model, this.id); }

  setModel(model: string) { this.row.model = model; db.query('UPDATE sessions SET model = ? WHERE id = ?').run(model, this.row.id); }

  onEvent(callback: (event: unknown) => void) { this.listeners.push(callback); }

  private emit(event: unknown) {
    if (this.deleted) return;
    db.query('INSERT INTO events VALUES (?, ?, ?)').run(this.row.id, JSON.stringify(event), new Date().toISOString());
    for (const listener of this.listeners) listener(event);
  }

  send(text: string) {
    if (this.deleted) throw new Error('Session was deleted');
    this.stopped = false;
    this.queue.push(text);
    if (!this.running) void this.next();
  }

  private async next() {
    const text = this.queue.shift();
    if (text === undefined) return;
    this.running = true;
    this.lastAnswer = '';
    db.query("UPDATE sessions SET status = 'running' WHERE id = ?").run(this.row.id);
    this.emit({ type: 'user', message: { role: 'user', content: text } });
    try {
    if (this.row.harness === 'codex') {
      const completed = await codexTurn({ folder: this.row.folder, model: this.row.model, reasoning: this.row.reasoning, threadId: this.row.harness_session_id,
        onThread: id => { if (this.deleted) return; this.row.harness_session_id = id; db.query('UPDATE sessions SET harness_session_id = ? WHERE id = ?').run(id, this.row.id); },
        secretary: this.row.id === 1, onProcess: process => this.recordProcess(process),
        onEvent: event => { if (event.method === 'item/completed' && event.params.item.type === 'agentMessage') this.lastAnswer = event.params.item.text; this.emit(event); } }, text);
      this.finish(completed);
      return;
    }
    const args = ['claude', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose',
      '--dangerously-skip-permissions', '--model', this.row.model];
    if (this.row.reasoning) args.push('--effort', this.row.reasoning);
    if (this.row.harness_session_id) args.push('--resume', this.row.harness_session_id);
    if (this.row.id === 1) args.push('--mcp-config', JSON.stringify({mcpServers:{secretary:{type:'http',url:'http://127.0.0.1:3001/mcp'}}}));
    const process = Bun.spawn(globalThis.process.platform === 'linux' ? ['setsid', ...args] : args, { cwd: this.row.folder, stdin: 'pipe', stdout: 'pipe', stderr: 'inherit' });
    this.recordProcess(process);
    process.stdin.write(JSON.stringify({ type: 'user', message: { role: 'user', content: text } }) + '\n');
    process.stdin.flush();
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
        this.emit(event);
        if (event.type === 'result') { this.lastAnswer = event.result || ''; completed = !event.is_error; process.kill(); }
      }
    }
    await process.exited;
    this.finish(completed);
    } catch (error) { this.emit({ type: 'error', message: String(error) }); this.finish(false); }
  }

  private finish(completed: boolean) {
    this.process = undefined;
    if (this.deleted) { this.running = false; return; }
    db.query('UPDATE sessions SET process_identity = NULL WHERE id = ?').run(this.id);
    const status = this.stopped ? 'stopped' : completed ? 'idle' : 'interrupted';
    db.query('UPDATE sessions SET status = ? WHERE id = ?').run(status, this.row.id);
    this.emit({ type: 'status', status });
    if (completed && this.row.parent_id) this.onComplete?.(this.lastAnswer);
    this.running = false;
    if (completed) void this.next();
  }
}
