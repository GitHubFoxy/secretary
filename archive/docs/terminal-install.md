# Terminal installation and configuration (T06)

Developer Edition runs locally on macOS or Linux. An owner authorizes **one** native harness: Codex or Claude Code. A Secretary cloud account, password and browser are not required for setup.

## One-command source install

On an existing developer host with Git, zsh, OpenSSL and Go 1.27.1+ (or mise):

```sh
curl -fsSL https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install.sh | bash
```

The installer checks prerequisites, clones into `~/.local/src/secretary` by default (override with `SECRETARY_INSTALL_DIR`), preserves an existing clean checkout and invokes the interactive terminal setup. It never modifies global Codex or Claude Code configuration. On a headless host, fresh setup selects an authenticated native harness (Codex first, then Claude Code). Codex with an already working ACP adapter is considered ready; when **only `codex-acp` is missing**, setup can instead verify authorization with `codex login status`, select Codex if npm is available, install the adapter during setup, and finally re-check full readiness before saving the harness. Unauthorized Codex is skipped when Claude Code is ready, and a standalone `codex-acp` adapter is never treated as an installed Codex CLI. An existing but broken ACP adapter is not assumed repairable through this missing-adapter path. When Codex ACP installation fails during **automatic** fresh setup and Claude Code is already authorized, setup falls back to Claude before creating `config.toml`; subsequent runs keep the Claude selection. If no fallback is available, it exits without writing an unusable configuration so that a retry can install ACP and complete fresh Codex setup. An explicitly selected `--harness codex` never silently switches to Claude. If neither harness is ready or can be prepared, setup fails without saving an unusable default. Only the selected harness is required.

To run setup explicitly in an existing source checkout:

```sh
./secretary setup --interactive
./secretary setup --non-interactive --harness claude_code
```

The interactive flow selects a detected harness, verifies readiness (and offers the native login step if necessary), writes private local credentials, configures an outbound local Execution Node and offers optional Telegram DM setup. It does not copy native provider credentials. Running setup again preserves existing configuration, Node identity, credentials and native sessions. For existing or partially installed deployments it checks the persisted `environment` and provisions **only missing** local Node pairing/admin tokens, including if `config.toml` or an already-paired Node identity exists; no existing secret or bootstrap token is rotated.

## Terminal operations

```sh
secretary config
secretary doctor
secretary start --no-open
secretary node start
secretary node status
secretary node doctor
secretary status
secretary logs
secretary stop
secretary restart
secretary update
```

After starting the server, run `secretary node start` once for owner-approved local Node pairing. The identity is saved in the private Node data directory and reused on subsequent starts. Server pairing credentials are separate from Telegram, Web bootstrap and native provider credentials. Do not publish the private `environment` file.

The configuration menu changes native defaults, local Node server/name/capacity and optional Telegram settings. A harness change is committed only after its native auth/readiness check succeeds; failed auth leaves the previous Secretary and Worker defaults unchanged. An already paired Node cannot silently change identity or server. Existing workspace mappings and MCP servers survive capacity/name edits; `secretary node setup --workspace project=/absolute/path` adds or replaces one mapping without deleting others, while `secretary node setup --clear-workspaces` explicitly removes all mappings. Settings and credentials remain editable as `~/.local/share/secretary/config.toml`, `node/config.json` and the private `environment` file. Changes to a running Server/Node require restart.

For Telegram, provide the BotFather token and optionally an owner DM chat ID. If an owner ID is not configured, binding requires the existing authorized pairing flow; arbitrary Telegram users are not granted command authority. For Web, the existing bootstrap-code-to-cookie session flow remains unchanged. A browser is optional during setup and Server operation, but required to use the Web client itself.

## Background services

On macOS, `install-service` and `node install-service` use launchd. On Linux they install `systemd --user` services; a working per-user systemd manager is required.

```sh
secretary install-service
secretary node install-service
secretary status
secretary node status
secretary uninstall-service
secretary node uninstall-service
```

Pair the local Node (`secretary node start`) **before** enabling its persistent background service. Service logs on Linux use `journalctl --user`. The macOS log files retain their existing paths. On Linux the systemd units do not embed tokens. The launchd units also reference the local script without storing secrets.

`secretary update` is a conservative `git pull --ff-only` followed by rebuild and restart of previously running services; it refuses dirty source checkouts. No automatic destructive migration is performed.

## Acceptance

`scripts/t06-terminal-cli-test.sh` uses fresh temporary homes, fake native CLIs, upgrade/retry fixtures for missing Node secrets, authenticated auto-selection (including Codex-only missing-ACP/npm provisioning, failed npm install falling back to Claude, idempotent repeat and clean Codex-only retry, unauthorized Codex fallback and adapter-only negatives), safe-failure cases and mock systemd commands. `.github/workflows/t06-terminal-cli.yml` runs shell syntax checks and these fixtures on Ubuntu. This is not a substitute for **manual** clean-user installs on both macOS and Linux, nor live Codex/Claude Code authorization and end-to-end model calls; record those checks separately before marking T06 release-accepted.
