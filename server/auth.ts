import { Database } from 'bun:sqlite';
import { randomBytes, randomInt, createHash } from 'node:crypto';
import { mkdirSync, chmodSync } from 'node:fs';
import { homedir } from 'node:os';

const folder = `${homedir()}/.secretary`;
mkdirSync(folder, { recursive: true, mode: 0o700 });
const db = new Database(`${folder}/auth.sqlite`, { create: true });
chmodSync(`${folder}/auth.sqlite`, 0o600);
db.run('CREATE TABLE IF NOT EXISTS pairing (code TEXT NOT NULL, expires INTEGER NOT NULL)');
db.run('CREATE TABLE IF NOT EXISTS devices (token TEXT PRIMARY KEY)');
db.run('CREATE TABLE IF NOT EXISTS pairing_attempts (count INTEGER NOT NULL)');
const hash = (value: string) => createHash('sha256').update(value).digest('hex');

export function createCode() {
  const alphabet = 'ABCDEFGHJKLMNPQRSTUVWXYZ23456789';
  const code = Array.from({length: 8}, () => alphabet[randomInt(alphabet.length)]).join('');
  db.transaction(() => {
    db.run('DELETE FROM pairing');
    db.run('DELETE FROM pairing_attempts');
    db.run('INSERT INTO pairing_attempts VALUES (0)');
    db.run('INSERT INTO pairing VALUES (?, ?)', [hash(code), Date.now() + 10 * 60_000]);
  })();
  return `${code.slice(0, 4)}-${code.slice(4)}`;
}
export function pair(input: string) {
  const code = input.toUpperCase().replaceAll('-', '').trim();
  return db.transaction(() => {
    const attempts = db.query('SELECT count FROM pairing_attempts').get() as {count:number} | null;
    if (!attempts || attempts.count >= 10) return null;
    db.run('UPDATE pairing_attempts SET count = count + 1');
    if (!/^[A-Z2-9]{8}$/.test(code)) return null;
    const result = db.query('DELETE FROM pairing WHERE code = ? AND expires > ?').run(hash(code), Date.now());
    if (!result.changes) return null;
    const token = randomBytes(32).toString('hex');
    db.run('INSERT INTO devices VALUES (?)', [hash(token)]);
    return token;
  })();
}
export function authorized(request: Request) {
  const token = request.headers.get('cookie')?.match(/(?:^|;\s*)secretary=([a-f0-9]{64})(?:;|$)/)?.[1];
  return !!token && !!db.query('SELECT 1 FROM devices WHERE token = ?').get(hash(token));
}
if (import.meta.main) console.log(`Код сопряжения (действует 10 минут): ${createCode()}`);
