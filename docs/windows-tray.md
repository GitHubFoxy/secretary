# Windows tray companion (W01)

Build on Windows with Go 1.27.1:

```powershell
go build -o secretary-node.exe ./cmd/secretary-node
go build -ldflags "-H=windowsgui" -o secretary-tray.exe ./cmd/secretary-tray
```

Place both executables in the same directory. The tray starts the existing headless `secretary-node.exe` automatically, using its normal pairing and data-directory configuration. An already enrolled Node needs no new pairing token. For non-default deployment config, set `SECRETARY_TRAY_NODE_CONFIG` to its absolute JSON path. To use a Node executable in another directory, set `SECRETARY_TRAY_NODE_EXE` to its absolute path. Normal `SECRETARY_NODE_*` environment settings are inherited by the Node.

To make the standalone executable available from Windows Start/Search without an installer, create a Start Menu shortcut (this is not an automatic login item):

```powershell
$target = (Resolve-Path .\secretary-tray.exe).Path
$start = Join-Path $env:APPDATA "Microsoft\Windows\Start Menu\Programs\Secretary Node.lnk"
$link = (New-Object -ComObject WScript.Shell).CreateShortcut($start)
$link.TargetPath = $target
$link.WorkingDirectory = Split-Path $target
$link.Save()
```

Launch **Secretary Node** from Start/Search. The app has no permanent window or taskbar item. Left- or right-click its system tray icon to open a compact menu. It displays whether the *tray-owned local process* is running, offers **Start Secretary Node** or **Quit Secretary Node**, and **Exit tray**. The popup follows the Windows app light/dark preference, including preference changes while running.

**Quit Secretary Node** signals the owned process to stop cooperatively. If it does not exit within ten seconds, the tray terminates that owned process, never enumerates/kills by executable name, and never stops the Secretary Server or any other Node. Closing the tray also stops its owned Node. The tray uses one process per desktop session and a Windows Job Object so a crashed tray does not strand its managed process. A headless `secretary-node.exe` remains independently usable without the tray.

This W01 companion does not install itself, register a login item, provide logs, or report remote/server connectivity. Those integrations belong to later tickets. On Windows, verify Start/Search, light/dark switching, status transitions, graceful shutdown with active work, crash recovery, and behavior alongside an independently launched Node.

## W02 live status

The managed Node writes an ephemeral tray-only status snapshot with actual
outbound connection observations and its existing harness discovery inventory.
The tray polls that source once a second and shows Server: Connected only after
a successful Node heartbeat. During reconnection it shows Reconnecting; when
the Node exits or the source is missing, invalid, or stale it shows Offline or
Reconnecting without a false Ready indication. Harness rows reflect the Node's
native inventory probes, not just installed executable names. The compact
menu lists the Server immediately after the Node heading, then a separator,
harness rows and existing controls. Windows app theme changes repaint the menu.

### W02 manual Windows acceptance

With the tray popup already open, connect/disconnect the Server and install, remove or change harness availability. Confirm the visible popup resizes without closing, preserves the separator and both controls, and never displays stale Ready. Change the Windows light/dark setting while it is open; exit the managed Node and confirm the menu refreshes to Offline. These native window and appearance transitions require a live Windows desktop and are not certified by unit tests.

**Execution model:** Secretary Workers execute directly with the operating-system permissions and environment of the Execution Node user. Secretary does not provide an additional Worker sandbox or privilege boundary. No separate Windows Worker account or account-provisioning UAC is required. Old `worker-account.json`, previously provisioned accounts and ACLs are not automatically deleted; inspect and clean up manually only after backing up Node identity and native state. Verify any old restrictive ACL still permits direct access by the Node owner.

## W03 installation, startup and logs

Run `scripts/install-windows-node.ps1` from an ordinary Windows account. The supported installer stages the Node and tray executables, installs their content-addressed versions under LocalAppData, registers **Secretary Node** in Start/Search and preserves the existing Node data directory, identity, local state and native provider checkpoints. Upgrading a running tray requests cooperative closure of its window (and its managed Node), waits for the old tray process to exit, atomically replaces the Start Menu shortcut and launches the new tray. Already-running standalone headless Nodes are not forcibly terminated; restart them through their own process supervisor to use the updated binary.

**Launch at Login** uses the current user's Windows `HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run` registry value. The installer retains and points an enabled preference at the new installed executable. The Windows Node process also enforces exclusive per-data-directory ownership through a named mutex, so the tray cannot start a second Node against the same data and credentials when a headless Node is active. The headless executable remains independently runnable without the tray.

