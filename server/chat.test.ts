import { expect, test } from 'bun:test';
import { Database } from 'bun:sqlite';
import { chatPage, chatMessage, reportText, usageLimit } from './chat';

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
  for (let i = 0; i < 45; i++) {
    db.query('INSERT INTO events VALUES (1, ?, ?)').run(JSON.stringify({ type: 'user', message: { content: `message ${i}` } }), '');
    db.query('INSERT INTO events VALUES (1, ?, ?)').run(JSON.stringify({ type: 'status', status: 'idle' }), '');
  }
  const page = chatPage(db, 1);
  expect(page.hasMore).toBe(true);
  expect(page.messages.map(m => m.text)).toEqual(Array.from({ length: 20 }, (_, i) => `message ${i + 25}`));
  const older = chatPage(db, 1, page.messages[0].cursor);
  expect(older.hasMore).toBe(true);
  expect(older.messages.map(m => m.text)).toEqual(Array.from({ length: 20 }, (_, i) => `message ${i + 5}`));
  const first = chatPage(db, 1, older.messages[0].cursor);
  expect(first.hasMore).toBe(false);
  expect(first.messages.map(m => m.text)).toEqual(Array.from({ length: 5 }, (_, i) => `message ${i}`));
  expect(chatPage(db, 2)).toEqual({ messages: [], hasMore: false });
});

test('ошибки хода видны в чате и не выглядят как ответ агента', () => {
  expect(chatMessage({ type: 'result', is_error: true, result: 'Not logged in · Please run /login' }))
    .toEqual({ kind: 'error', text: 'Claude Code не вошёл в подписку. На Host выполните: claude auth login' });
  expect(chatMessage({ type: 'result', is_error: true, result: 'другая ошибка' }))
    .toEqual({ kind: 'error', text: 'другая ошибка' });
  expect(chatMessage({ method: 'turn/completed', params: { turn: { error: { message: 'ошибка Codex' } } } }))
    .toEqual({ kind: 'error', text: 'ошибка Codex' });
});

test('прерывание хода не создаёт сообщение в чате', () => {
  expect(chatMessage({ type: 'result', subtype: 'error_during_execution', is_error: true, terminal_reason: 'aborted_streaming', errors: ['[ede_diagnostic] result_type=user'] })).toBeNull();
  expect(chatMessage({ method: 'turn/completed', params: { turn: { status: 'interrupted', error: null } } })).toBeNull();
  expect(chatMessage({ method: 'turn/completed', params: { turn: { status: 'interrupted', error: { message: 'interrupted' } } } })).toBeNull();
});

test('Claude: вызов инструмента становится шагом', () => {
  expect(chatMessage({type:'assistant', message:{content:[{type:'tool_use', name:'Bash', input:{command:'bun test'}}]}})).toEqual({kind:'step', tool:'Bash', summary:'bun test'});
});
test('Claude: рассуждение становится сообщением thinking', () => {
  expect(chatMessage({type:'assistant', message:{content:[{type:'thinking', thinking:'Проверю тесты'}]}})).toEqual({kind:'thinking', text:'Проверю тесты'});
});
test('Codex: команда становится шагом', () => {
  expect(chatMessage({method:'item/started', params:{item:{type:'commandExecution', command:'bun test'}}})).toEqual({kind:'step', tool:'Bash', summary:'bun test'});
});
test('Codex: рассуждение становится сообщением thinking', () => {
  expect(chatMessage({method:'item/completed', params:{item:{type:'reasoning', summary:['Первое', 'Второе']}}})).toEqual({kind:'thinking', text:'Первое\nВторое'});
});

test('начало ответа передаётся без текста', () => {
  expect(chatMessage({type:'assistant', message:{content:[{type:'text', text:'секрет'}]}})).toEqual({kind:'writing'});
  expect(chatMessage({method:'item/started', params:{item:{type:'agentMessage'}}})).toEqual({kind:'writing'});
});

test('завершённое сжатие обоих harness сохраняется в истории', () => {
  const time = '2026-11-26T12:00:00.000Z';
  const claude = {type: 'system', subtype: 'compact_boundary', compact_metadata: {trigger: 'manual', pre_tokens: 27445, post_tokens: 1192}};
  const codex = {method: 'item/completed', params: {item: {type: 'contextCompaction', id: '1', contentTokenCount: null}}};
  expect(chatMessage(claude, time)).toEqual({kind: 'compaction', time, beforeTokens: 27445, afterTokens: 1192});
  expect(chatMessage(codex, time)).toEqual({kind: 'compaction', time});
  expect(chatMessage({...codex, method: 'item/started'}, time)).toBeNull();
  expect(chatMessage({type: 'system', subtype: 'compact_boundary', compact_metadata: {pre_tokens: -1, post_tokens: Infinity}}, time)).toEqual({kind: 'compaction', time});
  const db = new Database(':memory:');
  db.run('CREATE TABLE events (session_id INTEGER, json TEXT, time TEXT)');
  for (const event of [claude, codex]) db.query('INSERT INTO events VALUES (1, ?, ?)').run(JSON.stringify(event), time);
  expect(chatPage(db, 1).messages.map(({cursor, ...message}) => message)).toEqual([
    {kind:'compaction', time, beforeTokens:27445, afterTokens:1192},
    {kind:'compaction', time},
  ]);
});

