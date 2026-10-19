# Versioned Linux binary installation (Install 02)

Install 02 uses the same schema-1 versioned release contract as macOS Install 01: `secretary-vVERSION-linux-ARCH.json` contains `schema`, `version`, `revision`, `os`, `arch`, `archive`, `sha256`, and `bytes`. Its matching tarball contains `secretaryd`, `secretaryctl`, `secretary-mcp`, `secretary-migrate`, `secretary-node`, the zsh CLI and default config/profiles. Supported architectures are Linux x86_64 (amd64) and aarch64 (arm64). The published artifacts are built on GitHub CI, not on the installation host.

## Requirements and install

Use a Linux host with a systemd user manager and a valid login session (user D-Bus at `$XDG_RUNTIME_DIR/bus`); a standard unprivileged account; curl, tar, GNU coreutils (sha256sum, mv, cp, cmp), Python 3, zsh, OpenSSL and systemctl. The native Codex + authorized ACP adapter **or** Claude Code harness must already be installed and authorized; installer does not install or sign into harnesses. No Go, Node/npm, compilation, root-owned global installation, FFmpeg or remote Node provisioning is performed.

```sh
curl -fsSL https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install-linux-binary.sh | bash
```

Select a published version with `SECRETARY_VERSION=1.2.3` (set in the shell environment before running the installer). The installer fetches a manifest and matching archive, verifies version/platform/revision/length/SHA-256, rejects unsafe tar members, checks all packaged executables, then stages the release under `~/.local/share/secretary/releases`. It uses a versioned `current` symlink and installs symlinks in `~/.local/bin`; put that directory on `PATH`. Source-managed binaries are not overwritten.

The installer provisions private `config.toml`, Profiles, bootstrap and pairing credentials and co-located Node config through the existing CLI `setup --non-interactive` with preinstalled-harness-only mode. No credential values are printed. New installations register `secretary.service` and `secretary-node.service` using `systemctl --user`, with no root-owned files. Both unit files preserve the installer user's native-harness PATH (including user directories), with escaped systemd paths even when HOME contains spaces or percent signs. On initial install the installer waits for nonzero live MainPIDs and an HTTP response from the Server; unhealthy activation rolls back units and the release pointer. The Node initiates outbound pairing to the local Server; inspect the Node logs and verify pairing succeeded before accepting a Worker. If no systemd user manager exists, the installer exits **before setup or activation** and must not be treated as a successful background installation.

## Operations, upgrades and recovery

```sh
secretary doctor
secretary status
secretary node status
systemctl --user status secretary.service secretary-node.service
systemctl --user cat secretary.service secretary-node.service
systemctl --user show -p MainPID -p ExecStart secretary.service secretary-node.service
journalctl --user -u secretary.service -u secretary-node.service
systemctl --user restart secretary.service secretary-node.service
systemctl --user stop secretary-node.service secretary.service
systemctl --user start secretary.service secretary-node.service
```

A new user service normally starts with the login session and stops after logout unless systemd lingering is enabled by the host administrator. Do not assume reboot persistence without verifying the distribution's user-manager/linger policy. Do not enable lingering silently or require sudo.

Re-running a version checks cached release contents against the verified download. Upgrades do not overwrite SQLite, Profiles, `environment`, config, Node credentials, identity, Workspace files or native sessions. Downloads/checksum/staging failures do not switch the active release; failed first activation attempts restore the previous pointer. Installed `VERSION` and `REVISION` are not proof of running revisions: after a successful upgrade restart **both** user services at a safe boundary, compare `secretary status` with the expected revision and inspect the `MainPID` executable through `/proc/PID/exe`. The installer intentionally does not automatically restart or interrupt active Workers on upgrade. A failed live restart requires the operator to restore the previous `current` release and restart services; artifact rollback alone does not undo an external runtime failure.

The binary path is distinct from the developer/source installation described in [terminal installation](terminal-install.md). Do not use source `secretary update` to manage versioned binary releases.

## Verification and live acceptance

`linux-binary-release.yml` builds native Go release binaries for both supported CPU architectures, packages the shared manifest, and runs `scripts/linux-binary-install-test.sh`. That test uses the packaged binaries, temporary HOME, real setup/CLI and mocked systemd for deterministic PATH, space/percent escaping, post-start crash, failed HTTP health and activation rollback tests; it tests missing user-manager rejection, repeated install, upgrade, manifest and checksum rejection, failed staging, and preservation of private state. This is **not** proof of real systemd user service behavior or provider acceptance.

Record manual results (distribution/version, architecture, version/revision and native harness) in the #89 live acceptance ledger:

1. On a new standard-user supported Linux host with actual `systemd --user`, install from the published release without Go/Node/npm, inspect `systemctl --user cat/show/status`, start/restart both units and verify their `MainPID` executable paths and actual running revisions. Reboot or log out/in and record whether services resume under the host's user-manager/linger policy.
2. Complete Web bootstrap pairing, verify Server health, local Node pairing and a pre-authorized native harness; run a local Task through Worker to real Result, then disconnect/restart Node and verify reconnected persistent Worker Follow-up without duplicate execution.
3. Reinstall and upgrade with existing SQLite, Profiles, bootstrap token, Node identity and native Worker session. Validate their persistence; intentionally reject bad/missing releases and verify the previous release still starts.
4. On an unsupported non-systemd host confirm the installer fails and does not claim installed services. Repeat on supported arm64 Linux where available.

These checks do not close #70 or #89.
