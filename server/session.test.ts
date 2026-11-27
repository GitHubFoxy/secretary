import { expect, test } from 'bun:test';
import type { Database } from 'bun:sqlite';
import { openDatabase } from './database';
import { getDraft, saveDraft } from './drafts';
import { listNotes, saveNote, deleteNote } from './notes';
import type { Turn, TurnOptions } from './harness';
import type { Session } from './session';
import { Sessions } from './sessions';

type Call = { text: string; options: TurnOptions; start(): void; answer(text: string): void; end(completed: boolean): void; fail(): void };

function setup(db: Database = openDatabase(':memory:')) {
  const calls: Call[] = [];
  const published: any[] = [];
  // Like a real harness, a started turn reports its session id, so the next turn resumes it.
  const turn: Turn = (options, text) => new Promise((resolve, reject) => calls.push({
    text, options,
    start() { options.onSessionId(options.resumeId ?? `thread-${calls.length}`); options.onDispatching(); options.onAccepted(); },
    answer(answer) { options.onEvent({ type: 'result', result: answer }); resolve(true); },
    end: resolve,
    fail: () => reject(new Error('spawn failed')),
  }));
  const sessions = new Sessions(db, {
    turns: { claude: turn, codex: turn },
    checkModel: async (_, model) => { if (model === 'bad') throw new Error('Unsupported model'); },
    onEvent: (_, event) => published.push(event),
    onChange() {},
  });
  sessions.load('/home/u/.secretary');
  const create = (extra = {}) => sessions.create({ harness: 'claude', model: 'm', reasoning: null, folder: '/tmp', ...extra });
  return { db, calls, published, sessions, create };
}
const tick = () => Bun.sleep(0);
const history = (s: Session) => s.events().filter(e => ['user', 'cancelled', 'delivery_uncertain'].includes(e.type)).map(e => `${e.type} ${e.text ?? e.message.content}`);
const statuses = (s: Session) => s.events().filter(e => e.type === 'status').map(e => e.status);
const pending = (db: Database) => (db.query('SELECT COUNT(*) AS n FROM pending_messages').get() as { n: number }).n;

test('событие ответа публикуется только после успешного завершения Secretary Agent', async () => {
  const { sessions, calls, published, create } = setup();
  const secretary = sessions.get(1);
  secretary.send('привет'); calls[0].start();
  calls[0].options.onEvent({type:'result', result:'# Ответ\n- **Ссылка**: [сайт](https://example.org)\n- `команда`'});
  expect(published.filter(e => e.type === 'secretary_reply')).toEqual([]);
  calls[0].end(true); await tick();
  expect(published.filter(e => e.type === 'secretary_reply')).toEqual([{type:'secretary_reply', text:'Ответ\n• Ссылка: сайт\n• команда'}]);
  const specialist = await create();
  specialist.send('задача'); calls[1].start(); calls[1].answer('готово'); await tick();
  secretary.send('ещё'); calls[2].start(); calls[2].options.onEvent({type:'result', result:'черновик'}); calls[2].end(false); await tick();
  expect(published.filter(e => e.type === 'secretary_reply')).toHaveLength(1);
});

test('выбранное сообщение из очереди прерывает ход, остальные сохраняют порядок', async () => {
  const { db, calls, create } = setup();
  const s = await create();
  s.send('ход'); calls[0].start();
  s.send('первое'); s.send('выбранное'); s.send('последнее');
  const id = s.queue.list()[1].id;
  s.sendQueued(id);
  expect(calls[0].options.interrupt.aborted).toBe(true);
  expect(s.queue.list().map(m => m.text)).toEqual(['выбранное', 'первое', 'последнее']);
  expect((db.query('SELECT urgent FROM pending_messages WHERE id = ?').get(id) as { urgent: number }).urgent).toBe(1);
  calls[0].end(false); await tick();
  expect(calls[1].text).toBe('выбранное');
  calls[1].start(); calls[1].answer('ok'); await tick();
  expect(calls[2].text).toBe('первое');
  calls[2].start(); calls[2].answer('ok'); await tick();
  expect(calls[3].text).toBe('последнее');
  expect(() => s.sendQueued(id)).toThrow();
});

test('сообщение активного хода нельзя отправить повторно из очереди', async () => {
  const { calls, create } = setup();
  const s = await create();
  s.send('ход');
  const id = s.queue.list()[0].id;
  calls[0].options.onDispatching();
  expect(() => s.sendQueued(id)).toThrow();
  expect(calls[0].options.interrupt.aborted).toBe(false);
});

