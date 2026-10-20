# Install 03 — native Codex CLI + ACP provisioning

## Selection and ownership

`secretary setup --interactive` offers Codex on fresh supported macOS/Linux. `secretary setup --harness codex` selects it explicitly. Provisioning uses the packaged Go `secretary-node` binary; it requires no Node.js, npm, global npm packages, Python or Go compiler. Supported release platforms: macOS arm64/amd64 and Linux arm64/amd64 (Linux ACP artifact requires glibc). Codex CLI is pinned to `0.163.0` and `codex-acp` to `0.16.0`, each with hard-coded upstream release URL and SHA-256. Successful archive checksum alone is not compatibility proof.

**ACP CLI version probe:** The pinned `zed-industries/codex-acp v0.16.0` program does not support `--version` (its CLI parser reports an unexpected argument, exit 2). Secretary must not require that flag. For this precise CLI usage failure it instead checks `--help` and records the version from the SHA-256-pinned managed release archive. An external adapter that cannot report its version is recorded as **version unavailable**, never assigned a fabricated number. Executable SHA-256 checks still apply on every inventory read, and authenticated ACP `initialize/session/new` capability probes remain required before a pair is marked ready; `--help` is not protocol readiness evidence. Other command failures, changed bytes, and failure of `--help` remain fatal.

Each host stores a Server inventory and an Execution node inventory independently at `~/.local/share/secretary/native/{server,node}/codex-pair.json`. Remote Nodes use their own host-local inventory, not Server paths. Each record contains harness kind, effective absolute executable paths, symlink-resolved paths, observed versions, SHA-256, managed/external provenance and ACP protocol validation state. Private native credential/config/history locations remain owned by Codex, not Secretary. Existing Worker bindings and session IDs are not rewritten.

## Native authorization and readiness

Native `codex login status` distinguishes the owner authorization prerequisite. On a new unauthenticated host, noninteractive setup can stage the binaries and prints the selected `codex login` instruction; runtime remains unavailable until native authorization and ACP protocol validation. Authorized setup and `secretary doctor` perform native ACP `initialize` and `session/new`, checking protocol version, loadSession, steering and model availability, then atomically record the validated pair. `secretary node doctor` independently validates and records the Node pair. Restart running Server/Node processes after authorizing a previously unready pair. Missing native authorization or exhausted provider quota is not an installation error Secretary can repair. No custom login, provider routing or alternate-harness fallback is implemented.

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

The real ACP initialize and session/new probe differentiates provider authentication, quota/rate limits, upstream unavailability, and protocol/transport errors. An inherited `SECRETARY_CODEX_PAIR_CANDIDATE` does not bypass inventory enforcement in a normal Server or Node process. Only the explicit `secretary-node --check-codex-readiness` diagnostic may probe a proposed pair before inventory activation.
