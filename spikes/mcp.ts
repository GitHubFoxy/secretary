// Minimal stateless MCP server (streamable HTTP, JSON responses) with a fake `delegate` tool.
export function startMcp(port: number, log: (s: string) => void) {
  return Bun.serve({
    port,
    async fetch(req) {
      if (req.method !== "POST") return new Response(null, { status: 405 });
      const msg = await req.json();
      if (msg.id === undefined) return new Response(null, { status: 202 }); // notification
      const ok = (result: unknown) => Response.json({ jsonrpc: "2.0", id: msg.id, result });
      switch (msg.method) {
        case "initialize":
          return ok({ protocolVersion: msg.params.protocolVersion, capabilities: { tools: {} }, serverInfo: { name: "secretary", version: "0" } });
        case "tools/list":
          return ok({ tools: [{ name: "delegate", description: "Delegate a task to a background agent session. Returns immediately.", inputSchema: { type: "object", properties: { harness: { type: "string" }, folder: { type: "string" }, task: { type: "string" } }, required: ["harness", "task"] } }] });
        case "tools/call":
          log(`MCP CALL ${msg.params.name} ${JSON.stringify(msg.params.arguments)}`);
          return ok({ content: [{ type: "text", text: JSON.stringify({ id: 7, status: "running" }) }] });
        default:
          return Response.json({ jsonrpc: "2.0", id: msg.id, error: { code: -32601, message: "no method" } });
      }
    },
  });
}