test('Immediate message прерывает ход и идёт раньше обычных, срочные уходят одним ходом', async () => {
  const { db, calls, create } = setup();
  const s = await create();
  s.send('долгий');
  calls[0].start();
  s.send('обычное');
  s.send('срочно 1', undefined, true);
  s.send('срочно 2', undefined, true);
  expect(calls[0].options.interrupt.aborted).toBe(true);
  calls[0].end(false); await tick();
  expect(calls[1].text).toBe('срочно 1\n\nсрочно 2');
  calls[1].start(); calls[1].answer('ok'); await tick();
  calls[2].start(); calls[2].answer('ok'); await tick();
  expect(history(s)).toEqual(['user долгий', 'user срочно 1', 'user срочно 2', 'user обычное']);
  expect(statuses(s)).toEqual(['running', 'idle', 'running', 'idle']);
  expect(pending(db)).toBe(0);
});

test('Immediate message до отправки не теряет прерванное сообщение', async () => {
  const { calls, create } = setup();
  const s = await create();
  s.send('первое');
  s.send('срочно', undefined, true);
  calls[0].end(false); await tick();
  expect(calls[1].text).toBe('срочно');
  calls[1].start(); calls[1].answer('ok'); await tick();
  expect(calls[2].text).toBe('первое');
});

test('прерывание: до отправки — отмена, после отправки без приёма — неизвестно, после приёма — в истории', async () => {
  const { calls, published, create } = setup();
  const s = await create();
  expect(s.interrupt('idle')).toBe(false);
  s.send('a');
  expect(s.interrupt('r1')).toBe(true);
  expect(s.interrupt('r2')).toBe(false);
  calls[0].end(false); await tick();
  s.send('b');
  calls[1].options.onDispatching();
  s.interrupt();
  calls[1].end(false); await tick();
  s.send('c');
  calls[2].start();
  s.send('d');
  s.interrupt('r3');
  calls[2].answer('успел'); await tick();
  expect(calls[3].text).toBe('d');
  expect(history(s)).toEqual(['cancelled a', 'delivery_uncertain b', 'user c']);
  expect(published.filter(e => e.type === 'interrupt_result')).toEqual([
    { type: 'interrupt_result', requestId: 'r1', outcome: 'ended' },
    { type: 'interrupt_result', requestId: 'r3', outcome: 'completed' },
  ]);
});

test('остановка отменяет очередь и в базе, ход убивается', async () => {
  const { db, calls, create } = setup();
  const s = await create();
  s.send('a'); calls[0].start();
  s.send('b');
  s.stop();
  expect(calls[0].options.stop.aborted).toBe(true);
  calls[0].end(false); await tick();
  expect(history(s)).toEqual(['user a', 'cancelled b']);
  expect(s.info.status).toBe('stopped');
  expect(pending(db)).toBe(0);
});

test('сообщение, отправленное во время завершения остановленного хода, уходит после него', async () => {
  const { calls, create } = setup();
  const s = await create();
  s.send('a');
  s.stop();
  s.send('b');
  calls[0].end(false); await tick();
  expect(calls.map(c => c.text)).toEqual(['a', 'b']);
});

test('сбой запуска оставляет сообщение в очереди, сбой после отправки не останавливает очередь', async () => {
  const { calls, create } = setup();
  const s = await create();
  s.send('a');
  calls[0].fail(); await tick();
  expect(s.info.status).toBe('interrupted');
  expect(s.queue.list().map(m => m.text)).toEqual(['a']);
  expect(s.events().some(e => e.type === 'error')).toBe(true);
  s.resumePending();
  calls[1].start(); calls[1].end(false); await tick();
  s.send('b');
  calls[2].options.onDispatching();
  s.send('c');
  calls[2].end(false); await tick();
  calls[3].start(); calls[3].answer('ok'); await tick();
  expect(history(s)).toEqual(['user a', 'delivery_uncertain b', 'user c']);
});

test('очередь переживает перезапуск, отправленное до падения не повторяется', async () => {
  const before = setup();
  const s = await before.create();
  s.send('a'); before.calls[0].options.onDispatching();
  s.send('b');
  const after = setup(before.db);
  const restored = after.sessions.get(s.id);
  expect(restored.info.status).toBe('interrupted');
  expect(history(restored)).toEqual(['delivery_uncertain a']);
  after.sessions.resumePending();
  expect(after.calls.map(c => c.text)).toEqual(['b']);
});

