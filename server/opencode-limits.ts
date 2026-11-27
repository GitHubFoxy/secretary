import { homedir } from 'node:os';

// Лимиты OpenCode Go: провайдер отдаёт их отдельным API (см. https://opencode.ai/docs/console/go#usage),
// в ответах inference их нет. Тот же ключ, которым PAG ходит в Go upstream.
const keyFile = `${homedir()}/pag/cpa/secrets/opencode-go.key`;

export type OpencodeWindow = { id: 'rolling' | 'weekly' | 'monthly'; percent: number; resetsAt?: string; limited: boolean };
export type OpencodeLimits = { windows: OpencodeWindow[] };

export function parseOpencodeLimits(data: any): OpencodeLimits {
  const usage = data?.usage;
  if (!usage || typeof usage !== 'object') throw new Error('OpenCode не вернул лимиты');
  const windows: OpencodeWindow[] = [];
  for (const id of ['rolling', 'weekly', 'monthly'] as const) {
    const meter = usage[id];
    if (!meter || typeof meter.percent !== 'number') throw new Error('OpenCode вернул лимиты без окна ' + id);
    windows.push({ id, percent: meter.percent, ...(typeof meter.resetsAt === 'string' ? { resetsAt: meter.resetsAt } : {}), limited: meter.status === 'rate-limited' });
  }
  return { windows };
}

export async function opencodeLimits(): Promise<OpencodeLimits> {
  const key = await Bun.file(keyFile).text().catch(() => '');
  if (!key.trim()) throw new Error('Нет ключа OpenCode Go на Host');
  const response = await fetch('https://opencode.ai/inference/go/v1/usage', {
    headers: { Authorization: `Bearer ${key.trim()}` },
    signal: AbortSignal.timeout(15_000),
  });
  if (!response.ok) throw new Error(`OpenCode вернул HTTP ${response.status}`);
  return parseOpencodeLimits(await response.json());
}
