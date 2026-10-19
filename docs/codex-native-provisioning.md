# Install 03 — native Codex CLI and ACP pair

This PR provisions upstream Codex CLI 0.163.0 and codex-acp 0.16.0 from version-pinned native release archives, with SHA-256 comparison before extraction. Versions are release-selection pins, not evidence of live compatibility. Runtime capability is determined by the existing native ACP probe (initialize, session/new, loadSession, steering, model catalog), without a model call.

## Scope and state

The Server and each Execution node must own independent pinned inventories. The current implementation uses `~/.local/share/secretary/native/{server,node}/codex-pair.json` for the local Server and local Node. Separately installed remote Nodes resolve their own host-local `node` inventory. The selected record contains absolute paths, real paths, observed versions, SHA-256, managed/external provenance, selected harness and protocol validation status. CLI/Node processes verify the record before activation, including symlink targets and digest. Missing, moved or altered executables fail closed. Native authentication/history/config are outside managed directories and not modified.

`secretary setup --harness codex` provisions a Server and Node pair, runs native ACP readiness under the owner's existing Codex authorization and records successful protocol validation. `secretary doctor`, `secretary node doctor`, `secretary serve` and `secretary node serve` use those selected paths. Native owner authorization uses `codex login` and `codex login status`. Native provider/quota issues are not repaired by Secretary. Claude-only setup does not call the Codex provisioner, and failed explicit Codex selection does not fall back to Claude.

## Explicit native inventory operations

```sh
python3 scripts/provision-codex-pair.py install --scope server
python3 scripts/provision-codex-pair.py install --scope node
python3 scripts/provision-codex-pair.py show --scope server
python3 scripts/provision-codex-pair.py doctor --scope server
python3 scripts/provision-codex-pair.py readiness --scope server --readiness-bin /absolute/path/to/secretary-node
python3 scripts/provision-codex-pair.py install --scope node --codex /absolute/path/to/codex --acp /absolute/path/to/codex-acp
python3 scripts/provision-codex-pair.py install --scope node --upgrade --codex /absolute/path/to/new-codex --acp /absolute/path/to/new-codex-acp --readiness-bin /absolute/path/to/secretary-node
python3 scripts/provision-codex-pair.py rollback --scope node --readiness-bin /absolute/path/to/secretary-node
```

A half-selected external pair is rejected. A repeated installation keeps the selected pair. Upgrade validates the proposed native protocol before atomic inventory replacement; the previous inventory is retained for explicit rollback. Existing native Codex credentials and session state are not moved or rewritten. Updating the selected inventory does not relocate already-running Worker bindings.

## Checks to run (not executed for this PR)

```sh
python3 -m unittest discover -s scripts -p 'test-provision-codex-pair.py' -v
go test ./internal/node ./cmd/secretary-node ./cmd/secretaryd ./internal/config
bash -n scripts/install-linux-binary.sh scripts/install-macos-binary.sh scripts/package-linux-binary-release.sh scripts/package-macos-binary-release.sh
bash scripts/macos-release-contract-test.sh
./scripts/phase4-release-gate.sh
```

## Real manual native acceptance (not performed)

On a fresh supported macOS arm64, macOS x86_64, Linux aarch64 and Linux x86_64 account without Node/npm, install Secretary and select Codex. Record `codex --version`, `codex-acp --version`, exact real paths and SHA-256 for both independently for Server and each remote/local Execution node. Record the OS version and libc on Linux. Run the recorded binary's `login status`; if unauthenticated, have the owner run its native `login`. Then run `secretary doctor` and `secretary node doctor`. Use the real Web conversation to dispatch Task → native Worker → terminal Result; send Follow-up to the same Worker and capture a second Result. Also verify session resume, no duplicate Result, quota/auth separation, service restart, pairing, version mismatch, moved binary, interrupted artifact download, reinstallation, explicit upgrade and rollback, and Claude-only install. Keep live evidence separate from deterministic fixtures.

## Known limitation requiring follow-up verification

No authorized Codex Task/Result was executed as part of this implementation. macOS clean-host availability of Python 3, script installation through binary packages, managed upgrade recovery and the macOS native Node.app environment must be validated separately; do not claim a ready-to-use no-prerequisite installation without those checks.