test('отчёт Specialist доходит до родителя, даже если родителя удалили — без ошибок', async () => {
  const { calls, sessions, create } = setup();
  const parent = await create();
  const child = await create({ parentId: parent.id, name: 'Специалист' });
  child.send('задача'); calls[0].start(); calls[0].answer('готово'); await tick();
  expect(calls[1].text).toBe(`[report #${child.id} claude /tmp] готово`);
  calls[1].start(); calls[1].answer('NO_REPLY'); await tick();
  child.send('ещё'); calls[2].start();
  sessions.delete(parent.id);
  calls[2].answer('готово'); await tick();
  expect(calls).toHaveLength(3);
  expect(child.info.parent_id).toBeNull();
});

test('правила сессий: Secretary Agent, видимость, имя, настройки', async () => {
  const { calls, sessions, create } = setup();
  const secretary = sessions.get(1);
  expect(sessions.list()[0]).toMatchObject({ id: 1, secretary: true, internal: true });
  secretary.send('привет');
  expect(calls[0].options.systemPrompt).toContain('Говори как живой человек');
  calls[0].start(); calls[0].answer('ok'); await tick();
  calls.length = 0;
  expect(() => sessions.delete(1)).toThrow();
  expect(() => secretary.setHidden(true)).toThrow();
  expect(() => secretary.setName('   ')).toThrow();
  await expect(create({ folder: '/home/u/.secretary/x' }).then(s => s.setHidden(true))).rejects.toThrow();
  await expect(create({ model: 'bad', reasoning: 'high' })).rejects.toThrow('Unsupported model');
  await expect(create({ harness: 'nope' })).rejects.toThrow();

  const s = await create({ harness: 'codex' });
  s.setHidden(true);
  expect(s.info.hidden).toBe(1);
  s.send('a'); calls[0].options.onSessionId('thread-1');
  await expect(sessions.configure(s.id, { harness: 'claude', model: 'm', reasoning: null })).rejects.toThrow();
  calls[0].start(); calls[0].answer('ok'); await tick();
  await sessions.configure(s.id, { harness: 'claude', model: 'sonnet', reasoning: null });
  expect(s.info).toMatchObject({ harness: 'claude', model: 'sonnet', harness_session_id: null });
});

test('NO_REPLY у Specialist не отправляет отчёт родителю, а stop каскадно останавливает детей', async () => {
  const { calls, sessions, create } = setup();
  const specialist = await create({ parentId: 1, name: 'Проект' });
  const child = await create({ parentId: specialist.id, name: 'Фича' });

  specialist.send('проверь');
  expect(calls[0].options.systemPrompt).toContain('Ты — Specialist');
  expect(calls[0].options.mcpUrl).toMatch(/\/mcp\/2$/);
  calls[0].start(); calls[0].answer('NO_REPLY'); await tick();
  expect(calls).toHaveLength(1);

  child.send('задача');
  expect(calls[1].options.mcpUrl).toBeUndefined();
  expect(calls[1].options.systemPrompt).toBeUndefined();
  calls[1].start();

  sessions.stop(specialist.id);
  expect(calls[1].options.stop.aborted).toBe(true);
});


test('сообщение голосового режима хранит получателя до начала хода, но не после перезапуска', async () => {
  const before = setup();
  const s = before.sessions.get(1);
  s.send('первое'); before.calls[0].start();
  const target = () => {};
  const otherTarget = () => {};
  s.send('голосовое', undefined, false, true, target);
  s.send('ещё голосовое', undefined, false, true, otherTarget);
  expect(before.published.some(event => event.type === 'speech_start')).toBe(false);
  before.calls[0].end(true); await tick();
  expect(before.published.find(event => event.type === 'speech_start').speechTarget).toBe(target);
  before.calls[1].start();
  before.calls[1].options.onEvent({type:'assistant', message:{content:[{type:'text', text:'Привет'}]}});
  expect(before.published.some(event => event.type === 'speech_event')).toBe(true);
  before.calls[1].end(true); await tick();
  expect(before.published.some(event => event.type === 'speech_end' && !event.interrupted)).toBe(true);
  expect(before.published.filter(event => event.type === 'speech_start').map(event => event.speechTarget)).toEqual([target, otherTarget]);
  before.calls[2].start(); before.calls[2].end(true); await tick();
  const pending = setup();
  pending.sessions.get(1).send('первое'); pending.calls[0].start();
  pending.sessions.get(1).send('голосовое', undefined, false, true, target);
  const after = setup(pending.db);
  expect(after.sessions.get(1).queue.list()[0].voice).toBe(true);
  after.sessions.resumePending();
  expect(after.published.some(event => event.type === 'speech_start')).toBe(false);
  after.calls[0].start();
  after.calls[0].options.onEvent({type:'assistant', message:{content:[{type:'text', text:'Привет'}]}});
  expect(after.published.some(event => event.type === 'speech_event')).toBe(false);
  after.calls[0].end(true); await tick();
  expect(after.published.some(event => event.type === 'speech_end')).toBe(false);
});

