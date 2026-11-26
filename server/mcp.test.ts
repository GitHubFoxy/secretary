import { afterAll, expect, test } from 'bun:test';
import { mkdtemp, readFile, rm, writeFile } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { openDatabase } from './database';
import type { Turn, TurnOptions } from './harness';
import { mkdtempSync, writeFileSync } from 'node:fs';
import { issueUrl, serveMcp } from './mcp';
import { instructionsFile, setSpecialistModel } from './persona';
import { Sessions } from './sessions';

type Call = { text: string; options: TurnOptions; start(): void; answer(text: string): void; end(completed: boolean): void };

function setup(root = '/nonexistent') {
  const calls: Call[] = [];
  const turn: Turn = (options, text) => new Promise(resolve => calls.push({
    text, options,
    start() { options.onDispatching(); options.onAccepted(); },
    answer(answer) { options.onEvent({ type: 'result', result: answer }); resolve(true); },
    end: resolve,
  }));
  const db = openDatabase(':memory:');
  const sessions = new Sessions(db, {
    turns: { claude: turn, codex: turn },
    checkModel: async () => {},
    onEvent() {},
    onChange() {},
  });
  sessions.load(root);
  const server = serveMcp(sessions, {
    port: 0,
    root,
    listModels: async harness => {
      if (harness === 'codex') throw new Error('codex not installed');
      return [{ id: 'opus', supportedReasoningEfforts: [{ reasoningEffort: 'low' }, { reasoningEffort: 'high' }] }];
    },
  });
  const rpc = async (callerId: number, method: string, params: unknown = {}) => {
    const res = await fetch(`${server.url}mcp/${callerId}`, {
      method: 'POST',
      headers: { 'content-type': 'application/json', authorization: `Bearer ${(sessions as any).tokens.get(callerId)}` },
      body: JSON.stringify({ jsonrpc: '2.0', id: 1, method, params }),
    });
    return { status: res.status, body: res.headers.get('content-type')?.includes('json') ? await res.json() : await res.text() };
  };
  const callTool = async (callerId: number, name: string, args: Record<string, unknown> = {}) => {
    const res = await rpc(callerId, 'tools/call', { name, arguments: args });
    expect(res.status).toBe(200);
    const content = res.body.result.content[0].text;
    return { isError: !!res.body.result.isError, data: res.body.result.isError ? content : JSON.parse(content) };
  };
  return { server, sessions, calls, rpc, callTool };
}

const servers: ReturnType<typeof serveMcp>[] = [];
afterAll(() => { for (const s of servers) s.stop(true); });

test('Secretary Agent: list отдаёт сессии и модели, send создаёт Specialist с наследованием и поддерживает queue/immediate', async () => {
  const env = setup();
  servers.push(env.server);

  const tools = await env.rpc(1, 'tools/list');
  expect(tools.body.result.tools.map((t: any) => t.name)).toEqual(['list', 'send', 'read', 'stop', 'send_file', 'day_summary', 'report_issue']);

  const listed = await env.callTool(1, 'list');
  expect(listed.isError).toBe(false);
  expect(listed.data.sessions).toEqual([]);
  expect(listed.data.models).toEqual({ claude: [{ id: 'opus', reasoning: ['low', 'high'] }] });

  const created = await env.callTool(1, 'send', { name: 'Заметки', folder: '/tmp/notes', message: 'разбери' });
  expect(created.isError).toBe(false);
  const specId = created.data.id;
  expect(env.sessions.get(specId).info).toMatchObject({ name: 'Заметки', folder: '/tmp/notes', harness: 'claude', model: 'opus', parent_id: 1 });

  env.calls[0].start();
  await env.callTool(1, 'send', { id: specId, message: 'в очередь' });
  expect(env.calls[0].options.interrupt.aborted).toBe(false);

  await env.callTool(1, 'send', { id: specId, message: 'срочно', immediate: true });
  expect(env.calls[0].options.interrupt.aborted).toBe(true);
});

