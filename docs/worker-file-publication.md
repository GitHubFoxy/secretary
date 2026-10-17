# Worker file publication (T09)

A Worker calls the injected `secretary_artifacts.publish_artifact` MCP tool with a named path **relative to its assigned workspace**. This is explicit; no auto-scan of Worker directories occurs. Remote Workers and the local Node use the same protocol.

## Trust boundary

The harness and its MCP child see only `SECRETARY_ARTIFACT_BROKER_URL`, a random, Attempt-scoped **loopback-only** Node endpoint. They **never receive** the enrolled Node credential or an HTTP-usable Server publication token. The broker accepts **only JSON path and optional filename**, not binary uploads; the Node process independently opens that path through pinned directory descriptors with no-follow traversal, rejects escaping, hidden and symlink paths and nonregular/unsupported/oversized files, and reads a bounded file. It then sends the bytes to the existing T08 Server Artifact Store, signing the exact path/name/content SHA-256 with the Node-held credential. The Server validates Node enrollment and revocation, the active immutable Worker/Attempt binding, the Node-specific provenance signature, the content digest and idempotency before persisting.

A Worker able to invoke the local broker directly can request only the same validated workspace file operation. Knowing the broker URL (or an old, previously exposed HMAC publication capability without Node-held provenance proof) cannot turn the Server endpoint into a raw arbitrary-byte upload. Attempt registrations are removed after terminal Result; broker shutdown follows Daemon shutdown.

## Result and Web

The Server persists each file under the owner-scoped T08 Artifact ID, attached to its Attempt. It merges ArtifactRefs into the durable terminal Result, and includes a clickable `/v1/artifacts/{id}?download=1` link for each stored owner-owned ArtifactRef **even if the Worker already mentioned `artifact://{id}`**. Duplicate URLs for the same Artifact ID are suppressed. The Web Markdown view renders the links. Authenticated download returns the original bytes and filename, with no access for anonymous clients. Artifacts and Results survive Worker closure and process restarts.

The T09 test exercises remote fake-harness → Node broker → Server → Web, bypass attempts, bytes, idempotency, owner-only download and restart. Live-native and cross-platform acceptance are separate verification gates.

## Native Worker platform security

All native Worker subprocesses and their shell/MCP descendants must be isolated from both Node credentials and co-located Secretary Server SQLite (which holds both encrypted node credentials and their decrypting transport secret). An environment-only token boundary is insufficient.

**Linux:** The Node requires root-owned bubblewrap and functioning user/PID/mount namespaces. An empty tmpfs root is created and only read-only system binaries/libraries/config, a verified Worker executable, the selected workspace and explicitly chosen native provider state are mounted. The Server database and its WAL/SHM files, Node state, arbitrary home files, and host process table remain inaccessible even under the same outer UID. The sandbox fails closed when unavailable. Codex and Claude each retain their own native state and OpenCode retains its selected DataHome. An E2E co-located Server/Node fixture creates a real transport key and registered credential and confirms the Worker cannot read these files.

**macOS:** Seatbelt sandbox-exec applies a default-deny filesystem profile to the Worker process and descendants while permitting the assigned workspace and provider stores, and excluding Server/Node secrets. Seatbelt must be supported by the installed macOS runtime; it fails closed if unavailable.

**Windows:** Native Workers use a separate, non-admin Windows OS account via LogonUserW and CreateProcessAsUser. Configure SECRETARY_WORKER_USERNAME, SECRETARY_WORKER_PASSWORD, SECRETARY_WORKER_DOMAIN and SECRETARY_WORKER_HOME in the trusted Node service. The Worker account must have its own persistent home/auth state and workspace access, and NTFS ACLs must deny reading Node identity/state and Server private data. The Node refuses insecure same-SID execution and validates secret file access using the Worker SID. Windows native state is stored under that account's home. The Windows acceptance workflow provisions a temporary account to test this boundary.

Source publishing uses pinned-handle no-follow traversal on each path component: Unix openat(O_NOFOLLOW), and Windows NtCreateFile with reparse-point refusal. The worker broker accepts paths, never caller-controlled bytes. Server publication verifies Node provenance and exact bytes.

Download-link deduplication requires a real Markdown link to the exact URL, not a bare URL, artifact:// mention, escaped bracket, Markdown image or code snippet.

**Acceptance limits:** scripted native-process state tests exercise Start / process exit / separate-process Resume, but live Codex/Claude/OpenCode model interactions and real-world installations on each supported OS remain unverified until independent acceptance.

## Re-review follow-up: host preparation and resume

- **macOS:** Native providers use the real canonical HOME and must persist checkpoints across separate shell processes. Codex, Claude and OpenCode are verified without treating `Operation not permitted` as an acceptable skip. Broad host-wide /tmp grants were removed; the Worker uses a private workspace TMPDIR. System-directory overrides are denied at the Node state subtree.
- **Linux:** The Node's arbitrary protected state dir is masked *after* all system/CLI binds, including data dirs underneath /usr and /etc. Global Node CLI installations resolve their package's node_modules dependency tree rather than bind a single executable; regression starts a user-installed fake native CLI and reads a sibling runtime asset. Protected files remain hidden.
- **Windows:** Existing provider credentials and session state in the Node user's .codex/.claude/.claude.json and selected OpenCode native DataHome are migrated into the isolated Worker account profile, preserving previously generated checkpoints. A destination directory that already exists but is empty is populated without overwriting newer files. No unsandboxed native-worker fallback is permitted. The native child runs under a separate SID and is assigned to a Windows Job Object with KILL_ON_JOB_CLOSE, so CommandContext cancellation also terminates the child PID/tree.
- **Windows one-time provisioning:** An administrator runs `scripts/setup-windows-worker.ps1 -NodeDataDir <Node directory> -Workspace <Worker workspace>` once. The script creates a non-admin Worker account, ACL-isolates Node credentials, and saves its account configuration as `worker-account.json` in the private Node data directory. Subsequent native Dispatch/Resume loads the protected config automatically without SECRETARY_WORKER_* environment overrides. Verify using `secretary-node --data-dir <Node directory> --check-worker-sandbox`. Hosts unable to provision an isolated SID must reject unsafe native Worker execution; existing Node state is never wiped.
- **Download links:** Every owned Artifact ID receives its own canonical owner-download link, irrespective of Markdown text, indented code, multi-backtick spans, escaped brackets or image references. Existing T08 publication and T10 image/T14 voice routes remain integrated.
