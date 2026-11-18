import { existsSync, readFileSync } from 'node:fs';

// Если процесс нельзя проверить, сохраняем его идентификатор и не запускаем следующий ход.
export function processState(
  identity: string,
  readStat: (path: string) => string = path => readFileSync(path, 'utf8'),
  procAvailable: () => boolean = () => existsSync('/proc/self/stat'),
  probe: (pid: number) => unknown = pid => process.kill(pid, 0),
): 'matching' | 'gone' | 'unknown' {
  if (process.platform !== 'linux' || !/^\d+:\d+$/.test(identity)) return 'unknown';
  const [pid, start] = identity.split(':');
  const number = Number(pid);
  if (!Number.isSafeInteger(number) || number <= 0) return 'unknown';
  try {
    const stat = readStat(`/proc/${pid}/stat`);
    const end = stat.lastIndexOf(')');
    if (end < 0) return 'unknown';
    const fields = stat.slice(end + 2).split(' ');
    if (!fields[0] || !/^\d+$/.test(fields[19] || '')) return 'unknown';
    return fields[0] !== 'Z' && fields[19] === start ? 'matching' : 'gone';
  } catch (error) {
    if ((error as NodeJS.ErrnoException).code !== 'ENOENT') return 'unknown';
    try { if (!procAvailable()) return 'unknown'; } catch { return 'unknown'; }
    try { probe(number); return 'unknown'; }
    catch (probeError) { return (probeError as NodeJS.ErrnoException).code === 'ESRCH' ? 'gone' : 'unknown'; }
  }
}

export function processAlive(...args: Parameters<typeof processState>) {
  return processState(...args) !== 'gone';
}
