// Сценарии для pending.test.ts. Запускаются отдельным процессом в каталоге с пустой базой (secretary.sqlite) и фейковыми claude/codex в PATH.
// Сессия 1 всегда служебная и не используется: у секретаря другие правила запуска.
import { expect } from 'bun:test';
import { existsSync, mkdirSync, readFileSync, writeFileSync } from 'node:fs';
import { join } from 'node:path';
import { Session, db } from '../session';

const dir = process.cwd();
const [scenario] = process.argv.slice(2);

async function until(check: () => boolean, label: string, timeout = 8000) {
  const start = Date.now();
  while (!check()) {
    if (Date.now() - start > timeout) throw new Error(`Таймаут: ${label}`);
    await Bun.sleep(20);
  }
}
const answerOf = (event: any): string | undefined => event.type === 'result' && !event.is_error ? event.result : event.params?.item?.text;
const answered = (s: Session, text: string) => s.events().some(event => answerOf(event) === `ANSWER ${text}`);
const userTexts = (s: Session) => s.events().filter(event => event.type === 'user').map(event => event.message.content);
const statuses = (s: Session) => s.events().filter(event => event.type === 'status').map(event => event.status);
const pendingCount = (id: number) => (db.query('SELECT COUNT(*) AS n FROM pending_messages WHERE session_id = ?').get(id) as { n: number }).n;
const calls = () => readFileSync(process.env.FAKE_LOG!, 'utf8').trim().split('\n').filter(Boolean).map(line => JSON.parse(line));
const started = (s: Session) => s.info.status === 'running' && s.events().some(event => event.type === 'user');

Session.create('opus', dir);
Session.interruptDeadlineMs = 5000;

