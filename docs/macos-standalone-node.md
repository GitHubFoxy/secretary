# Standalone macOS Execution Node (Install 05)

Use this installer only on an owner-controlled macOS host that will connect **outbound** to an existing, privately reachable HTTPS Secretary Server. The server must already be running on another host. This command does not install, launch, or configure a local Secretary Server, Web, Telegram, SQLite service, FFmpeg or FFprobe.

Select a versioned published release and inspect its manifest before installation. The installer downloads the matching macOS archive and manifest, checks version/architecture/revision, archive size and SHA-256, validates archive paths and verifies the signed menu bar companion. It requires an independently obtained pinned archive SHA-256 (`SECRETARY_NODE_PINNED_SHA256`) or an explicitly trusted Developer ID Team ID (`SECRETARY_NODE_TRUSTED_TEAM_ID`); ad-hoc signatures alone are never accepted without a pinned digest. Do not obtain a pin from the same mutable release manifest. The published archive is shared with the combined installer, but this installer extracts **only** `secretary-node` and `Secretary Node.app` into its isolated release directory. It needs no Go, Swift/Xcode, npm, or source checkout.

```bash
export SECRETARY_VERSION=1.0.0
export SECRETARY_NODE_SERVER=https://secretary.private.example
export SECRETARY_NODE_NAME=my-mac
export SECRETARY_NODE_HARNESS=claude
export SECRETARY_NODE_PINNED_SHA256=<independently-verified-64-hex-digest>
read -rs SECRETARY_NODE_PAIRING_TOKEN
export SECRETARY_NODE_PAIRING_TOKEN
curl -fsSL https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install-macos-standalone-node.sh -o /tmp/install-secretary-node.sh
bash /tmp/install-secretary-node.sh
unset SECRETARY_NODE_PAIRING_TOKEN
```

Replace the version with an actual published tag, use an owner-approved one-time pairing token from the existing Server, and choose `claude` or `codex`. `SECRETARY_NODE_HARNESS` selects which native dependency is checked/provisioned **at installation**, not a persistent dispatch restriction or default Worker harness. The Server continues selecting Workers through existing harness inventory/routing policies. The installer checks selected-harness readiness before activating the release; a later loss of authentication must surface through normal Node readiness. With `claude`, install and authenticate Claude Code natively in the user's account first. With `codex`, the installer invokes the existing managed Node-scope Codex CLI/ACP provisioning inventory; it does not replace personal Codex authentication or settings. Do not paste a pairing token into command-line arguments or shell history.

State is private under `~/.local/share/secretary-node/`: `config.json`, `data/identity.json`, native session checkpoints, runtime credentials, versioned `releases/`, and atomic `current` release symlink. `~/Applications/Secretary Node.app` is the Spotlight menu bar companion; its login item state and `~/Library/Application Support/SecretaryNode/` preferences and process guard persist across upgrades. The app starts the existing Node runtime, shows Server connectivity and harness readiness, supports Launch at Login, Open Logs, Quit, and keeps the Server separator. Its existing status freshness logic fails closed. Do not run another Node instance with the same state directory.

For unattended/headless use, invoke `~/.local/share/secretary-node/current/bin/secretary-node --config ~/.local/share/secretary-node/config.json` from your own service manager **instead of** the menu bar app. Never enable a legacy `dev.secretary.cli-node` LaunchAgent simultaneously. The installer refuses a combined Secretary install sharing the companion path. Paired Server identity cannot be changed by rerunning the installer: revoke/drain and deliberately re-enroll through the existing owner workflow.

Run the same installer with the same version to reinstall safely, or set `SECRETARY_VERSION` to a newer published version to upgrade. Corrupt artifacts, conflicting installations or failed activation leave the last selected release pointer and companion intact; identity, credentials and sessions are not deleted.

## Verification

CI macOS fixture: `bash scripts/macos-standalone-node-test.sh "$SECRETARY_TEST_DIST" "$GITHUB_SHA"` after the regular release packaging job. Syntax: `bash -n scripts/install-macos-standalone-node.sh scripts/macos-standalone-node-test.sh`. It checks integrity rejection, untrusted signer rejection, Node-only file installation, idempotence, upgrade/rollback fault injection, legacy LaunchAgent refusal and persisted state with developer-tool shims; an additional Go integration test uses a fake TLS pairing server to verify real one-time identity persistence; it does **not** establish live enrollment or native Worker execution.

**MANUAL / owner-controlled macOS acceptance:** use a real released (preferably notarized Developer ID) app and private HTTPS Server; generate an approved one-time token, enroll and verify per-Node credentials and outbound-only connection. Dispatch one genuinely authenticated native Task and verify exactly one Result; disconnect/reconnect, send an idle Follow-up and confirm the same saved session, then deliberately remove the session and verify an explicit failure. Check Spotlight launch, connected/reconnecting/offline and ready/unavailable transitions, system appearance, the Server separator, Login Items approval/reboot, Quit/relaunch, Open Logs, managed Codex/Claude auth, legacy LaunchAgent refusal and upgrades preserving enrollment/checkpoints. Record this separately from fixture CI.
