import { test, expect } from 'bun:test';
import { mkdtemp, rm } from 'node:fs/promises';
import { tmpdir } from 'node:os';
import { join } from 'node:path';
import { ExternalMcp } from './external-mcp';
import { Secrets } from './secrets';
import { claudeLaunch } from './claude';
import { codexLaunch } from './codex';
import { opencodeLaunch, opencodeServers } from './opencode';

// Parallel web_search должен доходить до всех трёх harness одинаково:
// реестр один, секреты подставляются на сервере, дальше каждый адаптер
// кладёт entry в свой формат конфига.
const parallel = {
  name: 'parallel', enabled: true, roles: ['secretary', 'specialist'],
  type: 'http', url: 'https://mcp.parallel.ai/mcp?tools=web_search,web_fetch',
  headers: { Authorization: 'Bearer ${secret:PARALLEL_API_KEY}' },
};

async function setup() {
  const root = await mkdtemp(join(tmpdir(), 'secretary-harness-mcp-'));
  await new Secrets(root).set('PARALLEL_API_KEY', 'test-key');
  new ExternalMcp(root).save([parallel]);
  return root;
}

for (const role of ['secretary', 'specialist'] as const) {
  test(`parallel виден в Claude (${role})`, async () => {
    const root = await setup();
    try {
      const { args, env } = await claudeLaunch({ model: 'x', mcpRoot: root, mcpRole: role } as any);
      const config = JSON.parse(args[args.indexOf('--mcp-config') + 1] as string);
      expect(config.mcpServers.parallel.type).toBe('http');
      expect(config.mcpServers.parallel.url).toBe((parallel as any).url);
      expect(Object.values(env as Record<string, string>)).toContain('Bearer test-key');
    } finally { await rm(root, { recursive: true, force: true }); }
  });

  test(`parallel виден в Codex (${role})`, async () => {
    const root = await setup();
    try {
      const { args, env } = await codexLaunch({ model: 'x', mcpRoot: root, mcpRole: role } as any);
      const flat = args.join(' ');
      expect(flat).toContain('mcp_servers.parallel.url');
      expect(flat).not.toContain('"parallel"');
      expect(flat).toContain((parallel as any).url);
      expect(Object.values(env as Record<string, string>)).toContain('Bearer test-key');
    } finally { await rm(root, { recursive: true, force: true }); }
  });

  test(`parallel виден в OpenCode (${role})`, async () => {
    const root = await setup();
    try {
      const { prepared } = await opencodeLaunch({ mcpRoot: root, mcpRole: role } as any);
      const servers = opencodeServers({}, prepared);
      const entry = servers.find(s => s.name === 'parallel');
      expect(entry).toMatchObject({ type: 'http', url: (parallel as any).url });
      expect(JSON.stringify(entry)).toContain('Bearer test-key');
    } finally { await rm(root, { recursive: true, force: true }); }
  });
}
