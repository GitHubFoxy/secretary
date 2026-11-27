import type { Database } from 'bun:sqlite';

export type UsageTotals = { inputTokens: number; outputTokens: number; thoughtTokens: number; cachedReadTokens: number; turns: number };
export type UsageDay = { day: string; inputTokens: number; outputTokens: number; turns: number };
export type OpencodeUsageReport = { today: UsageTotals; week: UsageTotals; allTime: { turns: number; inputTokens: number; outputTokens: number }; days: UsageDay[] };

const zero = (): UsageTotals => ({ inputTokens: 0, outputTokens: 0, thoughtTokens: 0, cachedReadTokens: 0, turns: 0 });

const num = (value: unknown) => typeof value === 'number' && Number.isFinite(value) && value >= 0 ? value : 0;

const dayKey = (date: Date) => `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;

// Расход OpenCode считаем из событий Secretary: провайдер (PAG, openai-compatible)
// лимитов не отдаёт — ни отдельным API, ни в ответах, — поэтому показываем только
// фактические токены. totalTokens в opencode_result растёт внутри сессии,
// складывать его между ходами нельзя: суммируем только токены хода.
export function opencodeUsage(db: Database, now = new Date()): OpencodeUsageReport {
  const today: UsageTotals = zero(), week: UsageTotals = zero();
  const allTime = { turns: 0, inputTokens: 0, outputTokens: 0 };
  const byDay = new Map<string, UsageDay>();
  const todayKey = dayKey(now);
  const weekStart = dayKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 6));
  const cutoff = dayKey(new Date(now.getFullYear(), now.getMonth(), now.getDate() - 13));
  const rows = db.query(`SELECT time, json FROM events WHERE json LIKE '%"type":"opencode_result"%'`).all() as { time: string; json: string }[];
  const add = (totals: UsageTotals, usage: any) => {
    totals.inputTokens += num(usage.inputTokens);
    totals.outputTokens += num(usage.outputTokens);
    totals.thoughtTokens += num(usage.thoughtTokens);
    totals.cachedReadTokens += num(usage.cachedReadTokens);
    totals.turns += 1;
  };
  for (const row of rows) {
    let event: any;
    try { event = JSON.parse(row.json); } catch { continue; }
    if (!event || typeof event.usage !== 'object' || event.usage === null) continue;
    const day = dayKey(new Date(row.time));
    allTime.turns += 1;
    allTime.inputTokens += num(event.usage.inputTokens);
    allTime.outputTokens += num(event.usage.outputTokens);
    if (day === todayKey) add(today, event.usage);
    if (day >= weekStart && day <= todayKey) add(week, event.usage);
    const bucket = byDay.get(day) ?? { day, inputTokens: 0, outputTokens: 0, turns: 0 };
    bucket.inputTokens += num(event.usage.inputTokens);
    bucket.outputTokens += num(event.usage.outputTokens);
    bucket.turns += 1;
    byDay.set(day, bucket);
  }
  const days = [...byDay.values()].filter(d => d.day >= cutoff && d.day <= todayKey).sort((a, b) => b.day.localeCompare(a.day));
  return { today, week, allTime, days };
}
