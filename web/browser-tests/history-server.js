const port = Number(process.env.HISTORY_TEST_PORT || 5198);
const build = process.env.HISTORY_TEST_BUILD;
if (!build)
  throw new Error("Set HISTORY_TEST_BUILD to an isolated build directory");
let mode = "ok",
  clients = new Set(),
  requests = [],
  pending = [];
const sessions = [
  {
    id: 1,
    name: "History",
    secretary: true,
    status: "idle",
    harness: "codex",
    model: "test",
  },
  { id: 2, name: "Other", status: "idle", harness: "codex", model: "test" },
];
Bun.serve({
  port,
  fetch(req, s) {
    let u = new URL(req.url);
    if (u.pathname === "/ws" && s.upgrade(req)) return;
    if (u.pathname === "/control") {
      mode = u.searchParams.get("mode") || mode;
      if (mode === "disconnect") for (const w of clients) w.close();
      if (mode === "release") {
        for (const [w, c] of pending) reply(w, c);
        pending = [];
      }
      return Response.json(requests);
    }
    let f = Bun.file(build + (u.pathname === "/" ? "/index.html" : u.pathname));
    return f.exists().then((ok) => (ok ? new Response(f) : Response.json({})));
  },
  websocket: {
    open(w) {
      clients.add(w);
      w.send(JSON.stringify({ type: "sessions", sessions }));
    },
    close(w) {
      clients.delete(w);
    },
    message(w, data) {
      let c = JSON.parse(data);
      if (c.type !== "read") {
        w.send(
          JSON.stringify({ type: "reply", requestId: c.requestId, text: "" }),
        );
        return;
      }
      requests.push(c.before ?? null);
      if (mode === "hold" && c.before) {
        pending.push([w, c]);
        return;
      }
      if (mode === "fail" && c.before) {
        w.send(
          JSON.stringify({
            type: "error",
            requestId: c.requestId,
            message: "synthetic failure",
          }),
        );
        return;
      }
      reply(w, c);
    },
  },
});
function reply(w, c) {
  if (c.id === 2) {
    w.send(
      JSON.stringify({
        type: "reply",
        requestId: c.requestId,
        messages: [
          { kind: "user", text: "other session", cursor: 100, images: [] },
        ],
        hasMore: false,
        queue: [],
      }),
    );
    return;
  }
  let end = c.before ? c.before - 1 : 60,
    start = Math.max(1, end - 19);
  let messages = Array.from({ length: end - start + 1 }, (_, i) => ({
    kind:
      mode.startsWith("group") && i >= Number(mode.slice(5) || 0)
        ? "thinking"
        : "user",
    text: `synthetic ${start + i}`,
    cursor: start + i,
    images: [],
  }));
  w.send(
    JSON.stringify({
      type: "reply",
      requestId: c.requestId,
      messages,
      hasMore: start > 1,
      queue: [],
    }),
  );
}
