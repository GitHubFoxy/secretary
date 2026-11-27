// Изолированный стенд для проверки модалки «Лимиты»: отдаёт собранный
// web/dist, фикстуры /codex/limits и /opencode/limits и минимальный /ws.
const port = Number(process.env.USAGE_TEST_PORT || 5199);
const build = process.env.USAGE_TEST_BUILD;
if (!build) throw new Error('Set USAGE_TEST_BUILD to an isolated build directory');
let mode = 'ok';
const now = Date.now();
const codex = { rateLimits: { limitName: 'Plus', planType: 'plus', primary: { usedPercent: 40, resetsAt: Math.floor(now / 1000) + 3600, windowDurationMins: 300 }, secondary: { usedPercent: 70, resetsAt: Math.floor(now / 1000) + 7200, windowDurationMins: 10080 } } };
const opencode = { windows: [
  { id: 'rolling', percent: 62, resetsAt: new Date(now + 3600_000).toISOString(), limited: false },
  { id: 'weekly', percent: 25, resetsAt: new Date(now + 2 * 86400_000).toISOString(), limited: false },
  { id: 'monthly', percent: 12, resetsAt: new Date(now + 29 * 86400_000).toISOString(), limited: false },
] };
const sessions = [{ id: 1, name: 'Test', secretary: true, status: 'idle', harness: 'opencode', model: 'pag/beam-free' }];
Bun.serve({
  port,
  fetch(req, s) {
    const u = new URL(req.url);
    if (u.pathname === '/ws' && s.upgrade(req)) return;
    if (u.pathname === '/control') {
      mode = u.searchParams.get('mode') || mode;
      return Response.json({ mode });
    }
    if (u.pathname === '/codex/limits' || u.pathname === '/opencode/limits') {
      if (mode === 'fail') return new Response('synthetic failure', { status: 500 });
      return Response.json(u.pathname === '/codex/limits' ? codex : opencode);
    }
    if (u.pathname === '/settings/video') return Response.json({ interval: '1' });
    const f = Bun.file(build + (u.pathname === '/' ? '/index.html' : u.pathname));
    return f.exists().then(ok => (ok ? new Response(f) : Response.json({})));
  },
  websocket: {
    open(w) { w.send(JSON.stringify({ type: 'sessions', sessions })); },
    message(w, data) {
      const c = JSON.parse(data);
      if (c.type === 'draft') {
        w.send(JSON.stringify({ type: 'reply', requestId: c.requestId, text: '' }));
        return;
      }
      w.send(JSON.stringify({ type: 'reply', requestId: c.requestId, messages: [], hasMore: false, queue: [] }));
    },
  },
});
console.log(`limits-test stand on ${port}`);
