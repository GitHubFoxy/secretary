# Install 03 — native Codex CLI + ACP provisioning

## Selection and ownership

`secretary setup --interactive` offers Codex on fresh supported macOS/Linux. `secretary setup --harness codex` selects it explicitly. Provisioning uses the packaged Go `secretary-node` binary; it requires no Node.js, npm, global npm packages, Python or Go compiler. Supported release platforms: macOS arm64/amd64 and Linux arm64/amd64 (Linux ACP artifact requires glibc). Codex CLI is pinned to `0.163.0`. On macOS/Linux the maintained adapter is `@agentclientprotocol/codex-acp 2.2.2`, pinned to Git commit `202e66e63343ec4f5a5e35ab590251d813d129ca` and built into a standalone executable in release CI using pinned Bun `1.3.6` and `npm ci` with the upstream lockfile. The checked Secretary release includes `bin/native-acp/codex-acp.tar.gz` and a SHA-256 receipt; end users do not need Bun, Node.js or npm. The generated `ReasoningEffort` type is a string, accepting newer values such as `max` and `ultra`. **Windows still uses the older ACP 0.16.0 adapter and is not covered by this migration; do not mark Windows live Codex support ready.** A successful archive checksum alone is not compatibility proof.

**ACP CLI version probe:** The legacy `zed-industries/codex-acp v0.16.0` program does not support `--version` (its CLI parser reports an unexpected argument, exit 2). Secretary must not require that flag. For this precise CLI usage failure it instead checks `--help` and records the version from the SHA-256-pinned managed release archive. An external adapter that cannot report its version is recorded as **version unavailable**, never assigned a fabricated number. Executable SHA-256 checks still apply on every inventory read, and authenticated ACP `initialize/session/new` capability probes remain required before a pair is marked ready; `--help` is not protocol readiness evidence. Other command failures, changed bytes, and failure of `--help` remain fatal.

Each host stores a Server inventory and an Execution node inventory independently at `~/.local/share/secretary/native/{server,node}/codex-pair.json`. Remote Nodes use their own host-local inventory, not Server paths. Each record contains harness kind, effective absolute executable paths, symlink-resolved paths, observed versions, SHA-256, managed/external provenance and ACP protocol validation state. Private native credential/config/history locations remain owned by Codex, not Secretary. Existing Worker bindings and session IDs are not rewritten.

## Native authorization and readiness

Native `codex login status` distinguishes the owner authorization prerequisite. On a new unauthenticated host, noninteractive setup can stage the binaries and prints the selected `codex login` instruction; runtime remains unavailable until native authorization and ACP protocol validation. Authorized setup and `secretary doctor` perform native ACP `initialize`, `session/new`, and `session/load`, checking protocol version, loadSession, steering negotiation and model availability before recording the validated pair. The release CI additionally runs a smoke test against the **actual packaged** adapter binary with a pinned Codex CLI and a no-inference sentinel credential. An idle-session steering request proves the extension handler is wired but does not prove active-turn steering; real authorized active-turn steering remains a separate live acceptance gate. `secretary node doctor` independently validates and records the Node pair. Restart running Server/Node processes after authorizing a previously unready pair. Missing native authorization or exhausted provider quota is not an installation error Secretary can repair. No custom login, provider routing or alternate-harness fallback is implemented.

`secretary serve`, `secretary node serve`, ordinary process starts and packaged native Execution node startup validate the selected inventory rather than silently following PATH. A damaged inventory makes Codex unavailable, not Claude unavailable. Explicit Claude-only setup never provisions Codex.

## Inspect, adopt, upgrade and rollback

The following commands are run on the respective Server/Node host with its packaged `secretary-node`. Run them only when the owner intends that operation.

```sh
secretary-node --codex-pair-action=install --codex-pair-scope=server
secretary-node --codex-pair-action=install --codex-pair-scope=node
secretary-node --codex-pair-action=env --codex-pair-scope=server
secretary-node --codex-pair-action=show --codex-pair-scope=node
secretary-node --codex-pair-action=doctor --codex-pair-scope=node
secretary-node --codex-pair-action=readiness --codex-pair-scope=node
secretary-node --codex-pair-action=install --codex-pair-scope=node --codex-pair-cli=/absolute/path/to/codex --codex-pair-acp=/absolute/path/to/codex-acp
secretary-node --codex-pair-action=upgrade --codex-pair-scope=node --codex-pair-cli=/absolute/path/to/new-codex --codex-pair-acp=/absolute/path/to/new-codex-acp
secretary-node --codex-pair-action=rollback --codex-pair-scope=node
```

