# Secretary Client boundary

`src/secretary/client.ts` is the Pi integration boundary for the Go Secretary server.

It uses the public HTTP API under `/v1/` and the conversation, Secretary-turn, and Worker activity WebSockets. Pairing exchanges the bootstrap token for a pending handoff, waits for owner approval, and redeems a separate Client credential. The bootstrap token and pending handoff are never retained by `SecretaryClient`.

The adapter owns no Secretary domain state. It keeps only presentation cursors and the selected `worker_ref`; Workers, Turns, Attempts, Results, Approvals, and reconnect decisions remain server-owned. A reconnect replays from the last sequence and reuses the selected `worker_ref`. It never calls a spawn operation.

The adapter intentionally has no Node protocol transport, Node credential field, or Node command method. `/v1/nodes` is available only as the server-owned inventory read required by the Client contract. Extensions remain local Pi presentation/client capabilities and no child Worker tree is created.

This is a TypeScript boundary because the phase-1 Pi packages cannot import the root Go module. Go integration tests use `httptest` and temporary SQLite; Pi tests use injected `fetch` and WebSocket factories. Neither suite requires real infrastructure.
