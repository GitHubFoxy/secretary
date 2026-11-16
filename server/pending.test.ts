import { expect, test } from 'bun:test';
import { chmodSync, mkdirSync, mkdtempSync, rmSync, writeFileSync } from 'node:fs';
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
      env: { ...process.env, PATH: `${bin}:${process.env.PATH}`, FAKE_LOG: join(dir, 'calls.log') },
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

test('сообщение, которое не запустилось, остаётся в очереди и доставляется после исправления', () => {
  expect(runScenario('spawn-failure-keeps-pending')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('ошибка хода не останавливает очередь', () => {
  expect(runScenario('turn-error-continues-queue')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);

test('остановка очищает очередь, в том числе в базе', () => {
  expect(runScenario('stop-clears-pending')).toEqual({ exitCode: 0, stderr: '' });
}, 30_000);