test('Specialist изолирован на своих детях, наследует folder, а уровень 2 не имеет доступа к MCP', async () => {
  const env = setup();
  servers.push(env.server);

  const s1 = (await env.callTool(1, 'send', { name: 'Проект А', folder: '/tmp/a', message: 'старт' })).data.id;
  const s2 = (await env.callTool(1, 'send', { name: 'Проект Б', folder: '/tmp/b', message: 'старт' })).data.id;

  const childRes = await env.callTool(s1, 'send', { name: 'фича', message: 'сделай' });
  expect(childRes.isError).toBe(false);
  const childId = childRes.data.id;
  expect(env.sessions.get(childId).info).toMatchObject({ parent_id: s1, folder: '/tmp/a' });

  // Specialist sees only its own children and cannot touch Secretary Agent or another Specialist.
  expect((await env.callTool(s1, 'list')).data.sessions.map((s: any) => s.id)).toEqual([childId]);
  expect((await env.callTool(s1, 'send', { id: s2, message: 'чужой' })).isError).toBe(true);
  expect((await env.callTool(s1, 'read', { id: 1 })).isError).toBe(true);
  expect((await env.callTool(s1, 'stop', { id: s2 })).isError).toBe(true);

  // Level-2 child is rejected at HTTP level.
  const denied = await env.rpc(childId, 'tools/list');
  expect(denied.status).toBe(401);

  // Stopping Specialist from Secretary Agent cascades to its running child.
  await env.callTool(1, 'stop', { id: s1 });
  expect(env.calls[0].options.stop.aborted).toBe(true);
  expect(env.calls[2].options.stop.aborted).toBe(true);
});

test('read по умолчанию отдаёт очищенные сообщения, а с raw: true — сырые события', async () => {
  const env = setup();
  servers.push(env.server);

  const s1 = (await env.callTool(1, 'send', { name: 'Проект', folder: '/tmp/p', message: 'привет' })).data.id;
  env.calls[0].start();
  env.calls[0].options.onEvent({ type: 'tool_use', name: 'Bash' });
  env.calls[0].answer('готово');

  const clean = await env.callTool(1, 'read', { id: s1 });
  expect(clean.data).toEqual([
    { kind: 'user', text: 'привет', images: 0 },
    { kind: 'agent', text: 'готово' },
  ]);

  const raw = await env.callTool(1, 'read', { id: s1, raw: true, limit: 10 });
  expect(raw.data.some((e: any) => e.type === 'tool_use')).toBe(true);
});

test('send_file показывает копию файла в чате того, кто его отправил: Secretary Agent или Specialist', async () => {
  const root = await mkdtemp(join(tmpdir(), 'secretary-send-file-'));
  try {
    const env = setup(root);
    servers.push(env.server);
    await writeFile(join(root, 'кот.png'), 'PNG');
    const sent = await env.callTool(1, 'send_file', { path: join(root, 'кот.png'), caption: 'Вот кот' });
    expect(sent).toEqual({ isError: false, data: { name: 'кот.png', size: 3 } });
    const [message] = env.sessions.chatHistory(1, 1) as any[];
    expect(message).toMatchObject({ kind: 'agent', text: 'Вот кот', files: [{ name: 'кот.png', size: 3, type: 'image/png' }] });
    expect(await readFile(join(root, 'files', message.files[0].id, 'кот.png'), 'utf8')).toBe('PNG');
    expect((await env.callTool(1, 'send_file', { path: join(root, 'нет.png') })).isError).toBe(true);

    const { id } = (await env.callTool(1, 'send', { name: 'S', folder: root, message: 'go' })).data;
    expect((await env.callTool(id, 'send_file', { path: 'кот.png' })).isError).toBe(false);
    expect((env.sessions.chatHistory(id, 1) as any[])[0]).toMatchObject({ kind: 'agent', files: [{ name: 'кот.png' }] });
    expect(env.sessions.chatHistory(1, 5).filter((m: any) => m.files)).toHaveLength(1);
  } finally { await rm(root, { recursive: true, force: true }); }
});

test('report_issue: ссылка на новый issue с текстом и версией', () => {
  const url = new URL(issueUrl('Не работает Power', 'Нажал Power — ничего', { harness: 'claude', model: 'opus' }, 'abc123'));
  expect(url.origin + url.pathname).toBe('https://github.com/GitHubFoxy/secretary/issues/new');
  expect(url.searchParams.get('title')).toBe('Не работает Power');
  expect(url.searchParams.get('body')).toContain('Нажал Power — ничего');
  expect(url.searchParams.get('body')).toContain('Версия: abc123');
  expect(url.searchParams.get('body')).toContain('Harness: claude, модель opus');
});

