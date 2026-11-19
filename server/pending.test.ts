import { expect, test } from 'bun:test';
import { chmodSync, existsSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

const fixtures = join(import.meta.dir, 'test-fixtures');

// Фейковые claude и codex лежат в PATH временного каталога. Сценарий работает с собственной базой в этом же каталоге.
function runScenario(name: string) {
  const dir = mkdtempSync(join(tmpdir(), 'secretary-pending-'));
  try {
    const bin = join(dir, 'bin');
    mkdirSync(bin);
    for (const [command, script] of [['claude', 'fake-claude.ts'], ['codex', 'fake-codex.ts']]) {
      const file = join(bin, command);
      writeFileSync(file, `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(join(fixtures, script))} "$@"\n`);
      chmodSync(file, 0o755);
    }
    const result = Bun.spawnSync([process.execPath, 'run', join(fixtures, 'pending-scenario.ts'), name], {
      cwd: dir,
      env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, FAKE_LOG: join(dir, 'calls.log'), ...(['interrupt-codex-not-dispatched', 'running-before-adapter-event'].includes(name) ? {FAKE_DELAY_THREAD:'1'} : {}) },
      stdout: 'pipe',
      stderr: 'pipe',
      timeout: 60_000,
    });
    return { exitCode: result.exitCode, stderr: result.stderr.toString() };
  } finally {
    rmSync(dir, { recursive: true, force: true });
  }
}

test('срочное сообщение прерывает долгий ход Claude, сохраняет контекст и идёт раньше обычного', () => {
  expect(runScenario('claude-urgent')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('два срочных сообщения не ждут упрямый ход дольше дедлайна и уходят одним ходом', () => {
  expect(runScenario('claude-two-urgent-stubborn')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('зависание Codex до turn/start не блокирует срочное сообщение', () => {
  expect(runScenario('codex-hang-before-turn-start')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('срочное сообщение прерывает долгий ход Codex и продолжает тот же тред', () => {
  expect(runScenario('codex-urgent-interrupts-long')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('подтверждённая очередь переживает перезапуск и выполняется', () => {
  // Сценарии перезапуска делят одну базу, поэтому запускаются в одном каталоге.
  const shared = mkdtempSync(join(tmpdir(), 'secretary-restart-'));
  try {
    const bin = join(shared, 'bin');
    mkdirSync(bin);
    for (const [command, script] of [['claude', 'fake-claude.ts'], ['codex', 'fake-codex.ts']]) {
      const file = join(bin, command);
      writeFileSync(file, `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(join(fixtures, script))} "$@"\n`);
      chmodSync(file, 0o755);
    }
    const env = { ...process.env, PATH: `${bin}:${process.env.PATH}`, FAKE_LOG: join(shared, 'calls.log') };
    const run = (name: string) => Bun.spawnSync([process.execPath, 'run', join(fixtures, 'pending-scenario.ts'), name], { cwd: shared, env, stdout: 'pipe', stderr: 'pipe', timeout: 60_000 });
    const written = run('restart-write');
    expect({ exitCode: written.exitCode, stderr: written.stderr.toString() }).toEqual({ exitCode: 0, stderr: '' });
    const restored = run('restart-read');
    expect({ exitCode: restored.exitCode, stderr: restored.stderr.toString() }).toEqual({ exitCode: 0, stderr: '' });
  } finally {
    rmSync(shared, { recursive: true, force: true });
  }
}, 60_000);

test('после падения при прерывании не повторяет неподтверждённый запрос', () => {
  const shared = mkdtempSync(join(tmpdir(), 'secretary-interrupt-crash-'));
  try {
    const bin = join(shared, 'bin');
    mkdirSync(bin);
    const file = join(bin, 'codex');
    writeFileSync(file, `#!/bin/sh\nexec ${JSON.stringify(process.execPath)} ${JSON.stringify(join(fixtures, 'fake-codex.ts'))} "$@"\n`);
    chmodSync(file, 0o755);
    const env = { ...process.env, PATH: `${bin}:${process.env.PATH}`, FAKE_LOG: join(shared, 'calls.log') };
    const run = (name: string) => Bun.spawnSync([process.execPath, 'run', join(fixtures, 'pending-scenario.ts'), name], {cwd:shared, env, stdout:'pipe', stderr:'pipe', timeout:60_000});
    const crashed = run('crash-interrupt-write');
    expect(crashed.exitCode).not.toBe(0);
    expect(existsSync(join(shared, 'interrupt-requested'))).toBe(true);
    const restored = run('crash-interrupt-read');
    expect({exitCode:restored.exitCode, stderr:restored.stderr.toString()}).toEqual({exitCode:0, stderr:''});
  } finally { rmSync(shared, {recursive:true, force:true}); }
}, 60_000);

test('сообщение, которое не запустилось, остаётся в очереди и доставляется после исправления', () => {
  expect(runScenario('spawn-failure-keeps-pending')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('ошибка хода не останавливает очередь', () => {
  expect(runScenario('turn-error-continues-queue')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('после выхода адаптера без подтверждения очередь продолжает работу без повтора неопределённого сообщения', () => {
  expect(runScenario('unconfirmed-exit-continues-queue')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('остановка очищает очередь, в том числе в базе', () => {
  expect(runScenario('stop-clears-pending')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

for (const harness of ['claude', 'codex']) {
  test(`прерывание хода ${harness} сохраняет очередь и позволяет продолжить сессию`, () => {
    expect(runScenario(`interrupt-${harness}`)).toEqual({ exitCode: 0, stderr: '' });
  }, 30_000);
}

test('неподтверждённый запрос при прерывании помечается как неопределённый и не повторяется', () => {
  expect(runScenario('interrupt-codex-before-start')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('задержанное подтверждение после прерывания сохраняет принятое сообщение', () => {
  expect(runScenario('interrupt-codex-delayed-ack')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('до отправки turn/start сообщение действительно отменяется', () => {
  expect(runScenario('interrupt-codex-not-dispatched')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('статус running публикуется до первого события зависшего адаптера', () => {
  expect(runScenario('running-before-adapter-event')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('запрос прерывания подтверждается сразу, результат приходит после завершения хода', () => {
  expect(runScenario('interrupt-feedback')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('естественное завершение хода не выдаётся за успешную отмену', () => {
  expect(runScenario('interrupt-feedback-completed')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

