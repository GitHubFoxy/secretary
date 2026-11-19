import { expect, test } from 'bun:test';
import { Database } from 'bun:sqlite';
import { chatHistoryPage, chatMessage, reportText } from './chat';

test('события обоих harness превращаются в сообщения чата', () => {
  expect(chatMessage({ type: 'result', result: 'ответ', usage: {} })).toEqual({ kind: 'agent', text: 'ответ' });
  expect(chatMessage({ method: 'item/completed', params: { item: { type: 'agentMessage', text: 'ответ' } } })).toEqual({ kind: 'agent', text: 'ответ' });
  expect(chatMessage({ type: 'result', result: 'NO_REPLY' })).toBeNull();
  expect(chatMessage({ type: 'assistant', message: {} })).toBeNull();
  expect(chatMessage({ type: 'user', message: { content: reportText({ id: 7, harness: 'codex', folder: '/p/x' }, 'готово') } }))
    .toEqual({ kind: 'report', text: 'готово', report: { id: 7, harness: 'codex', folder: '/p/x' } });
  expect(chatMessage({ type: 'delivery_uncertain', text: 'т' })).toEqual({ kind: 'uncertain', text: 'т', images: [] });
});

test('история отдаётся страницами по видимым сообщениям', () => {
  const db = new Database(':memory:');
  db.run('CREATE TABLE events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)');
  for (let i = 0; i < 15; i++) {
    db.query('INSERT INTO events VALUES (1, ?, ?)').run(JSON.stringify({ type: 'user', message: { content: `message ${i}` } }), '');
    db.query('INSERT INTO events VALUES (1, ?, ?)').run(JSON.stringify({ type: 'status', status: 'idle' }), '');
  }
  const page = chatHistoryPage(db, 1);
  expect(page.hasMore).toBe(true);
  expect(page.messages.map(m => m.text)).toEqual(Array.from({ length: 10 }, (_, i) => `message ${i + 5}`));
  expect(chatHistoryPage(db, 1, true)).toMatchObject({ hasMore: false, messages: { length: 15 } });
  expect(chatHistoryPage(db, 2)).toEqual({ messages: [], hasMore: false });
});