**Open Logs** selects `logs/node.log` under the tray's configured Node data directory in File Explorer. Missing or inaccessible paths report where to look and whether to start the Node or open the path manually. ACP-specific raw logs are stored under the data directory's `logs/acp` directory.

Windows CI exercises distinct v1/v2 executable fixtures, upgrade while the tray is running, Start/Search registration, restart, preservation of Node identity, configuration and native provider checkpoints, login registry toggles, headless ownership and Open Logs failure boundaries. Live Windows acceptance still must confirm Start/Search discoverability, login across a real Windows sign-in, tray theme changes and correct notification/Explorer behavior on a user desktop.


## Install 07 verified archive entrypoint

For a standalone Node on Windows, use `scripts/install-windows-node-release.ps1` with
`-ReleaseUri` and `-ReleaseSHA256`. The digest must be obtained independently of
the downloaded archive (for example from a release verified by the owner);
a hash downloaded from the same untrusted location does not establish authenticity.
The archive must contain exactly `secretary-node.exe`, `secretary-tray.exe`,
`release.json` and `install-windows-node.ps1`, with schema 1, version, platform `windows`, architecture
`amd64`, `node_sha256`, and `tray_sha256`.
The supported Windows architecture is x86_64 (amd64). Windows on ARM64 is out of
scope per [ADR-0006](adr/0006-supported-binary-platform-matrix.md); the manifest
parser still accepts an `arm64` value, but no ARM64 artifact is published or
accepted.
The installer validates the outer pinned SHA-256 before extracting and checks
the component digests and host architecture before running the Node installer as the current user. It does not install a Secretary Server or FFmpeg.
This archive format is not an Authenticode signature: publisher identity comes
from the independently authenticated release channel and pinned digest.

A local archive fixture can be addressed with an absolute `file:///` URI.
The existing Node pairing and native checkpoints are retained by the underlying
Windows installer. Native model execution, logout/login persistence and real
UI transitions still require desktop acceptance.


## Install 07 native runtime and acceptance matrix

A selected Codex runtime uses the shared Install 03 Node inventory, persisted under
the installing Windows owner's home. Windows amd64 use pinned Codex CLI
0.163.0 and codex-acp 0.16.0 native PE executables with SHA-256 values sourced
from the corresponding upstream release assets. Neither Go, global npm nor
Node.js is required by the installed release. Native direct process launch accepts `.exe` and wraps safe `.cmd` shims through `cmd.exe`; `.ps1` is not silently executed as a native binary. An owner may explicitly
select both compatible external `.exe` paths; neither external binaries nor
native credentials or user settings are overwritten. The Node process loads the chosen
inventory and fails Codex closed when it is absent, moved, tampered with or
not protocol-validated. Claude Code selection does not install Codex. Provisioning
does not perform Codex login; `codex login` remains the owner's action and
provider quota/authentication is distinct from binary readiness.

The prebuilt artifact is a ZIP containing two native executables, one manifest and `install-windows-node.ps1`, without a source checkout. The SHA-256 supplied to the installer must be independently pinned;
a SHA-256 checksum displayed alongside an untrusted download is insufficient
to establish author or channel authenticity. Use Windows Authenticode / a
verified trusted publication channel when organization policy requires a
publisher identity. The installer stages the verified release before changing
startup registration. Underlying W03 installer retains versioned binaries,
cooperative tray shutdown, singleton ownership, login preference, Start/Search,
Open Logs, Node identity and native checkpoints. No Worker-specific elevation or account provisioning occurs.

CI: `windows-standalone-node.yml` verifies archive/manifest/hash rejection,
Windows extraction fixtures, first/repeated installation and preservation of Node identity, native checkpoints and legacy account records. `windows-node.yml` retains W01/W02/W03 live-status,
Node reconnect, direct-user native process execution, headless and tray lifecycle fixtures. `windows-node-binary-release.yml` builds versioned amd64
Node/tray pairs on GitHub Actions. `go test ./internal/node -run '^TestInstall07'
-count=1 -timeout=5m -v` exercises Windows native artifact contract.
Independent reviewer and CI must verify these changes; no CI result is claimed.

Manual real desktop/provider acceptance still required: fresh standard-user
Windows x64 with no Go/Node/npm and no Worker-provisioning UAC; Start/Search, dark/light theme, status and Open Logs;
logout/login, Launch at Login, Quit with active work, headless co-existence;
first enrollment with a remote macOS/Linux Server; authorized Codex native
Task -> one Result -> reconnect without pairing -> Follow-up with unchanged
Worker/native session identity; explicit unavailable session and quota/auth
errors without replay; upgrade/repair from a trusted independently verified
release and checkpoint/credential preservation.