const scenarios: Record<string, () => Promise<void>> = {
  async 'crash-interrupt-write'() {
    const s = Session.create('gpt', dir, 'codex');
    s.send('HANG опасное действие');
    await until(() => existsSync(process.env.FAKE_LOG!) && calls().some(call => call.received === 'HANG опасное действие'), 'Codex получил запрос');
    s.send('очередь после сбоя');
    s.interrupt();
    writeFileSync(join(dir, 'interrupt-requested'), '');
    process.kill(process.pid, 'SIGKILL');
    await new Promise(() => {});
  },
  async 'crash-interrupt-read'() {
    Session.cleanInterrupted();
    Session.recoverUnconfirmed();
    Session.recoverUnconfirmed();
    const s = Session.get(2)!;
    expect(s.events().filter(event => event.type === 'delivery_uncertain').map(event => event.text)).toEqual(['HANG опасное действие']);
    expect(s.queued().map(item => item.text)).toEqual(['очередь после сбоя']);
    s.resumePending();
    await until(() => answered(s, 'очередь после сбоя') && s.info.status === 'idle', 'очередь после сбоя обработана');
    expect(calls().filter(call => call.received === 'HANG опасное действие')).toHaveLength(1);
    expect(pendingCount(s.id)).toBe(0);
  },
  async 'interrupt-claude'() { await interruptScenario('claude'); },
  async 'interrupt-codex'() { await interruptScenario('codex'); },
  async 'interrupt-codex-before-start'() {
    Session.interruptDeadlineMs = 500;
    const s = Session.create('gpt', dir, 'codex');
    s.send('HANG старт');
    await until(() => existsSync(process.env.FAKE_LOG!) && calls().some(call => call.received === 'HANG старт'), 'Codex ожидает turn/start');
    s.send('после отмены');
    s.interrupt();
    await until(() => answered(s, 'после отмены') && s.info.status === 'idle', 'следующее сообщение выполнено');
    expect(calls().filter(call => call.received === 'HANG старт')).toHaveLength(1);
    expect(s.events().some(event => event.type === 'delivery_uncertain' && event.text === 'HANG старт')).toBe(true);
    expect(s.events().some(event => event.type === 'cancelled' && event.text === 'HANG старт')).toBe(false);
  },
  async 'interrupt-codex-delayed-ack'() {
    const s = Session.create('gpt', dir, 'codex');
    s.send('DELAYED начатый ход');
    await until(() => existsSync(process.env.FAKE_LOG!) && calls().some(call => call.received === 'DELAYED начатый ход'), 'Codex получил запрос');
    s.interrupt();
    await until(() => s.info.status === 'idle', 'Codex прерван после подтверждения');
    expect(userTexts(s)).toEqual(['DELAYED начатый ход']);
    expect(s.events().some(event => event.type === 'cancelled' || event.type === 'delivery_uncertain')).toBe(false);
    expect(pendingCount(s.id)).toBe(0);
  },
  async 'interrupt-codex-not-dispatched'() {
    const s = Session.create('gpt', dir, 'codex');
    s.send('отменён до отправки');
    await until(() => existsSync(`${process.env.FAKE_LOG}.thread`), 'Codex создаёт тред');
    s.interrupt();
    await until(() => s.info.status === 'idle', 'ход отменён');
    expect(existsSync(process.env.FAKE_LOG!)).toBe(false);
    expect(s.events().some(event => event.type === 'cancelled' && event.text === 'отменён до отправки')).toBe(true);
    expect(pendingCount(s.id)).toBe(0);
  },
  // Срочное сообщение прерывает долгий ход Claude, контекст сохраняется (--resume), обычное сообщение уходит после срочного.
  async 'claude-urgent'() {
    const s = Session.create('opus', dir, 'claude');
    s.send('LONG задача');
    await until(() => started(s), 'ход Claude начался');
    s.send('обычное');
    const sentAt = Date.now();
    s.send('срочно', undefined, true);
    await until(() => answered(s, 'обычное') && s.info.status === 'idle', 'ответы на срочное и обычное');
    expect(Date.now() - sentAt).toBeLessThan(4000);
    expect(userTexts(s)).toEqual(['LONG задача', 'срочно', 'обычное']);
    expect(answered(s, 'срочно')).toBe(true);
    // Прерванный ход не выводит статус: по одному idle на каждый завершённый ход (срочное и обычное), interrupted не появляется.
    expect(statuses(s)).toEqual(['idle', 'idle']);
    expect(calls().find(call => call.received === 'срочно')).toMatchObject({ resume: 'claude-session' });
    expect(pendingCount(s.id)).toBe(0);
  },

  // Два срочных сообщения во время прерывания упрямого хода (прерывание проигнорировано) уходят одним ходом после дедлайна.
  async 'claude-two-urgent-stubborn'() {
    Session.interruptDeadlineMs = 500;
    const s = Session.create('opus', dir, 'claude');
    s.send('STUBBORN долгий');
    await until(() => started(s), 'упрямый ход начался');
    const sentAt = Date.now();
    s.send('срочно 1', undefined, true);
    s.send('срочно 2', undefined, true);
    await until(() => answered(s, 'срочно 1\n\nсрочно 2') && s.info.status === 'idle', 'ответ на два срочных сообщения');
    expect(Date.now() - sentAt).toBeLessThan(4000);
    expect(userTexts(s)).toEqual(['STUBBORN долгий', 'срочно 1', 'срочно 2']);
    expect(calls().filter(call => call.received?.startsWith('срочно'))).toEqual([{ harness: 'claude', received: 'срочно 1\n\nсрочно 2', resume: 'claude-session' }]);
    expect(s.events().some(event => event.type === 'error')).toBe(false);
  },

  // Codex получил turn/start, но не ответил: срочное сообщение выполняется, а исходное не повторяется.
  async 'codex-hang-before-turn-start'() {
    Session.interruptDeadlineMs = 500;
    const s = Session.create('gpt', dir, 'codex');
    s.send('HANG старт');
    await until(() => existsSync(process.env.FAKE_LOG!) && calls().some(call => call.received === 'HANG старт'), 'Codex получил turn/start');
    const sentAt = Date.now();
    s.send('срочно', undefined, true);
    await until(() => answered(s, 'срочно') && s.info.status === 'idle', 'ответ Codex на срочное сообщение');
    expect(Date.now() - sentAt).toBeLessThan(4000);
    expect(userTexts(s)).toEqual(['срочно']);
    expect(s.events().some(event => event.type === 'delivery_uncertain' && event.text === 'HANG старт')).toBe(true);
    expect(calls().filter(call => call.received === 'HANG старт')).toHaveLength(1);
    expect(s.events().some(event => event.type === 'error')).toBe(false);
    expect(statuses(s)).toEqual(['idle']);
    expect(pendingCount(s.id)).toBe(0);
  },

  // Сообщение не запускается (нет папки): оно остаётся в очереди и в базе, в истории его нет, ошибка видна.
  // После появления папки очередь запускается повторно и сообщение доходит до агента один раз.
  async 'spawn-failure-keeps-pending'() {
    const folder = join(dir, 'появится-позже');
    const s = Session.create('opus', folder, 'claude');
    s.send('не запускается');
    await until(() => s.info.status === 'interrupted', 'ход не запустился');
    expect(userTexts(s)).toEqual([]);
    expect(s.queued().map(item => item.text)).toEqual(['не запускается']);
    expect(pendingCount(s.id)).toBe(1);
    expect(s.events().some(event => event.type === 'error')).toBe(true);
    // Путь папки тот же, поэтому тот же экземпляр запускает очередь после появления папки.
    mkdirSync(folder);
    s.resumePending();
    await until(() => answered(s, 'не запускается') && s.info.status === 'idle', 'сообщение доставлено после появления папки');
    expect(userTexts(s)).toEqual(['не запускается']);
    expect(pendingCount(s.id)).toBe(0);
    expect(s.queued()).toEqual([]);
  },

  // Ход завершился ошибкой после приёма: следующее сообщение из очереди всё равно выполняется.
  async 'turn-error-continues-queue'() {
    const s = Session.create('opus', dir, 'claude');
    s.send('FAIL первый');
    s.send('после ошибки');
    await until(() => answered(s, 'после ошибки') && s.info.status === 'idle', 'очередь продолжилась после ошибки');
    expect(userTexts(s)).toEqual(['FAIL первый', 'после ошибки']);
    expect(pendingCount(s.id)).toBe(0);
  },

  // Codex отвечает на turn/interrupt статусом interrupted: срочное сообщение идёт в тот же тред.
  async 'codex-urgent-interrupts-long'() {
    const s = Session.create('gpt', dir, 'codex');
    s.send('LONG codex');
    await until(() => s.events().some(event => event.method === 'turn/started'), 'ход Codex начался');
    s.send('срочно', undefined, true);
    await until(() => answered(s, 'срочно') && s.info.status === 'idle', 'ответ Codex на срочное сообщение');
    expect(userTexts(s)).toEqual(['LONG codex', 'срочно']);
    expect(statuses(s)).toEqual(['idle']);
    expect(calls().filter(call => call.harness === 'codex').map(call => call.thread)).toEqual(['codex-thread', 'codex-thread']);
  },

  // Часть 1 перезапуска: сообщения подтверждены (записаны в базу), и программа падает, не дождавшись хода.
  async 'restart-write'() {
    const s = Session.create('opus', dir, 'claude');
    s.send('LONG задача');
    await until(() => started(s), 'ход начался');
    s.send('очередь 1');
    s.send('очередь 2');
    expect(pendingCount(s.id)).toBe(2);
    process.exit(0);
  },

  // Часть 2 перезапуска: очередь восстанавливается из базы и уходит в ход без повторного ввода.
  async 'restart-read'() {
    const s = Session.get(2)!;
    expect(pendingCount(s.id)).toBe(2);
    Session.cleanInterrupted();
    db.query("UPDATE sessions SET status = 'interrupted' WHERE status = 'running'").run();
    s.resumePending();
    await until(() => answered(s, 'очередь 1') && answered(s, 'очередь 2') && s.info.status === 'idle', 'восстановленная очередь выполнена');
    expect(userTexts(s)).toEqual(['LONG задача', 'очередь 1', 'очередь 2']);
    expect(pendingCount(s.id)).toBe(0);
  },

  // Остановка очищает очередь и в базе: после перезапуска остановленные сообщения не появятся.
  // Но в истории они остаются как «Не отправлено», а очередь рассылается слушателям, пока сообщение ждёт.
  async 'stop-clears-pending'() {
    const s = Session.create('opus', dir, 'claude');
    const seen: any[] = [];
    s.onEvent(event => seen.push(event));
    s.send('LONG задача');
    await until(() => started(s), 'ход начался');
    s.send('не отправлять');
    expect(pendingCount(s.id)).toBe(1);
    expect(seen.some(event => event.type === 'queue' && event.items.map((item: any) => item.text).join() === 'не отправлять')).toBe(true);
    expect(s.events().some(event => event.type === 'queue')).toBe(false);
    s.stop();
    await until(() => s.info.status === 'stopped', 'статус stopped');
    expect(pendingCount(s.id)).toBe(0);
    expect(s.queued()).toEqual([]);
    expect(userTexts(s)).toEqual(['LONG задача']);
    expect(s.events().filter(event => event.type === 'cancelled').map(event => event.text)).toEqual(['не отправлять']);
    expect(seen.some(event => event.type === 'cancelled' && event.text === 'не отправлять')).toBe(true);
  },
};

