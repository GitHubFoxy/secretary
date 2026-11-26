import type { Database } from 'bun:sqlite';
import { agentAnswer, chatTranscript } from './chat';
import { writeEvent } from './database';
import type { Turn, TurnOptions } from './harness';
import { validateImages } from './images';
import { filePath, validateFiles, type FileAttachment } from './files';
import { MessageQueue, type Message } from './queue';

export type SessionRow = { id: number; parent_id: number | null; harness: string; model: string; folder: string; branch: string | null; harness_session_id: string | null; status: string; name: string | null; reasoning: string | null; hidden: number };
export type Status = 'idle' | 'running' | 'interrupted' | 'stopped';
export type TurnContext = Pick<TurnOptions, 'mcpUrl' | 'mcpToken' | 'systemPrompt'>;

export const SECRETARY_ID = 1;

// The main Secretary Agent session and sessions inside its folder belong to Secretary itself, not to the owner's list.
export function isInternal(row: Pick<SessionRow, 'id' | 'folder'>) {
  return row.id === SECRETARY_ID || row.folder.split('/').includes('.secretary');
}

// Level-1 Specialist: direct child of Secretary Agent or a top-level session created by the Owner.
export function isSpecialist(row: Pick<SessionRow, 'id' | 'parent_id'>) {
  return row.id !== SECRETARY_ID && (row.parent_id === null || row.parent_id === SECRETARY_ID);
}

type ActiveTurn = {
  items: Message[];
  interrupt: AbortController;
  stop: AbortController;
  dispatched: boolean;
  accepted: boolean;
  // Interrupted for an urgent message: the queue goes on without idle status or a report to the parent.
  restarting: boolean;
  // Interrupted by the owner; the request id comes back in interrupt_result.
  userInterrupt?: { requestId?: string };
  answer: string;
};

export class Session {
  readonly queue: MessageQueue;
  private turn?: ActiveTurn;
  private stopped = false;
  private deleted = false;
  private listeners: ((event: any, cursor?: number) => void)[] = [];
  // Called with the last answer when a turn completes on its own.
  onComplete?: (answer: string) => void;

  constructor(
    private db: Database,
    private row: SessionRow,
    private turns: Record<string, Turn>,
    private turnContext: (row: SessionRow) => TurnContext = () => ({}),
  ) {
    this.queue = new MessageQueue(db, row.id, (event, cursor) => this.publish(event, cursor));
  }

  get id() { return this.row.id; }
  get info() { return this.db.query('SELECT * FROM sessions WHERE id = ?').get(this.id) as SessionRow; }
  events() { return (this.db.query('SELECT json FROM events WHERE session_id = ? ORDER BY rowid').all(this.id) as { json: string }[]).map(row => JSON.parse(row.json)); }
  onEvent(callback: (event: any, cursor?: number) => void) { this.listeners.push(callback); }

  setHidden(hidden: boolean) {
    if (isInternal(this.row) || typeof hidden !== 'boolean') throw new Error('Недопустимое изменение видимости сессии');
    this.db.query('UPDATE sessions SET hidden = ? WHERE id = ?').run(hidden ? 1 : 0, this.id);
  }

  setName(name: unknown) {
    if (typeof name !== 'string' || !name.trim() || name.trim().length > 100) throw new Error('Название должно содержать от 1 до 100 символов');
    this.row.name = name.trim();
    this.db.query('UPDATE sessions SET name = ? WHERE id = ?').run(this.row.name, this.id);
  }

  // Switching harness starts a new harness session: the old context cannot be resumed by the other harness.
  // The chat itself goes on; the next turn hands the new harness a transcript.
  configure(harness: string, model: string, reasoning: string | null) {
    if (this.turn) throw new Error('Сессия работает');
    const { harness: oldHarness, model: oldModel, reasoning: oldReasoning } = this.row;
    if (harness === oldHarness && model === oldModel && reasoning === oldReasoning) return;
    if (harness !== oldHarness) this.row.harness_session_id = null;
    Object.assign(this.row, { harness, model, reasoning });
    this.db.query('UPDATE sessions SET harness = ?, model = ?, reasoning = ?, harness_session_id = ? WHERE id = ?').run(harness, model, reasoning, this.row.harness_session_id, this.id);
    this.emit({ type: 'model_switch', from: { harness: oldHarness, model: oldModel, reasoning: oldReasoning }, to: { harness, model, reasoning } });
  }

  delete() {
    if (this.id === SECRETARY_ID) throw new Error('Нельзя удалить Secretary Agent');
    if (this.turn) throw new Error('Сначала остановите сессию');
    this.db.transaction(() => {
      this.db.query('UPDATE sessions SET parent_id = NULL WHERE parent_id = ?').run(this.id);
      this.queue.discard();
      this.db.query('DELETE FROM events WHERE session_id = ?').run(this.id);
      this.db.query('DELETE FROM notes WHERE session_id = ?').run(this.id);
      this.db.query('DELETE FROM drafts WHERE session_id = ?').run(this.id);
      this.db.query('DELETE FROM sessions WHERE id = ?').run(this.id);
    })();
    this.deleted = true;
    this.listeners = [];
    this.onComplete = undefined;
  }

  // An ordinary message waits for the turn to end. An immediate one interrupts the running turn and goes next.
  // The message is stored before returning: the client only gets a confirmation for a stored message.
  send(text: unknown, input?: unknown, immediate = false, voice = false, speechTarget?: (message: object) => void, inputFiles?: unknown) {
    if (typeof text !== 'string') throw new Error('Некорректный текст сообщения');
    const images = validateImages(input);
    const files = validateFiles(inputFiles);
    if (!text.trim() && !images.length && !files.length) throw new Error('Сообщение пустое');
    if (this.deleted) throw new Error('Сессия удалена');
    this.stopped = false;
    const urgent = immediate && !!this.turn;
    this.queue.add(text, images, urgent, voice, speechTarget, files);
    this.startOrInterrupt(urgent);
  }

