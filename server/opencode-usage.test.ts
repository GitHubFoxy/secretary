import { expect, test } from 'bun:test';
import { Database } from 'bun:sqlite';
import { opencodeUsage } from './opencode-usage';

const turn = (usage: object) => JSON.stringify({ type: 'opencode_result', text: 'ответ', stopReason: 'end_turn', usage });

test('расход OpenCode складывается по дням из событий', () => {
  const db = new Database(':memory:');
  db.run('CREATE TABLE events (session_id INTEGER NOT NULL, json TEXT NOT NULL, time TEXT NOT NULL)');
  const insert = (time: string, json: string) => db.query('INSERT INTO events VALUES (1, ?, ?)').run(json, time);
  const now = new Date();
  const iso = (daysAgo: number) => new Date(now.getTime() - daysAgo * 86_400_000).toISOString();
  insert(iso(0), turn({ inputTokens: 100, outputTokens: 20, thoughtTokens: 5, cachedReadTokens: 80 }));
  insert(iso(0), turn({ inputTokens: 50, outputTokens: 10 }));
  insert(iso(3), turn({ inputTokens: 1000, outputTokens: 200 }));
  insert(iso(8), turn({ inputTokens: 5000, outputTokens: 500 }));
  insert(iso(0), JSON.stringify({ type: 'opencode_result', text: '', stopReason: 'cancelled' }));
  insert(iso(0), JSON.stringify({ type: 'result', result: 'ответ', usage: {} }));
  insert(iso(0), 'not json');

  const report = opencodeUsage(db, now);
  expect(report.today).toEqual({ inputTokens: 150, outputTokens: 30, thoughtTokens: 5, cachedReadTokens: 80, turns: 2 });
  expect(report.week).toEqual({ inputTokens: 1150, outputTokens: 230, thoughtTokens: 5, cachedReadTokens: 80, turns: 3 });
  expect(report.allTime).toEqual({ turns: 4, inputTokens: 6150, outputTokens: 730 });
  expect(report.days.map(d => [d.day, d.turns])).toHaveLength(3);
  expect(report.days[0].turns).toBe(2);
});
