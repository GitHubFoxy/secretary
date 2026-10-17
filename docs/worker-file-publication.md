# Worker file publication (T09)

A Worker calls the injected `secretary_artifacts.publish_artifact` MCP tool with a named path **relative to its assigned workspace**. This is explicit; no auto-scan of Worker directories occurs. Remote Workers and the local Node use the same protocol.

## Trust boundary

The harness and its MCP child see only `SECRETARY_ARTIFACT_BROKER_URL`, a random, Attempt-scoped **loopback-only** Node endpoint. They **never receive** the enrolled Node credential or an HTTP-usable Server publication token. The broker accepts **only JSON path and optional filename**, not binary uploads; the Node process independently opens that path via `os.OpenRoot`, rejects escaping, hidden and symlink paths and nonregular/unsupported/oversized files, and reads a bounded file. It then sends the bytes to the existing T08 Server Artifact Store, signing the exact path/name/content SHA-256 with the Node-held credential. The Server validates Node enrollment and revocation, the active immutable Worker/Attempt binding, the Node-specific provenance signature, the content digest and idempotency before persisting.

A Worker able to invoke the local broker directly can request only the same validated workspace file operation. Knowing the broker URL (or an old, previously exposed HMAC publication capability without Node-held provenance proof) cannot turn the Server endpoint into a raw arbitrary-byte upload. Attempt registrations are removed after terminal Result; broker shutdown follows Daemon shutdown.

## Result and Web

The Server persists each file under the owner-scoped T08 Artifact ID, attached to its Attempt. It merges ArtifactRefs into the durable terminal Result, and includes a clickable `/v1/artifacts/{id}?download=1` link for each stored owner-owned ArtifactRef **even if the Worker already mentioned `artifact://{id}`**. Duplicate URLs for the same Artifact ID are suppressed. The Web Markdown view renders the links. Authenticated download returns the original bytes and filename, with no access for anonymous clients. Artifacts and Results survive Worker closure and process restarts.

The T09 test exercises remote fake-harness → Node broker → Server → Web, bypass attempts, bytes, idempotency, owner-only download and restart. Live-native and cross-platform acceptance are separate verification gates.