Explicitly selecting an external pair does not mutate it, its global configuration or credentials. Selecting one binary without the other is rejected. A selected pair remains pinned on repeat setup even when PATH changes. A moved binary, replaced bytes, unsupported host, partial managed release, interrupted download or failed native protocol preflight stops activation with a visible diagnostic. Managed releases retain their SHA-256-pinned upstream archives and re-extract both executables for byte comparison on repeated setup, so unchanged version strings cannot conceal replacement. Explicit upgrades journal previous and candidate inventories before activation; recovery reconciles an interrupted switch and updates the rollback record only after the candidate is active. Native login, quota/provider, executable, and ACP protocol failures are classified separately.

Upgrade checks the proposed pair with the actual owner authorization and ACP transport before atomically replacing the inventory. The previous record is kept under `codex-pair.previous.json` for explicit rollback. Old managed binary directories are retained. Running native Workers are not restarted or migrated as an upgrade side effect. If a provider/harness is unavailable, resolve authorization or binary compatibility before retrying.

## Deterministic verification commands (not executed)

```sh
go test ./internal/node ./cmd/secretary-node ./cmd/secretaryd ./internal/config -count=1
bash -n scripts/install-linux-binary.sh scripts/install-macos-binary.sh scripts/package-linux-binary-release.sh scripts/package-macos-binary-release.sh
bash scripts/macos-release-contract-test.sh
./scripts/phase4-release-gate.sh
```

Native Go tests cover external adoption, repeat setup, same-version tampering of CLI and ACP, moved paths, interrupted upgrade journal reconciliation, rollback preservation, actual ACP initialize/session/new wire responses and rejection of invalid candidates, SHA-256 comparison and incomplete download. The Phase 5 setup fixture runs the actual Go provisioner and independently checks Server/Node inventories. Binary installer release fixtures and Go integration tests must be checked by CI. Tests must not include actual native credentials.

## Manual live acceptance (not executed)

Perform a clean install on macOS arm64, macOS amd64, Linux arm64 and Linux amd64, without Node/npm/Python. Choose Codex, verify native binary provenance for both Server and Node, run the exact selected executable's `login status`, authorize with native `codex login`, and run `secretary doctor` and `secretary node doctor`. Capture each executable's absolute path, SHA-256 and `--version`; capture selected versions and protocol handshake separately. Under that authorized account, start real Server and Execution node, pair Node, create a Task via Web/Telegram, verify native Worker Result exactly once, then send Follow-up to the same Worker and capture the next Result. Verify restart/resume, unsupported steering, native quota/auth error separation, moved/changed binaries, conflicting global PATH, simulated interrupted download, repeated setup, manual upgrade, rollback and Claude-only setup.

This documentation is not live evidence: authorized Task/Result, subscription/quota and clean-host acceptance remain manual gates of #89 and this ticket. Do not report a successful live run without the actual host, revision, binary versions and Result record.


## Concurrency, native provider errors, and diagnostics

On macOS/Linux, each Server or Node inventory operation takes a nonblocking exclusive advisory file lock for its scope before reading or changing the selected pair, pending journal, and last-good rollback. Another concurrent installer or upgrader for the same scope fails without writing the inventory; separate scopes can proceed independently. The lock file persists after exit; the OS releases the lock on process termination. Interrupted journals are reconciled under this lock before further operations.

The real ACP initialize/session-new/session-load probe differentiates provider authentication, quota/rate limits, upstream unavailability, and protocol/transport errors. A failed session/new carries its stage, timeout and bounded schema error from the adapter stderr (for example `failed to load models cache: unknown variant max`), without leaking arbitrary logs or credentials. An inherited `SECRETARY_CODEX_PAIR_CANDIDATE` does not bypass inventory enforcement in a normal Server or Node process. Only the explicit `secretary-node --check-codex-readiness` diagnostic may probe a proposed pair before inventory activation.


## MAC-01: Codex Secretary Worker lifecycle acceptance

