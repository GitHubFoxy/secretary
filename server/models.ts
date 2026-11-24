import { readJsonLines } from './harness';

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

const loaders: Record<string, () => Promise<ModelDetails[]>> = { claude: claudeModels, codex: codexModels };
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
