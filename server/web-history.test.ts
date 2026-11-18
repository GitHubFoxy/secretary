import { expect, test } from 'bun:test';
import { chatHistory, chatHistoryPage } from './web-history';
import { recentChatEvents } from './web-history';
import { Database } from 'bun:sqlite';

test('keeps all chat event types and discards protocol noise', () => {
  const events = [{type:'user',message:{content:'hi'}}, {type:'result',result:'ok'}, {type:'error',message:'error'}, {type:'cancelled',text:'no'}, {type:'delivery_uncertain',text:'maybe'}, {method:'item/completed',params:{item:{type:'agentMessage',text:'hello'}}}, {method:'item/completed',params:{item:{type:'commandExecution'}}}, {method:'item/agentMessage/delta'}, null];
  expect(chatHistory(events).map(e => e.type || e.method)).toEqual(['user','result','error','cancelled','delivery_uncertain','item/completed']);
  expect(chatHistory([{type:'result',result:'ok', noisy:'secret'}])).toEqual([{type:'result',result:'ok'}]);
});

test('loads ten visible messages in order while skipping noise across batches', () => {
  const db = new Database(':memory:');
  db.run('CREATE TABLE events (session_id INTEGER, json TEXT, time TEXT)');
  const add = db.query('INSERT INTO events VALUES (?, ?, ?)');
  for (let i = 0; i < 15; i++) {
    add.run(1, JSON.stringify({type:'user',message:{content:`message ${i}`}}), '');
    for (let j = 0; j < 30; j++) add.run(1, JSON.stringify({method:'item/agentMessage/delta'}), '');
    add.run(1, JSON.stringify({type:'result',result:'NO_REPLY'}), '');
    add.run(2, JSON.stringify({type:'user',message:{content:'other session'}}), '');
  }
  expect(recentChatEvents(db, 1).map(e => e.message.content)).toEqual(Array.from({length:10}, (_,i) => `message ${i+5}`));
  const initial = chatHistoryPage(db, 1);
  expect(initial.hasMore).toBe(true);
  expect(initial.events.map(e => e.message.content)).toEqual(Array.from({length:10}, (_,i) => `message ${i+5}`));
  const full = chatHistoryPage(db, 1, true);
  expect(full.hasMore).toBe(false);
  expect(full.events.map(e => e.message.content)).toEqual(Array.from({length:15}, (_,i) => `message ${i}`));
  expect(chatHistoryPage(db, 2).hasMore).toBe(true);
  expect(chatHistoryPage(db, 3)).toEqual({events:[], hasMore:false});
  expect(db.query('SELECT count(*) AS n FROM events WHERE session_id = 1').get()).toEqual({n:480});
  db.close();
});
