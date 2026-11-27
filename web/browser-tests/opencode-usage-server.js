// Изолированный стенд для проверки карточки «Расход OpenCode»: отдаёт собранный
// web/dist, фикстуру /opencode/usage и минимальный /ws. Живую службу не трогает.
const port = Number(process.env.USAGE_TEST_PORT || 5199);
const build = process.env.USAGE_TEST_BUILD;
if (!build) throw new Error('Set USAGE_TEST_BUILD to an isolated build directory');
let mode = 'ok';
const hits = [];
const usage = {
  today: { inputTokens: 150, outputTokens: 30, thoughtTokens: 5, cachedReadTokens: 80, turns: 2 },
  week: { inputTokens: 1150, outputTokens: 230, thoughtTokens: 5, cachedReadTokens: 80, turns: 3 },
  allTime: { turns: 4, inputTokens: 6150, outputTokens: 730 },
  days: [
    { day: '2026-11-27', inputTokens: 150, outputTokens: 30, turns: 2 },
    { day: '2026-11-24', inputTokens: 1000, outputTokens: 200, turns: 1 },
  ],
};
const sessions = [{ id: 1, name: 'Test', secretary: true, status: 'idle', harness: 'opencode', model: 'pag/beam-free' }];
Bun.serve({
  port,
  fetch(req, s) {
    const u = new URL(req.url);
    if (u.pathname === '/ws' && s.upgrade(req)) return;
    if (u.pathname === '/control') {
      mode = u.searchParams.get('mode') || mode;
      if (u.searchParams.get('mode') === 'hits') return Response.json({ hits });
      return Response.json({ mode });
    }
    if (u.pathname === '/opencode/usage') {
      hits.push(Date.now());
      if (mode === 'fail') return new Response('synthetic failure', { status: 500 });
      return Response.json(usage);
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
console.log(`usage-test stand on ${port}`);
