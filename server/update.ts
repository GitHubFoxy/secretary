import { Database } from 'bun:sqlite';
import { copyFileSync, existsSync, mkdirSync, readdirSync, readlinkSync, renameSync, rmSync, symlinkSync } from 'node:fs';
import { homedir } from 'node:os';
import { join } from 'node:path';

const folder = join(homedir(), '.secretary');
const current = join(folder, 'current');
const previous = readlinkSync(current);
const staging = join(folder, `update-${crypto.randomUUID()}`);
const release = join(folder, 'releases', crypto.randomUUID());
const backup = join(staging, 'backup');
let stopped = false;
let switched = false;
let committed = false;
let preserveBackup = false;
const run = (...args: string[]) => {
  const result = Bun.spawnSync(args, { stdout: 'inherit', stderr: 'inherit' });
  if (result.exitCode !== 0) throw new Error(`${args[0]}: ${result.exitCode}`);
};
const switchTo = (path: string) => {
  const link = join(folder, `.current-${crypto.randomUUID()}`);
  symlinkSync(path, link);
  renameSync(link, current);
};
try {
  mkdirSync(staging, { recursive: true, mode: 0o700 });
  const base = 'https://github.com/GitHubFoxy/secretary/releases/latest/download';
  run('curl', '--progress-bar', '-fL', `${base}/secretary.tar.gz`, '-o', join(staging, 'secretary.tar.gz'));
  run('curl', '--progress-bar', '-fL', `${base}/secretary.tar.gz.sha256`, '-o', join(staging, 'secretary.tar.gz.sha256'));
  const checksum = (await Bun.file(join(staging, 'secretary.tar.gz.sha256')).text()).trim().split(/\s+/)[0];
  const hash = new Bun.CryptoHasher('sha256').update(await Bun.file(join(staging, 'secretary.tar.gz')).arrayBuffer()).digest('hex');
  if (!/^[a-f0-9]{64}$/.test(checksum) || checksum !== hash) throw new Error('Контрольная сумма релиза не совпадает');
  mkdirSync(release, { recursive: true });
  run('tar', '-xzf', join(staging, 'secretary.tar.gz'), '-C', release);
  for (const file of ['server/index.ts', 'web/dist/index.html', 'secretary', 'server/update.ts'])
    if (!existsSync(join(release, file))) throw new Error(`В релизе нет ${file}`);
  run('systemctl', '--user', 'stop', 'secretary.service');
  stopped = true;
  mkdirSync(backup);
  for (const file of readdirSync(folder).filter(file => file.endsWith('.sqlite'))) {
    const db = new Database(join(folder, file), { readonly: true });
    try { db.query('VACUUM INTO ?').run(join(backup, file)); } finally { db.close(); }
  }
  switchTo(release);
  switched = true;
  run('systemctl', '--user', 'start', 'secretary.service');
  run('curl', '--retry', '10', '--retry-delay', '1', '--retry-connrefused', '--fail', '--silent', '--show-error', 'http://127.0.0.1:3000/');
  committed = true;
  rmSync(join(folder, 'backup'), { recursive: true, force: true });
  renameSync(backup, join(folder, 'backup'));
  for (const name of readdirSync(join(folder, 'releases'))) {
    const path = join(folder, 'releases', name);
    if (path !== previous && path !== release) rmSync(path, { recursive: true, force: true });
  }
  console.log('\nОбновление завершено.');
} catch (error) {
  console.error('Ошибка обновления:', error);
  if (stopped && !committed) {
    try {
      if (switched) {
        run('systemctl', '--user', 'stop', 'secretary.service');
        switchTo(previous);
        for (const file of readdirSync(backup)) {
          for (const suffix of ['', '-wal', '-shm']) rmSync(join(folder, file + suffix), { force: true });
          copyFileSync(join(backup, file), join(folder, file));
        }
      }
      run('systemctl', '--user', 'start', 'secretary.service');
      console.error('Предыдущая версия восстановлена.');
    } catch (rollbackError) { preserveBackup = true; console.error('Ошибка отката:', rollbackError, 'Копия баз:', backup); }
  }
  if (!committed && !preserveBackup) rmSync(release, { recursive: true, force: true });
  process.exitCode = 1;
} finally {
  if (!preserveBackup) rmSync(staging, { recursive: true, force: true });
}
