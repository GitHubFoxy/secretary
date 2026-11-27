import { readJsonLines } from './harness';
import { mkdtempSync, rmSync } from 'node:fs';
import { tmpdir } from 'node:os';
import { join } from 'node:path';

type ModelDetails = { id: string; aliases?: string[]; supportedReasoningEfforts: { reasoningEffort: string }[] };

export function parseClaudeModels(output: string): ModelDetails[] {
  for (const line of output.split('\n')) {
    if (!line.trim()) continue;
    const msg = JSON.parse(line);
    if (msg.type !== 'control_response' || msg.response?.request_id !== 'models') continue;
    if (msg.response.subtype !== 'success') throw new Error('Claude Code не вернул список моделей');
    const list = msg.response.response?.models;
    if (!Array.isArray(list)) throw new Error('Claude Code вернул пустой список моделей');
    const byId = new Map<string, ModelDetails>();
    for (const model of list) {
      if (!model.resolvedModel) continue;
      const existing = byId.get(model.resolvedModel);
      if (existing) {
        if (model.value && !existing.aliases?.includes(model.value)) existing.aliases = [...(existing.aliases || []), model.value];
        continue;
      }
      byId.set(model.resolvedModel, {
        id: model.resolvedModel,
        ...(model.value && model.value !== model.resolvedModel ? { aliases: [model.value] } : {}),
        supportedReasoningEfforts: (model.supportedEffortLevels || []).map((reasoningEffort: string) => ({ reasoningEffort })),
      });
    }
    if (!byId.size) throw new Error('Claude Code вернул пустой список моделей');
    return [...byId.values()];
  }
  throw new Error('Claude Code не ответил на запрос списка моделей');
}

async function claudeModels() {
  const p = Bun.spawn(['claude', '-p', '--input-format', 'stream-json', '--output-format', 'stream-json', '--verbose'],
    { stdin: 'pipe', stdout: 'pipe', stderr: 'ignore', signal: AbortSignal.timeout(10000) });
  try {
    p.stdin.write(JSON.stringify({ type: 'control_request', request_id: 'models', request: { subtype: 'initialize' } }) + '\n');
    p.stdin.end();
    return parseClaudeModels(await new Response(p.stdout).text());
  } finally { p.kill(); }
}

async function codexModels(): Promise<ModelDetails[]> {
  const p = Bun.spawn(['codex', 'app-server'], { stdin: 'pipe', stdout: 'pipe', stderr: 'ignore' });
  const send = (message: object) => { p.stdin.write(JSON.stringify(message) + '\n'); p.stdin.flush(); };
  send({ id: 1, method: 'initialize', params: { clientInfo: { name: 'secretary', version: '0.1' }, capabilities: null } });
  send({ method: 'initialized' });
  send({ id: 2, method: 'model/list', params: {} });
  let models: ModelDetails[] | undefined;
  try {
    await readJsonLines(p.stdout, msg => {
      if (msg.id !== 2) return;
      if (msg.error) throw new Error(msg.error.message);
      models = msg.result.data;
      return false;
    });
  } finally { p.kill(); }
  if (!models) throw new Error('Codex exited');
  return models;
}

// Effort levels differ per model ('default' means reasoning untouched, so it is not a level).
export function parseOpencodeModels(models: string[], efforts: Record<string, string[]>): ModelDetails[] {
  return models.map(id => ({
    id,
    supportedReasoningEfforts: (efforts[id] ?? []).filter(level => level !== 'default').map(reasoningEffort => ({ reasoningEffort })),
  }));
}

async function opencodeModels(): Promise<ModelDetails[]> {
  const root = mkdtempSync(join(tmpdir(), 'secretary-opencode-models-'));
  try {
    const p = Bun.spawn(['opencode', 'acp'], { cwd: root, stdin: 'pipe', stdout: 'pipe', stderr: 'ignore',
      env: { ...process.env, OPENCODE_CONFIG_CONTENT: JSON.stringify({ permissions: [{ action: '*', resource: '*', effect: 'allow' }] }) },
      signal: AbortSignal.timeout(120_000) });
    const send = (message: object) => { p.stdin.write(JSON.stringify(message) + '\n'); p.stdin.flush(); };
    let id = 0;
    // Model switches resolve out of order, so each response is matched by its request id.
    const pending = new Map<number, { resolve: (value: any) => void; reject: (error: Error) => void }>();
    const request = (method: string, params: object): Promise<any> => new Promise((resolve, reject) => {
      pending.set(++id, { resolve, reject });
      send({ jsonrpc: '2.0', id, method, params });
    });
    const reading = readJsonLines(p.stdout, msg => {
      if (msg.method || msg.id === undefined) return;
      const waiter = pending.get(msg.id);
      pending.delete(msg.id);
      if (msg.error) waiter?.reject(new Error(msg.error.message ?? 'OpenCode не ответил'));
      else waiter?.resolve(msg.result);
    });
    try {
      await request('initialize', { protocolVersion: 1, clientCapabilities: {} });
      const session = await request('session/new', { cwd: root, mcpServers: [] });
      const sessionId = session.sessionId;
      const models: string[] = [];
      for (const option of session.configOptions ?? []) if (option.id === 'model') for (const item of option.options ?? []) models.push(item.value);
      if (!models.length) throw new Error('OpenCode вернул пустой список моделей');
      const efforts: Record<string, string[]> = {};
      for (const model of models) {
        const updated = await request('session/set_config_option', { sessionId, configId: 'model', value: model });
        for (const option of updated.configOptions ?? []) if (option.id === 'effort') efforts[model] = (option.options ?? []).map((item: any) => item.value);
      }
      return parseOpencodeModels(models, efforts);
    } finally { p.kill(); await reading.catch(() => {}); }
  } finally {
    rmSync(root, { recursive: true, force: true });
  }
}

const loaders: Record<string, () => Promise<ModelDetails[]>> = { claude: claudeModels, codex: codexModels, opencode: opencodeModels };
const cache = new Map<string, Promise<ModelDetails[]>>();

// Asking a harness for its models takes seconds, so validation reuses the last list. The settings screen asks for a fresh one.
export function modelDetails(harness: string, fresh = false) {
  const load = loaders[harness];
  if (!load) throw new Error('Unknown harness');
  if (fresh || !cache.has(harness)) {
    const models = load();
    cache.set(harness, models);
    models.catch(() => cache.delete(harness));
  }
  return cache.get(harness)!;
}

export async function checkReasoning(harness: string, model: string, reasoning: string) {
  const details = (await modelDetails(harness)).find(m => m.id === model || m.aliases?.includes(model));
  if (!details?.supportedReasoningEfforts.some(e => e.reasoningEffort === reasoning)) throw new Error('Unsupported model or reasoning level');
}
