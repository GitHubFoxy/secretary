# Worker file publication (T09)

Remote Workers use the injected `secretary_artifacts.publish_artifact` MCP tool with a relative path inside their Node-managed workspace, and optionally a download filename. Publication is explicit; no workspace scanning occurs.

The Node-side MCP tool reads one regular file, rejects absolute/traversal/hidden/symlink paths and unsupported names, and enforces T08's 8 MiB limit. It transfers exact bytes into the Server Artifact Store. The capability is signed with the enrolled Node's credential and scoped to the current Worker Attempt; the Node credential itself is not included in the MCP tool's environment. The Server checks enrollment, immutable Attempt binding and active state, then stores with an idempotency key. Repeated publication of the same path and bytes returns the same Artifact; changed bytes with the same path are rejected.

When the Worker finishes, the Server attaches published references to the terminal Result and Personal Conversation entry, including Web download links at `/v1/artifacts/{id}?download=1`. Download retains T08's owner-scoped access control and checksum validation. Files persist after restart and Worker closure.

T09 does not introduce a new binary transport, mandatory Project association, child-agent or permission engine. Remote integration tests use a deterministic fake harness; live native harness and OS acceptance are separate gates.
