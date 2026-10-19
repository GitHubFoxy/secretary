# Linux standalone Execution Node

This installs an Execution Node on Linux x86_64 or aarch64 without installing Secretary Server, its database, web service, Telegram adapter, FFmpeg or FFprobe. Install as a normal user. Requirements: Bash, curl, Python 3.12+, tar, sha256sum, and a private outbound HTTPS route to the already-running Secretary Server. No Go, Node.js, npm, or source compilation is required on the target machine.

The installer consumes the same versioned `secretary-vVERSION-linux-ARCH.json` and `.tar.gz` artifacts as the Linux combined installer. It checks manifest metadata, byte count, SHA-256, archive safety, VERSION, and REVISION before changing the active Node pointer. The archive does contain the combined release payload, but only `secretary-node` and provenance metadata are copied to the standalone deployment. A release manifest checksum authenticates archive bytes relative to the manifest; obtain manifests only from the trusted official release or another explicitly trusted release host.

## Initial installation

The owner must first approve enrollment on the remote Secretary Server and supply its private one-time Node pairing token. The remote Server address must be reachable over private HTTPS; public-internet exposure is not required. Never paste the token into a support ticket or shell history.

```sh
export SECRETARY_NODE_PAIRING_TOKEN='OWNER_APPROVED_TOKEN'
curl -fsSLo install-linux-standalone-node.sh https://raw.githubusercontent.com/GitHubFoxy/secretary/main/scripts/install-linux-standalone-node.sh
bash install-linux-standalone-node.sh --server https://secretary.example.internal --name linux-worker-1 --harness codex
unset SECRETARY_NODE_PAIRING_TOKEN
bash install-linux-standalone-node.sh start
```

Select `--harness claude_code` to use an already installed and authorized native Claude Code CLI rather than provisioning Codex. Codex selection invokes the existing pinned Node-scoped Codex CLI/ACP manager; the installed inventory and provider auth are separate. An unavailable or unauthorized native harness is not silently replaced. Use `doctor` to inspect readiness. The installer cannot obtain provider authorization or extend subscription/quota.

The Node's config, native session data and pairing identity live under `~/.local/share/secretary/node`. Its versioned binaries and active symlink live under `~/.local/share/secretary/standalone-node`. The one-time enrollment token is stored with user-only permissions under `node/enrollment.env` for initial startup. Once Node has paired successfully and `node/data/identity.json` exists, securely remove `node/enrollment.env`; Node reconnects using its durable identity and transport credential. Do not delete Node data, native provider history or enrollment identity during upgrade.

## Operations

```sh
bash install-linux-standalone-node.sh doctor
bash install-linux-standalone-node.sh status
bash install-linux-standalone-node.sh enable-service
bash install-linux-standalone-node.sh start
bash install-linux-standalone-node.sh stop
bash install-linux-standalone-node.sh logs
bash install-linux-standalone-node.sh disable-service
SECRETARY_VERSION=1.2.3 bash install-linux-standalone-node.sh upgrade
bash install-linux-standalone-node.sh repair
```

`status` reports the installed version/revision and the real running executable path (when the user systemd service is installed). Service commands require an active user systemd manager. The optional `secretary-standalone-node.service` is managed separately from the combined Secretary Server and Node services; incompatible existing unit files are not intentionally overwritten. Without systemd, `start` runs the Node in the foreground; stop it with Ctrl-C. `logs` requires the service and reads the user journal. User systemd services normally start after login; on supported hosts, persistent execution outside login requires an explicit administrator-approved `loginctl enable-linger USER`. The installer does not enable linger or escalate privileges.

Repeat installation and repairs preserve the private Node config, pairing identity, native session files and independent harness settings. An archive verification or dependency-provisioning failure does not activate the new version. The previous version remains in the version directory for recovery. A service restart failure attempts to restore the previous active symlink; a host crash during activation may require manual service recovery.

## Acceptance and manual evidence

The Linux CI workflow tests arm64 and amd64 release artifact verification, clean install without a Server, safe rerun, upgrade, corrupt archive refusal, identity/session preservation, and simulated user-systemd operations. This does not establish native provider behavior or a live remote service. The live acceptance ledger (#89) must separately capture a real Linux → private HTTPS remote Server enrollment and authenticated provider-backed Task, exactly one Result, idle Follow-up after Node restart with the same native session, and a deliberate missing-session failure with no blind retry. Record Node binary/revision, native harness versions, operating system, effective service unit and actual process executable. No such live evidence is claimed by this PR.
