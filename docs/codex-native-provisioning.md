# Codex native provisioning — Install 03 (#125)

Status: **foundation only; integration and live acceptance pending**. This document does not assert successful platform acceptance.

The scoped inventory prototype lives in `scripts/provision-codex-pair.py`. It supports macOS arm64/x86_64 and Linux arm64/x86_64, without Node.js, npm, or a global package-manager installation. Selected versions are Codex CLI 0.163.0 and codex-acp 0.16.0, downloaded as version-pinned upstream native archives with recorded SHA-256. This is not yet an established compatible version pair; native ACP handshake validation must be integrated before activation.

Inventory commands:

```sh
python3 scripts/provision-codex-pair.py install --scope server
python3 scripts/provision-codex-pair.py install --scope node
python3 scripts/provision-codex-pair.py doctor --scope server
python3 scripts/provision-codex-pair.py doctor --scope node
python3 scripts/provision-codex-pair.py show --scope node
```

External adoption requires an explicit matching pair:

```sh
python3 scripts/provision-codex-pair.py install --scope node --codex /absolute/path/to/codex --acp /absolute/path/to/codex-acp
```

The inventory is scoped separately under `~/.local/share/secretary/native/server` and `~/.local/share/secretary/native/node`; it records absolute and resolved executable paths, observed versions, SHA-256 and provenance. It does not store native credentials. Running `codex login` is always owner-driven. **The existing Secretary runtime does not yet consume this inventory.** It must not be interpreted as guaranteeing doctor/install/upgrade/readiness/runtime pair coherence.

Deterministic checks to run in CI:

```sh
python3 -m unittest discover -s scripts -p 'test-provision-codex-pair.py' -v
go test ./internal/node ./internal/config
bash -n scripts/install-linux-binary.sh scripts/install-macos-binary.sh
```

Manual native checks still required, each recorded with exact binary versions and artifact SHA:

1. Clean macOS arm64, macOS amd64, Linux arm64 and Linux amd64 user without Node/npm: install pair into Server scope and separately into Node scope; verify paths and native executable versions.
2. Run the selected `codex login`, `codex login status`, and the selected `codex-acp` via ACP initialize and session/new; verify protocol, loadSession and steering.
3. Start Secretary using the selected Server pair, pair the selected Execution node with its independent pair, dispatch a real Task, observe native session and exactly one Result; then Follow-up and Result.
4. Check moved/replaced executables, mismatched versions, failed downloads, repeated setup, upgrades, rollback, Claude-only route, offline/expired native authorization and quota errors.
