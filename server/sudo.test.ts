import { expect, test } from 'bun:test';
import { Sudo } from './sudo';

test('подтверждение выполняет команду один раз и не сохраняет пароль в событиях', async () => {
  const events: any[] = [];
  const calls: any[] = [];
  const sudo = new Sudo((id, event) => events.push({ id, event }), async (...args) => { calls.push(args); return { exitCode: 0, output: 'ok secret' }; });
  const result = sudo.request(1, 'id', 'проверка', '/tmp');
  const id = events[0].event.id;
  await sudo.approve(id, 'secret');
  expect(await result).toEqual({ status: 'completed', exitCode: 0, output: 'ok [скрыто]' });
  expect(calls).toEqual([['id', 'secret', '/tmp']]);
  expect(JSON.stringify(events)).not.toContain('secret');
  await expect(sudo.approve(id, 'secret')).rejects.toThrow();
});

test('отклонение не запускает команду', async () => {
  const events: any[] = [];
  const sudo = new Sudo((_, event) => events.push(event), async () => { throw new Error('unexpected'); });
  const result = sudo.request(1, 'id', 'причина', '/tmp');
  sudo.reject(events[0].id);
  expect(await result).toEqual({ status: 'rejected' });
});

test('истёкший запрос нельзя подтвердить', async () => {
  const events: any[] = [];
  const sudo = new Sudo((_, event) => events.push(event), async () => { throw new Error('unexpected'); }, 5);
  const result = sudo.request(1, 'id', 'причина', '/tmp');
  expect(await result).toEqual({ status: 'timeout' });
  await expect(sudo.approve(events[0].id, 'secret')).rejects.toThrow();
});

test('пароль не попадает в таблицу events', async () => {
  const { openDatabase, writeEvent } = await import('./database');
  const db = openDatabase(':memory:');
  const sudo = new Sudo((id, event) => { writeEvent(db, id, event); }, async () => ({ exitCode: 0, output: '' }));
  const pending = sudo.request(1, 'id', 'причина', '/tmp');
  const rows = () => db.query('SELECT json FROM events').all() as { json: string }[];
  const id = JSON.parse(rows()[0].json).id;
  await sudo.approve(id, 'unique-password');
  await pending;
  expect(JSON.stringify(rows())).not.toContain('unique-password');
  db.close();
});