test('Secretary Agent: list показывает видимых Specialists, скрытые — только по hidden: true', async () => {
  const env = setup();
  servers.push(env.server);
  const settings = { harness: 'claude', model: 'opus', reasoning: null };
  const active = await env.sessions.create({ ...settings, folder: '/tmp/a', parentId: 1, name: 'Активный' });
  const settled = await env.sessions.create({ ...settings, folder: '/tmp/b', parentId: 1, name: 'Завершённый' });
  await env.sessions.create({ ...settings, folder: '/tmp/a', parentId: active.id, name: 'Worker' });
  settled.setHidden(true);

  const names = async (args = {}) => (await env.callTool(1, 'list', args)).data.sessions.map((s: any) => s.name);
  expect(await names()).toEqual(['Активный']);
  expect(await names({ hidden: true })).toEqual(['Завершённый']);
});

test('новый Specialist берёт модель из настройки владельца и получает его инструкции; Worker их не получает', async () => {
  const root = mkdtempSync(join(tmpdir(), 'secretary-'));
  setSpecialistModel(root, { harness: 'codex', model: 'gpt', reasoning: 'high' });
  writeFileSync(instructionsFile(root, 'specialist'), 'Не пушить без «да».');
  const env = setup(root);
  servers.push(env.server);

  const created = await env.callTool(1, 'send', { name: 'Проект', folder: '/tmp/p', message: 'начни' });
  expect(env.sessions.get(created.data.id).info).toMatchObject({ harness: 'codex', model: 'gpt', reasoning: 'high' });
  expect(env.calls[0].options.systemPrompt).toContain('Ты — Specialist');
  expect(env.calls[0].options.systemPrompt).toContain('Не пушить без «да».');

  const worker = await env.callTool(created.data.id, 'send', { name: 'Фича', message: 'сделай' });
  expect(env.sessions.get(worker.data.id).info).toMatchObject({ harness: 'codex', model: 'gpt', reasoning: 'high' });
  expect(env.calls[1].options.systemPrompt).toBeUndefined();

  setSpecialistModel(root, null);
  const second = await env.callTool(1, 'send', { name: 'Другой', folder: '/tmp/q', message: 'начни' });
  expect(env.sessions.get(second.data.id).info).toMatchObject({ harness: 'claude', model: 'opus' });
});

test('MCP проверяет токен Session, Host и Origin', async () => {
  const env = setup();
  servers.push(env.server);
  const specialist = await env.sessions.create({ harness: 'claude', model: 'opus', reasoning: null, folder: '/tmp', parentId: 1 });
  const tokens = (env.sessions as any).tokens;
  const request = (authorization?: string, extra: Record<string, string> = {}) => fetch(`${env.server.url}mcp/1`, {
    method: 'POST', headers: { 'content-type': 'application/json', ...(authorization ? { authorization } : {}), ...extra },
    body: JSON.stringify({ jsonrpc: '2.0', id: 1, method: 'tools/list' }),
  });
  expect((await request()).status).toBe(401);
  expect((await request(`Bearer ${tokens.get(specialist.id)}`)).status).toBe(401);
  expect((await request(`Bearer ${tokens.get(1)}`)).status).toBe(200);
  expect((await request(`Bearer ${tokens.get(1)}`, { host: 'evil.example' })).status).toBe(403);
  expect((await request(`Bearer ${tokens.get(1)}`, { origin: 'http://127.0.0.1' })).status).toBe(403);
});


test('day_summary показывает события дня только Secretary Agent', async () => {
  const env = setup(); servers.push(env.server);
  const created = await env.callTool(1, 'send', { name: 'Проект', folder: '/tmp', message: 'сделай' });
  env.calls[0].start(); env.calls[0].answer('готово');
  const summary = await env.callTool(1, 'day_summary');
  const session = summary.data.sessions.find((s: any) => s.id === created.data.id);
  expect(session.name).toBe('Проект');
  expect(session.turns).toBe(1);
  expect(session.events.map((e: any) => e.kind)).toEqual(['user', 'agent']);
  expect((await env.callTool(1, 'day_summary', { date: '2024-02-30' })).isError).toBe(true);
  expect((await env.callTool(created.data.id, 'day_summary')).isError).toBe(true);
  expect((await env.callTool(1, 'day_summary', { date: '2000-01-01' })).data.sessions).toEqual([]);
});
