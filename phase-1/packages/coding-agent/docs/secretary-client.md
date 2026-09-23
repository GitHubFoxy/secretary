# Secretary Client boundary

`src/secretary/client.ts` is the Pi integration boundary for the Secretary server. It uses the public `/v1/` HTTP API and conversation, Secretary-turn, and Worker activity WebSockets. Pairing exchanges a bootstrap token for a pending handoff and redeems a separate Client credential.

The adapter owns no Secretary domain state. It retains only presentation cursors and the selected `worker_ref`. It never creates Workers or connects to a Node protocol.

## Stable Pi extension

Use the normal Pi TypeScript extension mechanism. Load `src/extensions/secretary.ts` directly with `pi --extension <path-to-secretary.ts>` or install it in the configured extension directory, then run `/secretary`. Set `SECRETARY_BASE_URL` to the Tailscale-reachable server URL. By default the extension reads `~/.config/secretary/viewer-credential`; override the path with `SECRETARY_CLIENT_CREDENTIAL_FILE`. The file must have mode `0600`.

The command opens a compose editor, lets the user select Secretary or an existing Worker, then sends the body directly over HTTP to Secretary. It does not call `sendUserMessage` and does not expose the body to Pi's model. Live stream data is reduced to conversation body and Worker text/status; raw event payloads, tool arguments, credentials, IDs other than selected Worker reference, and reasoning are not rendered.

`defaultPairScopes` remains read-only: `conversation:read`, `worker:read`, and `approval:read`. To use messaging, an owner must explicitly approve a freshly paired credential with `conversation:read`, `conversation:write`, `worker:read`, `worker:message`, and `approval:read`. `worker:message` authorizes only POST `/v1/workers/{worker_ref}/message`; `conversation:write` only POST `/v1/messages`. Revoke the old Client before replacing its credential. Never grant broad `worker:write` to the extension.

Tests use injected HTTP and WebSocket transports. They do not require a live Secretary server or Tailscale connection.
