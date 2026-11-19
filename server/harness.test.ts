// The adapter contract (harness.ts), checked against fake `claude` and `codex` executables.
import { afterAll, expect, test } from 'bun:test';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { claudeTurn } from './claude';
import { codexTurn } from './codex';
import { harnessTiming, type Turn } from './harness';

const root = mkdtempSync(join(tmpdir(), 'secretary-harness-'));
const bin = join(root, 'bin');
mkdirSync(bin);
for (const name of ['claude', 'codex']) {
  writeFileSync(join(bin, name), `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(join(import.meta.dir, 'test-fixtures', `fake-${name}.ts`))} "$@"\n`);
  chmodSync(join(bin, name), 0o755);
}
process.env.PATH = `${bin}:${process.env.PATH}`;
harnessTiming.interruptDeadlineMs = 300;
afterAll(() => rmSync(root, { recursive: true, force: true }));

let folders = 0;
function run(turn: Turn, text: string, { resumeId = null as string | null, folder = 'f' } = {}) {
  const dir = join(root, `${folder}-${++folders}`);
  mkdirSync(dir);
  const interrupt = new AbortController(), stop = new AbortController();
  const log: string[] = [];
  const result = turn({
    folder: dir, model: 'm', reasoning: null, resumeId, secretary: false, interrupt: interrupt.signal, stop: stop.signal,
    onSessionId: id => log.push(`session ${id}`),
    onEvent: event => { const answer = event.result || event.params?.item?.text; if (answer) log.push(answer); },
    onDispatching: () => log.push('dispatching'),
    onAccepted: () => log.push('accepted'),
  }, text, []);
  const calls = () => existsSync(join(dir, 'calls.log')) ? readFileSync(join(dir, 'calls.log'), 'utf8').trim().split('\n').map(line => JSON.parse(line)) : [];
  return { result, log, calls, interrupt, stop };
}
const until = async (check: () => boolean) => { for (let i = 0; !check(); i++) { if (i > 400) throw new Error('timeout'); await Bun.sleep(10); } };

for (const [name, turn, resumeKey] of [['claude', claudeTurn, 'resume'], ['codex', codexTurn, 'thread']] as const) {
  test(`${name}: ход отдаёт ответ, принимает сообщение один раз и продолжает сессию`, async () => {
    const t = run(turn, 'привет', { resumeId: `${name}-old` });
    expect(await t.result).toBe(true);
    expect(t.log.filter(entry => !entry.startsWith('session'))).toEqual(['dispatching', 'accepted', 'ANSWER привет']);
    expect(t.calls()[0][resumeKey]).toBe(name === 'claude' ? 'claude-old' : 'codex-thread');
  });

  test(`${name}: прерывание завершает долгий ход`, async () => {
    const t = run(turn, 'LONG');
    await until(() => t.log.includes('accepted'));
    t.interrupt.abort();
    expect(await t.result).toBe(false);
  });
}

test('claude: ход, который игнорирует прерывание, убивается после дедлайна', async () => {
  const t = run(claudeTurn, 'STUBBORN');
  await until(() => t.log.includes('accepted'));
  t.interrupt.abort();
  expect(await t.result).toBe(false);
});

test('codex: зависший turn/start убивается после дедлайна, сообщение отправлено, но не принято', async () => {
  const t = run(codexTurn, 'HANG');
  await until(() => t.calls().length > 0);
  t.interrupt.abort();
  await expect(t.result).rejects.toThrow();
  expect(t.log).not.toContain('accepted');
  expect(t.log).toContain('dispatching');
});

test('codex: прерывание до turn/start отменяет отправку', async () => {
  const t = run(codexTurn, 'не отправлять', { folder: 'slow-thread' });
  await Bun.sleep(50);
  t.interrupt.abort();
  expect(await t.result).toBe(false);
  expect(t.log).not.toContain('dispatching');
  expect(t.calls()).toEqual([]);
});
