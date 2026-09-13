# Secretary Client boundary

`src/secretary/client.ts` is the Pi integration boundary for the Go Secretary server.

It uses the public HTTP API under `/v1/` and the conversation, Secretary-turn, and Worker activity WebSockets. Pairing exchanges the bootstrap token for a pending handoff, waits for owner approval, and redeems a separate Client credential. The bootstrap token and pending handoff are never retained by `SecretaryClient`.

The adapter owns no Secretary domain state. It keeps only presentation cursors and the selected `worker_ref`; Workers, Turns, Attempts, Results, Approvals, and reconnect decisions remain server-owned. A reconnect replays from the last sequence and reuses the selected `worker_ref`. It never calls a spawn operation.

The adapter intentionally has no Node protocol transport, Node credential field, or Node command method. `/v1/nodes` is available only as the server-owned inventory read required by the Client contract. Extensions remain local Pi presentation/client capabilities and no child Worker tree is created.

This is a TypeScript boundary because the phase-1 Pi packages cannot import the root Go module. Go integration tests use `httptest` and temporary SQLite; Pi tests use injected `fetch` and WebSocket factories. Neither suite requires real infrastructure.

## Production entrypoint

`PI_EXPERIMENTAL=1 pi secretary` is the production Secretary client path. Pass a Client credential with `--base-url` and `--credential`, or use `--bootstrap-token`, `--device-id`, and `--display-name` for owner-approved pairing. The command creates `SecretaryClientRuntime`, loads the Personal Conversation replay, subscribes to its live stream, and optionally opens a server-owned Worker observer with `--worker-ref`. `--once` prints one snapshot without keeping live sockets open. Without `--once`, an interactive terminal uses the same Pi fullscreen TUI host.

Example:

```sh
pi --experimental secretary --base-url http://127.0.0.1:8080 \
  --credential "$SECRETARY_CLIENT_CREDENTIAL" --worker-ref worker-7
```

Approval identifiers are always server `request_id` values. `respondWorker(workerRef, requestId, response)` sends that field to `/v1/workers/{worker_ref}/message`; `approve(requestId)` and `deny(requestId)` use the direct `/v1/approvals/{request_id}/{approve,deny}` endpoints. Pi never invents a second approval identifier.
