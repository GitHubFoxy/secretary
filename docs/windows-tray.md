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