test('распознаёт лимит Claude Code и Codex, но не обычные ошибки', () => {
  const reset = new Date(2030, 0, 2, 15, 30).getTime();
  const now = new Date(2030, 0, 2, 10).getTime();
  expect(usageLimit({type:'result', is_error:true, result:'Usage limit reached, resets at 3:30 PM', emittedAtMs:now})).toEqual({resetsAt:reset});
  expect(usageLimit({method:'turn/completed', params:{turn:{error:{codexErrorInfo:'usageLimitExceeded', resetsAt:reset / 1000}}}})).toEqual({resetsAt:reset});
  expect(usageLimit({method:'error', emittedAtMs:now, params:{willRetry:false, error:{codexErrorInfo:'usageLimitExceeded', message:'try again at 3:30 PM.'}}})).toEqual({resetsAt:reset});
  expect(usageLimit({method:'error', params:{willRetry:true, error:{codexErrorInfo:'usageLimitExceeded'}}})).toBeNull();
  expect(usageLimit({type:'result', is_error:true, result:'authentication failed'})).toBeNull();
  expect(usageLimit({type:'result', is_error:false, result:'usage limit'})).toBeNull();
});

const opencodeUpdate = (update: object) => ({ method: 'session/update', params: { sessionId: 'ses_1', update } });

test('OpenCode: итог хода становится ответом, отмена — ничем', () => {
  expect(chatMessage({ type: 'opencode_result', text: 'ответ', stopReason: 'end_turn', usage: {} })).toEqual({ kind: 'agent', text: 'ответ' });
  expect(chatMessage({ type: 'opencode_result', text: 'NO_REPLY', stopReason: 'end_turn', usage: {} })).toBeNull();
  expect(chatMessage({ type: 'opencode_result', text: 'частично', stopReason: 'cancelled', usage: {} })).toBeNull();
});

test('OpenCode: чанки и вызовы инструментов становятся сообщениями чата', () => {
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'agent_message_chunk', content: { type: 'text', text: 'прив' } }))).toEqual({ kind: 'writing' });
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'agent_thought_chunk', content: { type: 'text', text: 'проверю тесты' } }))).toEqual({ kind: 'thinking', text: 'проверю тесты' });
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'tool_call', toolCallId: 'c1', name: 'shell', kind: 'execute', title: 'shell', status: 'pending', rawInput: { command: 'bun test' } })))
    .toEqual({ kind: 'step', tool: 'shell', summary: 'bun test' });
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'tool_call', toolCallId: 'c2', name: 'read', kind: 'read', title: 'read', status: 'pending', rawInput: { path: 'note.txt' } })))
    .toEqual({ kind: 'step', tool: 'read', summary: 'note.txt' });
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'tool_call_update', toolCallId: 'c1', status: 'completed' }))).toBeNull();
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'usage_update', used: 1, size: 2 }))).toBeNull();
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'available_commands_update', availableCommands: [] }))).toBeNull();
});

test('OpenCode: сжатие видно и через обновление, и через маркер', () => {
  const time = '2026-11-27T12:00:00.000Z';
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'compaction_update', status: 'completed' }), time)).toEqual({ kind: 'compaction', time });
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'compaction_update', status: 'started' }), time)).toBeNull();
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'session_info_update', _meta: { 'opencode/compaction': { status: 'failed' } } }), time)).toEqual({ kind: 'compaction', time });
  expect(chatMessage(opencodeUpdate({ sessionUpdate: 'session_info_update', _meta: { 'opencode/compaction': { status: 'started' } } }), time)).toBeNull();
});

test('OpenCode: повтор после rate-limit распознаётся как лимит', () => {
  const retry = (type: string) => opencodeUpdate({ sessionUpdate: 'session_info_update', _meta: { 'opencode/retry': { attempt: 1, nextRetryAt: '2026-11-27T12:00:05.000Z', error: { type, message: 'slow down' } } } });
  expect(usageLimit(retry('provider.rate-limit'))).toEqual({ resetsAt: Date.parse('2026-11-27T12:00:05.000Z') });
  expect(chatMessage(retry('provider.rate-limit'))).toBeNull();
  expect(usageLimit(retry('provider.auth'))).toBeNull();
});
