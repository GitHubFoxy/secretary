// Session, MessageQueue and Sessions in process, with a scripted harness in place of Claude Code and Codex.
import { expect, test } from 'bun:test';
import type { Database } from 'bun:sqlite';
import { reportText } from './chat';
import { openDatabase } from './database';
import type { Turn, TurnOptions } from './harness';
import type { Session } from './session';
import { Sessions } from './sessions';

type Call = { text: string; options: TurnOptions; start(): void; answer(text: string): void; end(completed: boolean): void; fail(): void };

function setup(db: Database = openDatabase(':memory:')) {
  const calls: Call[] = [];
  const published: any[] = [];
  const turn: Turn = (options, text) => new Promise((resolve, reject) => calls.push({
    text, options,
    start() { options.onDispatching(); options.onAccepted(); },
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
  expect(calls[1].text).toBe(reportText(child.info, 'готово'));
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
  expect(calls[0].options.mcpUrl).toMatch(/\/mcp\/2$/);
  calls[0].start(); calls[0].answer('NO_REPLY'); await tick();
  // Secretary Agent should NOT receive a report for NO_REPLY.
  expect(calls).toHaveLength(1);

  child.send('задача');
  expect(calls[1].options.mcpUrl).toBeUndefined();
  expect(calls[1].options.systemPrompt).toBeUndefined();
  calls[1].start();

  sessions.stop(specialist.id);
  expect(calls[1].options.stop.aborted).toBe(true);
});