test('смена harness продолжает чат: событие в истории, новый harness получает переписку, смена модели — нет', async () => {
  const { calls, sessions, create } = setup();
  const s = await create();
  s.send('первый вопрос'); calls[0].start();
  calls[0].options.onEvent({ type: 'tool_use', name: 'Bash' });
  calls[0].answer('первый ответ'); await tick();

  await sessions.configure(s.id, { harness: 'claude', model: 'sonnet', reasoning: null });
  s.send('второй'); calls[1].start(); calls[1].answer('ok'); await tick();
  expect(calls[1].options.resumeId).toBe('thread-1');
  expect(calls[1].text).toBe('второй');
  expect(calls[1].options.systemPrompt).toContain('Ты — Specialist');

  await sessions.configure(s.id, { harness: 'codex', model: 'gpt', reasoning: 'high' });
  await sessions.configure(s.id, { harness: 'codex', model: 'gpt', reasoning: 'high' });
  expect(s.events().filter(e => e.type === 'model_switch').map(e => `${e.from.model}→${e.to.model}`)).toEqual(['m→sonnet', 'sonnet→gpt']);
  s.send('третий');
  expect(calls[2].options.resumeId).toBeNull();
  expect(calls[2].options.systemPrompt).toContain('Ты — Specialist');
  expect(calls[2].text).toContain('Пользователь: первый вопрос\n\nАгент: первый ответ\n\nПользователь: второй\n\nАгент: ok');
  expect(calls[2].text).not.toContain('Bash');
  expect(calls[2].text.endsWith('</history>\n\nтретий')).toBe(true);
  calls[2].start(); calls[2].answer('ok'); await tick();
  await sessions.configure(s.id, { harness: 'claude', model: 'sonnet', reasoning: null });
  s.send('четвёртый');
  expect(calls[3].options.resumeId).toBeNull();
  expect(calls[3].options.systemPrompt).toContain('Ты — Specialist');
  expect(calls[3].text).toContain('первый вопрос');
});


test('заметки сохраняются, обновляются и удаляются вместе с Session', async () => {
  const {db, sessions, create} = setup();
  const session = await create();
  saveNote(db, session.id, 'a', 'первая');
  expect(listNotes(db, session.id).map((note: any) => note.text)).toEqual(['первая']);
  saveNote(db, session.id, 'a', 'обновлённая');
  expect(listNotes(db, session.id).map((note: any) => note.text)).toEqual(['обновлённая']);
  deleteNote(db, session.id, 'a');
  expect(listNotes(db, session.id)).toEqual([]);
  saveNote(db, session.id, 'b', 'другая');
  sessions.delete(session.id);
  expect(listNotes(db, session.id)).toEqual([]);
});

test('после перезапуска ход с сессией harness продолжается автоматически', async () => {
  const first = setup();
  const s = await first.create();
  s.send('задача'); first.calls[0].start();
  const restored = setup(first.db);
  expect(restored.calls).toHaveLength(0);
  restored.sessions.resumePending();
  expect(restored.calls).toHaveLength(1);
  expect(restored.calls[0].options.resumeId).toBe('thread-1');
  expect(restored.calls[0].text).toBe('Secretary перезапустился посреди твоего хода. Продолжи работу с того места, где остановился; если всё уже сделано — коротко подведи итог.');
  expect(restored.sessions.get(s.id).info.status).toBe('running');
  restored.calls[0].start(); restored.calls[0].answer('готово'); await tick();
  expect(restored.sessions.get(s.id).info.status).toBe('idle');
  expect(restored.sessions.chatHistory(s.id, 20).some(m => m.kind === 'user' && m.text.includes('Secretary перезапустился'))).toBe(false);
});