Source: [alpha.11 Mac acceptance, MAC-01](https://github.com/GitHubFoxy/secretary/blob/c7ed3c6f9c0bd4183cb4496aefa12081f9b0214e/.scratch/phase-5/reports/alpha11-live-acceptance-20261022.md). The published alpha.11 completed two Worker-creation requests with a successful native final answer but no canonical Worker, Attempt, or Result. Its native `spawn_agent` calls are not Secretary `spawn_worker` calls. Neither that observation nor empty `secretary_runtime_launches` establishes why MCP tools were not used.

For a Codex Secretary, the managed profile now defaults to `addressed-reply-v1`. Ordinary assistant final prose cannot independently complete a successful Secretary turn: the server must accept an origin-addressed `reply_to_user` call. The Secretary MCP `spawn_worker` call owns Worker creation, Node selection, dispatch, and idempotency; the user-facing creation confirmation carries `action_kind=spawn_worker` and the returned `action_turn_id` as `worker_turn_id`. The server checks the durable Worker creation origin against the active Secretary turn before accepting that confirmation. Native `spawn_agent` produces no such origin. Direct conversational answers still use `reply_to_user`, and Claude's existing reply mode is unchanged.

The installed managed Codex pair already stages the pinned `codex-code-mode-host` upstream asset, introduced separately from the original alpha.9 finding in [#173](https://github.com/GitHubFoxy/secretary/issues/173). This change additionally fails Codex managed readiness when the host executable is missing or unsafe, rather than treating ACP initialize/model availability as proof of working MCP execution. It does not prove the cause of the alpha.11 report or satisfy #173's independent published-candidate acceptance. The Codex 0.163.0 and ACP 2.2.2 pins remain unchanged.

Deterministic regression tests, intended for GitHub Actions on the PR (not executed in Chat mode):

```sh
go test ./internal/config ./internal/node ./internal/mcp ./internal/secretary ./internal/ctl -count=1
go test ./... 
```

Automated assertions cover the default Codex reply contract, addressed MCP transport into ACP, accepted and rejected typed Worker creation acknowledgements, projectless remote-node dispatch with one canonical Worker/Attempt under idempotent replay, and missing managed MCP host readiness. Existing projectless Claude and Secretary conversational tests remain in the Go suite. ACP fixtures do not demonstrate a real Codex model selecting the MCP tool.

MANUAL, not passed: publish and clean-install a new candidate on macOS arm64; confirm the actual managed CLI/ACP/code-mode-host pair and doctor diagnostics; use authorized Codex Secretary to create exactly one projectless Worker on an independently enrolled Execution Node; observe its question Result, send an answer to the same Worker, and verify exactly one subsequent Result, stable Worker binding and native session/workspace, then restart Server/Node and verify continuity without duplicate dispatch. Record server/Node identities and persisted Worker/Attempt/Result counts. Also repeat simple Secretary conversation and projectless Claude regression, and perform separate Linux acceptance for #173. Do not label fixtures or build-only CI as live PASS.


### MAC-01 review follow-up: readiness and confirmation boundaries

The addressed-reply-v1 MCP schema requires `reply_kind=answer`, `reply_kind=error`, or `reply_kind=spawn_worker` for each user-facing `reply_to_user` call, alongside the server-issued Secretary turn and input identities. For `spawn_worker`, the server requires `action_kind=spawn_worker`, the exact `worker_turn_id` returned by the canonical spawn operation, and a durable accepted queued-creation or delivered dispatch origin bound to that Secretary turn and input. The server generates the successful creation acknowledgement after validating this binding; absent or fabricated action proof is rejected regardless of the owner's wording. Independent answers and genuine error reports use distinct reply kinds without requiring an accepted spawn; these kinds do not carry Worker action proof. The contract does not semantically verify arbitrary prose within answers or errors.

The managed host preflight verifies that the selected file is a native executable with a recognized platform header, safe type and execute permissions; ACP readiness still checks the selected adapter. These checks cannot prove that the native Codex code-mode host successfully discovers the Secretary MCP catalogue or executes tools in an authenticated model turn. At runtime, `secretary_runtime_launches` / `secretary_turn_launches` and `SecretaryMCPDiscovery` expose launch, initialize, tools-list and `has_spawn_worker` observations when available. Missing observations alone do not prove missing tools; a Codex addressed turn without a durable `reply_to_user` ends failed and now emits an explicit, non-successful owner-facing diagnostic. This is separate from affirmative live MCP discovery and requires a published-candidate test. Previously installed external Secretary profiles can differ from the embedded version; verify both upgraded existing installations and clean setups.
