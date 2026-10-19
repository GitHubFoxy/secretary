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

## W03 installation, startup and logs

Run `scripts/install-windows-node.ps1` from an ordinary Windows account. The supported installer stages the Node and tray executables, installs their content-addressed versions under LocalAppData, registers **Secretary Node** in Start/Search and preserves the existing Node data directory, identity, local state and Worker credentials. Upgrading a running tray requests cooperative closure of its window (and its managed Node), waits for the old tray process to exit, atomically replaces the Start Menu shortcut and launches the new tray. Already-running standalone headless Nodes are not forcibly terminated; restart them through their own process supervisor to use the updated binary.

**Launch at Login** uses the current user's Windows `HKCU\\Software\\Microsoft\\Windows\\CurrentVersion\\Run` registry value. The installer retains and points an enabled preference at the new installed executable. The Windows Node process also enforces exclusive per-data-directory ownership through a named mutex, so the tray cannot start a second Node against the same data and credentials when a headless Node is active. The headless executable remains independently runnable without the tray.

**Open Logs** selects `logs/node.log` under the tray's configured Node data directory in File Explorer. Missing or inaccessible paths report where to look and whether to start the Node or open the path manually. ACP-specific raw logs are stored under the data directory's `logs/acp` directory.

Windows CI exercises distinct v1/v2 executable fixtures, upgrade while the tray is running, Start/Search registration, restart, preservation of Node files and Worker account credentials, login registry toggles, headless ownership and Open Logs failure boundaries. Live Windows acceptance still must confirm Start/Search discoverability, login across a real Windows sign-in, tray theme changes and correct notification/Explorer behavior on a user desktop.