test('после перезапуска ход без сессии harness остаётся прерванным', async () => {
  const first = setup();
  const s = await first.create();
  s.send('задача'); first.calls[0].options.onDispatching();
  const restored = setup(first.db);
  restored.sessions.resumePending();
  expect(restored.sessions.get(s.id).info.status).toBe('interrupted');
  expect(restored.calls).toHaveLength(0);
});

test('черновик сохраняется и удаляется вместе с Session', async () => {
  const {db, sessions, create} = setup();
  const session = await create();
  expect(getDraft(db, session.id)).toBe('');
  saveDraft(db, session.id, 'текст');
  expect(getDraft(db, session.id)).toBe('текст');
  saveDraft(db, session.id, '');
  expect(db.query('SELECT * FROM drafts WHERE session_id = ?').get(session.id)).toBeNull();
  saveDraft(db, session.id, 'снова');
  sessions.delete(session.id);
  expect(db.query('SELECT * FROM drafts WHERE session_id = ?').get(session.id)).toBeNull();
});

test('сжатие требует свободную непустую Session и сохраняет событие harness', async () => {
  const { calls, create } = setup();
  const s = await create();
  await expect(s.compact()).rejects.toThrow('Нет контекста');
  s.send('привет'); calls[0].start();
  await expect(s.compact()).rejects.toThrow('Сессия работает');
  calls[0].answer('ответ'); await tick();
  const compact = s.compact();
  expect(calls[1].options.compact).toBe(true);
  expect(calls[1].options.resumeId).toBe('thread-1');
  expect(calls[1].text).toBe('/compact');
  calls[1].start();
  calls[1].options.onEvent({type:'system', subtype:'compact_boundary'});
  calls[1].end(true); await compact;
  expect(s.events().some(e => e.subtype === 'compact_boundary')).toBe(true);
  expect(s.info.status).toBe('idle');
  expect(history(s)).toEqual(['user привет']);
});

test('лимит создаёт событие, отчёт родителю и переживающее перезапуск расписание', async () => {
  const { db, calls, sessions, create } = setup();
  const child = await create({parentId: 1, harness: 'codex'});
  const reset = Date.now() + 3600_000;
  child.send('задача'); calls[0].start();
  const error = {codexErrorInfo:'usageLimitExceeded', resetsAt: Math.floor(reset / 1000)};
  calls[0].options.onEvent({method:'error', params:{willRetry:false, error}});
  calls[0].options.onEvent({method:'turn/completed', params:{turn:{status:'failed', error}}});
  calls[0].end(false); await tick();
  const at = Math.floor(reset / 1000) * 1000;
  expect(child.events().filter(e => e.type === 'usage_limit')).toEqual([{type:'usage_limit', resetsAt:at}]);
  expect(calls[1].text).toContain(`[report #${child.id} codex /tmp] Упёрся в лимит подписки`);
  const restored = new Sessions(db, {turns:{claude: async () => true, codex: async () => true}, checkModel:async () => {}, onEvent() {}, onChange() {}});
  restored.load('/home/u/.secretary');
  const schedules = restored.schedules.list([child.id]);
  expect(schedules).toHaveLength(1);
  expect(schedules[0].next_run).toBe(at + 60_000);
  expect(schedules[0].text).toBe('Лимит подписки сбросился, продолжи работу с того места, где остановился');
  sessions.schedules.tick(at + 60_000);
  expect(calls[2].text).toContain('Лимит подписки сбросился, продолжи работу с того места, где остановился');
  expect(sessions.schedules.list([child.id])[0].next_run).toBeNull();
});

test('лимит без времени назначает продолжение через 30 минут', async () => {
  const { calls, sessions, create } = setup();
  const child = await create();
  child.send('задача'); calls[0].start();
  calls[0].options.onEvent({type:'result', is_error:true, result:'Rate limit reached'});
  const before = Date.now(); calls[0].end(false); await tick();
  const schedule = sessions.schedules.list([child.id])[0];
  expect(schedule.next_run!).toBeGreaterThanOrEqual(before + 30 * 60_000);
  expect(schedule.next_run!).toBeLessThanOrEqual(Date.now() + 30 * 60_000);
});

test('при ошибке хода родитель получает один отчёт', async () => {
  const { calls, create } = setup();
  const parent = await create();
  const child = await create({ parentId: parent.id });
  child.send('задача'); calls[0].start(); calls[0].fail(); await tick();
  expect(calls).toHaveLength(2);
  expect(calls[1].text).toBe(`[report #${child.id} claude /tmp] Ход упал: spawn failed`);
});