  // Files from the agent appear in the chat like its answer; the turn goes on.
  showFiles(files: FileAttachment[], text: string) { this.emit({ type: 'agent_files', text, files }); }

  sendQueued(id: unknown) {
    this.queue.promote(id, this.turn?.items ?? []);
    this.stopped = false;
    this.startOrInterrupt(true);
  }

  private startOrInterrupt(urgent: boolean) {
    if (!this.turn) void this.next();
    else if (urgent) { this.turn.restarting = true; this.turn.interrupt.abort(); }
  }

  interrupt(requestId?: string) {
    if (!this.turn || this.turn.userInterrupt) return false;
    this.turn.userInterrupt = { requestId };
    this.turn.interrupt.abort();
    return true;
  }

  // Stopping cancels messages that have not reached the harness. They stay in history as not sent.
  stop() {
    this.stopped = true;
    const turn = this.turn;
    this.queue.dropAll(item => turn?.dispatched && turn.items.includes(item) ? 'delivery_uncertain' : 'cancelled');
    if (turn) turn.stop.abort();
    else this.setStatus('stopped');
  }

  resumePending() {
    if (!this.turn && !this.deleted && this.queue.length) void this.next();
  }

  private emit(event: unknown) {
    const cursor = writeEvent(this.db, this.id, event);
    this.publish(event, cursor);
  }
  private publish(event: unknown, cursor?: number) { for (const listener of this.listeners) listener(event, cursor); }

  private setStatus(status: Status) {
    if (this.info.status === status) return;
    this.db.query('UPDATE sessions SET status = ? WHERE id = ?').run(status, this.id);
    this.emit({ type: 'status', status });
  }

  private async next() {
    const items = this.queue.peek();
    if (!items.length) return;
    const turn: ActiveTurn = this.turn = { items, interrupt: new AbortController(), stop: new AbortController(), dispatched: false, accepted: false, restarting: false, answer: '' };
    this.setStatus('running');
    const speechTarget = this.id === SECRETARY_ID ? items.findLast(item => item.voice && item.speechTarget)?.speechTarget : undefined;
    if (speechTarget) this.publish({ type: 'speech_start', speechTarget });
    // Several urgent messages go as one turn but stay separate in history.
    const history = this.row.harness_session_id ? '' : chatTranscript(this.db, this.id);
    const text = history + items.map(item => [item.text, ...item.files.map(file => `Файл: ${filePath(file)} (${file.name}, ${file.size} байт)`)].filter(Boolean).join('\n')).filter(Boolean).join('\n\n');
    const images = items.flatMap(item => item.images);
    let completed = false;
    try {
      completed = await this.turns[this.row.harness]({
        ...this.turnContext(this.row),
        folder: this.row.folder, model: this.row.model, reasoning: this.row.reasoning, resumeId: this.row.harness_session_id, secretary: this.id === SECRETARY_ID,
        interrupt: turn.interrupt.signal, stop: turn.stop.signal,
        onSessionId: id => {
          if (id === this.row.harness_session_id) return;
          this.row.harness_session_id = id;
          this.db.query('UPDATE sessions SET harness_session_id = ? WHERE id = ?').run(id, this.id);
        },
        onDispatching: () => { this.queue.inFlight(items); turn.dispatched = true; },
        onAccepted: () => { turn.accepted = true; this.queue.accept(items); },
        onEvent: event => { turn.answer = agentAnswer(event) ?? turn.answer; if (speechTarget) this.publish({type:'speech_event', event}); this.emit(event); },
      }, text, images);
      if (!turn.accepted && !completed && !turn.interrupt.signal.aborted && !this.stopped) this.emit({ type: 'error', message: 'Агент завершился без подтверждения сообщения' });
    } catch (error) {
      // A turn interrupted for an urgent message restarts at once, so its error is not shown.
      if (!turn.restarting) this.emit({ type: 'error', message: String(error) });
    }
    if (speechTarget) this.publish({ type: 'speech_end', interrupted: !completed || turn.interrupt.signal.aborted || turn.stop.signal.aborted });
    this.finish(turn, completed);
  }

  private finish(turn: ActiveTurn, completed: boolean) {
    // Messages of the turn that never reached history: cancelled if the harness never saw them, uncertain if it might have.
    if (turn.userInterrupt || (turn.dispatched && !turn.accepted)) this.queue.drop(turn.items, turn.dispatched ? 'delivery_uncertain' : 'cancelled');
    const requestId = turn.userInterrupt?.requestId;
    if (requestId) this.publish({ type: 'interrupt_result', requestId, outcome: completed ? 'completed' : 'ended' });
    this.turn = undefined;
    // A turn interrupted for an urgent message is not finished: status stays running, no report.
    if (!this.stopped && turn.restarting && !completed && this.queue.length) { void this.next(); return; }
    this.setStatus(this.stopped ? 'stopped' : completed || turn.userInterrupt ? 'idle' : 'interrupted');
    // The queue goes on once the turn's messages have left it, whichever way. A failure before dispatch keeps them queued,
    // so a broken launch does not loop; resumePending or the next message retries.
    if (!this.stopped && !turn.items.some(item => this.queue.has(item)) && this.queue.length) void this.next();
    if (completed && !turn.userInterrupt) {
      try { this.onComplete?.(turn.answer); } catch (error) { console.error('Report failed', error); }
    }
  }
}
