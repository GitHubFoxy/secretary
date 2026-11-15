# Worker file publication (T09)

A Worker calls the injected `secretary_artifacts.publish_artifact` MCP tool with a named path **relative to its assigned workspace**. This is explicit; no auto-scan of Worker directories occurs. Remote Workers and the local Node use the same protocol.

## Trust boundary

The harness and its MCP child see only `SECRETARY_ARTIFACT_BROKER_URL`, a random, Attempt-scoped **loopback-only** Node endpoint. They **never receive** the enrolled Node credential or an HTTP-usable Server publication token. The broker accepts **only JSON path and optional filename**, not binary uploads; the Node process independently opens that path through pinned directory descriptors with no-follow traversal, rejects escaping, hidden and symlink paths and nonregular/unsupported/oversized files, and reads a bounded file. It then sends the bytes to the existing T08 Server Artifact Store, signing the exact path/name/content SHA-256 with the Node-held credential. The Server validates Node enrollment and revocation, the active immutable Worker/Attempt binding, the Node-specific provenance signature, the content digest and idempotency before persisting.

A Worker able to invoke the local broker directly can request only the same validated workspace file operation. Knowing the broker URL (or an old, previously exposed HMAC publication capability without Node-held provenance proof) cannot turn the Server endpoint into a raw arbitrary-byte upload. Attempt registrations are removed after terminal Result; broker shutdown follows Daemon shutdown.

## Result and Web

The Server persists each file under the owner-scoped T08 Artifact ID, attached to its Attempt. It merges ArtifactRefs into the durable terminal Result, and includes a clickable `/v1/artifacts/{id}?download=1` link for each stored owner-owned ArtifactRef **even if the Worker already mentioned `artifact://{id}`**. Duplicate URLs for the same Artifact ID are suppressed. The Web Markdown view renders the links. Authenticated download returns the original bytes and filename, with no access for anonymous clients. Artifacts and Results survive Worker closure and process restarts.

The T09 test exercises remote fake-harness → Node broker → Server → Web, bypass attempts, bytes, idempotency, owner-only download and restart. Live-native and cross-platform acceptance are separate verification gates.

## Native Worker execution (all platforms)

**Secretary Workers execute directly with the operating-system permissions and environment of the Execution Node user. Secretary does not provide an additional Worker sandbox or privilege boundary.**

Workers, harnesses, shells and MCP descendants have ordinary access to host files, processes and environment permitted to that same user. They inherit `CODEX_PATH`, `SECRETARY_ACP_COMMAND` and normal variables. Native Codex and Claude Code retain their own built-in permission behavior; Secretary adds no Seatbelt profiles, Bubblewrap namespaces, mount allowlists, privileged CLI manifests, separate Windows Worker SID or sandbox-specific environment allowlist.

This includes the ability to read and modify Node/Server state whenever it is accessible to the Execution Node user. Artifact publication capabilities, Node pairing, Server API authorization, session ownership and integrity checks remain separate protocol controls—not an OS privilege boundary.

Artifact publication still uses pinned-handle, no-follow traversal of workspace-relative paths: Unix `openat(O_NOFOLLOW)` and Windows `NtCreateFile` with reparse-point refusal. The broker accepts named paths rather than arbitrary caller-supplied bytes and the Server validates active Attempt identity and Node provenance.

### Legacy Windows Worker account cleanup (manual only)

Existing installations may retain `worker-account.json`, a local `secretary_worker`-style Windows account, restricted NTFS ACLs and native provider state stored inside that account's home. These are **not** removed or migrated automatically. The old state may contain the only copy of previous Codex/Claude/OpenCode sessions; preserving the Node's JSON/SQLite files alone does not make those provider sessions immediately accessible to a different Windows user.

Before cleanup, back up Node identity/config/SQLite and both current-user and legacy Worker provider directories. Inspect the saved account record, local account and permissions (`Get-Content <NodeDataDir>\\worker-account.json | ConvertFrom-Json`, `Get-LocalUser`, `icacls <path>`). If a Follow-up needs sessions in the legacy profile, copy the relevant provider state to the current Node user's provider home **only after** verifying ownership and permissions. Review and correct any old ACLs that prevent the current Node user from using existing workspaces or sessions. Removing the old account or its files is an explicit operator action, potentially requiring administrator privileges; do so only after verifying successful native session continuity and a recoverable backup. The new installer itself never requests UAC for Worker provisioning.

## Native state and host acceptance

Codex, Claude Code and OpenCode continue to use their persistent provider stores directly. CI verifies direct process launch, Worker identity/environment, access outside workspace, cancellation and native checkpoint persistence. Linux T09 uses the real pinned Codex CLI with a local deterministic Responses-compatible backend to create and resume the same native thread; this is **not** authenticated provider or published-release acceptance.

**Windows:** Run `scripts/install-windows-node.ps1` as the Node owner with a verified Node/tray executable pair, or `scripts/install-windows-node-release.ps1` with a separately trusted archive SHA-256. No UAC or account provisioning is needed for Worker execution. Old `worker-account.json`, previously created Windows Worker accounts and ACLs are intentionally **not** removed automatically. Back up data, review these leftovers and clean up manually if desired. Previously restrictive ACLs may need manual adjustments to allow the Node user access to native state. Node identity, configuration and native checkpoints remain preserved on upgrade.

**Linux/macOS:** No root provisioning or sandbox prerequisite. Native Codex/ACP runtime verification, release digest validation and session persistence remain required; global npm is not a supported runtime prerequisite for managed Codex. Do not close #174 until a published candidate completes Worker ACP `initialize`, real Attempt→Result and native Follow-up; #170 remains a separate Public v1 gate.