async function interruptScenario(harness: string) {
  const s = Session.create(harness === 'claude' ? 'opus' : 'gpt', dir, harness);
  s.send('LONG задача');
  await until(() => started(s), 'ход начался');
  s.send('очередь');
  s.interrupt();
  await until(() => answered(s, 'очередь') && s.info.status === 'idle', 'очередь продолжилась');
  s.send('после остановки');
  await until(() => answered(s, 'после остановки') && s.info.status === 'idle', 'следующий ход завершился');
  expect(userTexts(s)).toEqual(['LONG задача', 'очередь', 'после остановки']);
  const call = calls().find(call => call.received === 'после остановки');
  expect(harness === 'claude' ? call.resume : call.thread).toBe(harness === 'claude' ? 'claude-session' : 'codex-thread');
  s.send('LONG снова');
  await until(() => s.queued().length === 0 && s.info.status === 'running', 'второй долгий ход начался');
  s.interrupt();
  await until(() => s.info.status === 'idle', 'остановка без очереди');
  s.send('продолжить');
  await until(() => answered(s, 'продолжить') && s.info.status === 'idle', 'продолжение после остановки');
}

const run = scenarios[scenario];
if (!run) throw new Error(`Неизвестный сценарий: ${scenario}`);
await run();
process.exit(0);
