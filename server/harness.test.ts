import { afterAll, expect, test } from 'bun:test';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, readFileSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { claudeTurn } from './claude';
import { codexTurn } from './codex';
import { opencodeTurn } from './opencode';
import { slashCommands } from './commands';
import { harnessTiming, type Turn } from './harness';
import { ensurePersona, SECRETARY_PROMPT } from './persona';

const root = mkdtempSync(join(tmpdir(), 'secretary-harness-'));
const bin = join(root, 'bin');
mkdirSync(bin);
for (const name of ['claude', 'codex', 'opencode']) {
  writeFileSync(join(bin, name), `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(join(import.meta.dir, 'test-fixtures', `fake-${name}.ts`))} "$@"\n`);
  chmodSync(join(bin, name), 0o755);
}
process.env.PATH = `${bin}:${process.env.PATH}`;
harnessTiming.interruptDeadlineMs = 300;
afterAll(() => rmSync(root, { recursive: true, force: true }));

let folders = 0;
function run(turn: Turn, text: string, { resumeId = null as string | null, folder = 'f', persona = false, compact = false } = {}) {
  const dir = join(root, `${folder}-${++folders}`);
  mkdirSync(dir);
  if (persona) {
    writeFileSync(join(dir, 'AGENTS.md'), '# Старые правила\n\n## Предпочтения владельца\n\nЛюблю чай.\n\n## Системный контракт (лучше не менять)\n\nСтарый контракт.\n');
    ensurePersona(dir);
  }
  const interrupt = new AbortController(), stop = new AbortController();
  const log: string[] = [];
  const result = turn({
    folder: dir, model: 'm', reasoning: null, resumeId, compact, secretary: persona, systemPrompt: persona ? SECRETARY_PROMPT : undefined, interrupt: interrupt.signal, stop: stop.signal,
    onSessionId: id => log.push(`session ${id}`),
    onEvent: event => { const answer = event.result || event.params?.item?.text || event.text; if (answer) log.push(answer); },
    onDispatching: () => log.push('dispatching'),
    onAccepted: () => log.push('accepted'),
  }, text, []);
  const calls = () => existsSync(join(dir, 'calls.log')) ? readFileSync(join(dir, 'calls.log'), 'utf8').trim().split('\n').map(line => JSON.parse(line)) : [];
  return { result, log, calls, interrupt, stop, dir };
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

test('claude: упавший harness отдаёт владельцу код выхода и конец stderr', async () => {
  await expect(run(claudeTurn, 'CRASH').result).rejects.toThrow('Claude Code завершился с кодом 1: Invalid API key · Please run /login');
});

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

test('codex без входа показывает команду для авторизации до отправки', async () => {
  const t = run(codexTurn, 'привет', { folder: 'no-auth' });
  await expect(t.result).rejects.toThrow('codex login --device-auth');
  expect(t.log).not.toContain('dispatching');
  expect(t.calls()).toEqual([]);
});

for (const [name, turn] of [['claude', claudeTurn], ['codex', codexTurn]] as const) {
  test(`${name}: существующая установка получает новые правила и сохраняет предпочтения`, async () => {
    const t = run(turn, 'привет', { persona: true, resumeId: 'existing-session' });
    expect(await t.result).toBe(true);
    expect(t.calls()[0].prompt).toContain('Говори как живой человек');
    expect(readFileSync(join(t.dir, 'AGENTS.md'), 'utf8')).toBe('## Предпочтения владельца\n\nЛюблю чай.\n\n');
    expect(readFileSync(join(t.dir, 'AGENTS.md.bak'), 'utf8')).toContain('Старый контракт.');
    expect(readFileSync(join(t.dir, 'CLAUDE.md'), 'utf8')).toBe('@AGENTS.md\n');
    if (name === 'codex') {
      expect(t.calls()[0].prompt).toContain('Люблю чай.');
      expect(t.calls()[0].model).toBe('m');
    }
  });
}

test('миграция старого файла по умолчанию сохраняет пустые предпочтения', () => {
  const dir = join(root, 'old-default');
  mkdirSync(dir);
  const old = '# Secretary Agent\n\n## Предпочтения владельца\n\n*(Пока пусто — заполняется по просьбе владельца.)*\n\n## Системный контракт (лучше не менять)\n\nКонтракт.\n';
  writeFileSync(join(dir, 'AGENTS.md'), old);
  ensurePersona(dir);
  ensurePersona(dir);
  expect(readFileSync(join(dir, 'AGENTS.md'), 'utf8')).toBe('## Предпочтения владельца\n\n*(Пока пусто — заполняется по просьбе владельца.)*\n\n');
  expect(readFileSync(join(dir, 'AGENTS.md.bak'), 'utf8')).toBe(old);
});

test('opencode: ход отдаёт ответ, принимает сообщение один раз и продолжает сессию', async () => {
  const t = run(opencodeTurn, 'привет', { resumeId: 'opencode-old' });
  expect(await t.result).toBe(true);
  expect(t.log.filter(entry => !entry.startsWith('session'))).toEqual(['dispatching', 'accepted', 'ANSWER привет']);
  expect(t.calls().filter(call => call.text).map(call => call.session)).toEqual(['opencode-old']);
});

test('opencode: прерывание завершает долгий ход', async () => {
  const t = run(opencodeTurn, 'LONG');
  await until(() => t.log.includes('accepted'));
  t.interrupt.abort();
  expect(await t.result).toBe(false);
});

test('opencode: зависший session/prompt убивается после дедлайна, сообщение отправлено, но не принято', async () => {
  const t = run(opencodeTurn, 'HANG');
  await until(() => t.calls().some(call => call.text));
  t.interrupt.abort();
  await expect(t.result).rejects.toThrow();
  expect(t.log).not.toContain('accepted');
  expect(t.log).toContain('dispatching');
});

test('opencode: прерывание до session/prompt отменяет отправку', async () => {
  const t = run(opencodeTurn, 'не отправлять', { folder: 'slow-resume' });
  await Bun.sleep(50);
  t.interrupt.abort();
  expect(await t.result).toBe(false);
  expect(t.log).not.toContain('dispatching');
  expect(t.calls().filter(call => call.text)).toEqual([]);
});

test('opencode без входа показывает команду для авторизации', async () => {
  const t = run(opencodeTurn, 'привет', { folder: 'no-auth' });
  await expect(t.result).rejects.toThrow('opencode auth login');
  expect(t.log).not.toContain('accepted');
});

test('opencode: упавший harness отдаёт владельцу код выхода и конец stderr', async () => {
  await expect(run(opencodeTurn, 'CRASH').result).rejects.toThrow('OpenCode завершился с кодом 1: connection refused');
});

test('opencode: compact принимается и с обновлением сжатия, и без него', async () => {
  for (const folder of ['f', 'quiet-compact']) {
    const t = run(opencodeTurn, '/compact', { folder, resumeId: 'opencode-old', compact: true });
    expect(await t.result).toBe(true);
    expect(t.log).toContain('dispatching');
    expect(t.log).toContain('accepted');
    expect(t.calls().filter(call => call.text)).toEqual([{ text: '/compact', session: 'opencode-old' }]);
  }
});

test('opencode: загрузка команд удаляет пустую сессию листинга', async () => {
  const dir = join(root, `commands-${++folders}`);
  mkdirSync(dir);
  const commands = await slashCommands('opencode', dir);
  expect(commands).toEqual([{ name: 'review', description: 'review changes' }]);
  const calls = readFileSync(join(dir, 'calls.log'), 'utf8').trim().split('\n').map(line => JSON.parse(line));
  expect(calls).toContainEqual({ delete: 'opencode-session' });
});

test('opencode: системный промпт едет файлом в OPENCODE_CONFIG_CONTENT поверх настроек владельца', async () => {
  const previous = process.env.OPENCODE_CONFIG_CONTENT;
  process.env.OPENCODE_CONFIG_CONTENT = JSON.stringify({ model: 'pag/custom', permissions: [] });
  try {
    const t = run(opencodeTurn, 'привет', { persona: true, resumeId: 'existing-session' });
    expect(await t.result).toBe(true);
    const config = JSON.parse(t.calls().find(call => call.setup)?.config);
    expect(config.model).toBe('pag/custom');
    expect(config.permissions).toEqual([{ action: '*', resource: '*', effect: 'allow' }]);
    expect(config.instructions).toHaveLength(1);
    expect(readFileSync(config.instructions[0], 'utf8')).toBe(SECRETARY_PROMPT);
  } finally {
    if (previous === undefined) delete process.env.OPENCODE_CONFIG_CONTENT;
    else process.env.OPENCODE_CONFIG_CONTENT = previous;
  }
});

test('спавн: setsid идёт абсолютным путём, пропавшая папка даёт маркер для понятной ошибки', async () => {
  const { spawnHarness } = await import('./harness');
  const { friendlyErrorMessage } = await import('./chat');
  const interrupt = new AbortController(), stop = new AbortController();
  expect(() => spawnHarness(['echo', 'hi'], join(root, 'нет-такой-папки'), { interrupt: interrupt.signal, stop: stop.signal }))
    .toThrow('SESSION_FOLDER_MISSING');
  try {
    spawnHarness(['echo', 'hi'], join(root, 'нет-такой-папки'), { interrupt: interrupt.signal, stop: stop.signal });
  } catch (error) {
    expect(friendlyErrorMessage(error)).toBe('Не удалось запустить агента: нет папки Session или программы на Host. Проверьте папку и установку, затем повторите.');
  }
  const dir = join(root, 'spawn-ok');
  mkdirSync(dir, { recursive: true });
  const harness = spawnHarness(['echo', 'hi'], dir, { interrupt: interrupt.signal, stop: stop.signal }, {});
  await harness.close();
});
