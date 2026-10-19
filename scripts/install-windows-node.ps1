# T09 Windows Node install/upgrade entrypoint. Run as the existing Node owner;
# this script requests UAC for the isolated SID/ACL stage when necessary.
# Existing identity.json, node-state.json and provider checkpoints are never reset.
param(
 [string]$NodeDataDir = (Join-Path (Join-Path $env:USERPROFILE '.secretary') 'node'),
 [string]$WorkerHome = (Join-Path $env:ProgramData 'SecretaryWorker'),
 [string]$Workspace = '',
 [string]$WorkerUsername = 'secretary_worker',
 [string]$NodeBinary = '',
 [string]$TrayBinary = '',
 [string]$CodexOwnerHome = ''
)
$ErrorActionPreference='Stop'
$repositoryRoot=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot '..')).Path
$setupScript=(Resolve-Path -LiteralPath (Join-Path $PSScriptRoot 'setup-windows-worker.ps1')).Path
if (-not (Test-Path -LiteralPath $NodeDataDir -PathType Container)) {
 New-Item -Path $NodeDataDir -ItemType Directory -Force | Out-Null
}
$NodeDataDir=(Resolve-Path -LiteralPath $NodeDataDir).Path
# Do not attempt to create ProgramData/Worker ACL roots as the
# unelevated Node owner. The UAC stage creates them with privileged ACLs.
$WorkerHome=[IO.Path]::GetFullPath($WorkerHome)
if ([string]::IsNullOrWhiteSpace($Workspace)) { $Workspace=(Join-Path $WorkerHome 'workspaces') }
$Workspace=[IO.Path]::GetFullPath($Workspace)
$stage=Join-Path $env:TEMP ('secretary-install-'+[guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $stage -Force | Out-Null
$stageNode=Join-Path $stage 'secretary-node.exe'
$stageTray=Join-Path $stage 'secretary-tray.exe'
if ([string]::IsNullOrWhiteSpace($NodeBinary) -or [string]::IsNullOrWhiteSpace($TrayBinary)) {
 throw 'The Windows installer requires a verified prebuilt Node/tray pair. Use install-windows-node-release.ps1 with a pinned release SHA-256.'
}
Copy-Item -LiteralPath (Resolve-Path -LiteralPath $NodeBinary).Path -Destination $stageNode
Copy-Item -LiteralPath (Resolve-Path -LiteralPath $TrayBinary).Path -Destination $stageTray
$hash=([IO.Path]::GetFileNameWithoutExtension((Get-FileHash -LiteralPath $stageNode -Algorithm SHA256).Hash))+'-'+(Get-FileHash -LiteralPath $stageTray -Algorithm SHA256).Hash
$installBin=Join-Path (Join-Path $env:LOCALAPPDATA 'Secretary\versions') $hash
$installedNode=Join-Path $installBin 'secretary-node.exe'
$trayInstalled=Join-Path $installBin 'secretary-tray.exe'
if (-not (Test-Path -LiteralPath $installBin -PathType Container)) {
 $partial=$installBin+'.'+[guid]::NewGuid().ToString('N')+'.staging'
 New-Item -ItemType Directory -Path $partial -Force | Out-Null
 try {
  Copy-Item -LiteralPath $stageNode -Destination (Join-Path $partial 'secretary-node.exe')
  Copy-Item -LiteralPath $stageTray -Destination (Join-Path $partial 'secretary-tray.exe')
  Move-Item -LiteralPath $partial -Destination $installBin
 } finally {
  if (Test-Path -LiteralPath $partial) { Remove-Item -LiteralPath $partial -Recurse -Force }
 }
}
Remove-Item -LiteralPath $stage -Recurse -Force
$arguments=@{
 NodeDataDir=$NodeDataDir; Workspace=$Workspace
 WorkerHome=$WorkerHome; WorkerUsername=$WorkerUsername; CodexOwnerHome=$CodexOwnerHome
}
$identityBefore=$null
$identityPath=Join-Path $NodeDataDir 'identity.json'
if (Test-Path -LiteralPath $identityPath -PathType Leaf) {
 $identityBefore=(Get-FileHash -LiteralPath $identityPath -Algorithm SHA256).Hash
}
$principal=[Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if ($principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
 & $setupScript @arguments
} else {
 # The ordinary install flow requests elevation exactly for the account/ACL
 # transaction, rather than failing Dispatch later with missing credentials.
 $encodedArgs=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes(($arguments|ConvertTo-Json -Compress)))
 $escapedScript=$setupScript.Replace("'","''")
 $elevatedScript="$"+"json=[Text.Encoding]::UTF8.GetString([Convert]::FromBase64String('$encodedArgs')); $"+"a=@{}; ($"+"json | ConvertFrom-Json).PSObject.Properties | ForEach-Object { $"+"a[$"+"_.Name]=$"+"_.Value }; & '$escapedScript' @a; if (-not $"+ "?) { exit 1 }"
 $encodedCommand=[Convert]::ToBase64String([Text.Encoding]::Unicode.GetBytes($elevatedScript))
 $elevated=Start-Process -FilePath (Join-Path $PSHOME 'powershell.exe') -Verb RunAs -ArgumentList "-NoProfile -NonInteractive -EncodedCommand $encodedCommand" -Wait -PassThru
 if ($elevated.ExitCode -ne 0) { throw 'Worker account/ACL provisioning was declined or failed. Node state was not reset.' }
}
if ($identityBefore -and ((Get-FileHash -LiteralPath $identityPath -Algorithm SHA256).Hash -ne $identityBefore)) {
 throw 'Node identity unexpectedly changed during upgrade'
}
& $installedNode --data-dir $NodeDataDir --check-worker-sandbox
if ($LASTEXITCODE -ne 0) { throw 'Windows Worker sandbox/Dispatch preflight failed' }
$runKey='HKCU:\Software\Microsoft\Windows\CurrentVersion\Run'
$loginCommand=(Get-ItemProperty -Path $runKey -Name 'Secretary Node' -ErrorAction SilentlyContinue).'Secretary Node'
$oldShortcut=Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Secretary Node.lnk'
$oldTarget=$null
if (Test-Path -LiteralPath $oldShortcut) {
 $oldTarget=(New-Object -ComObject WScript.Shell).CreateShortcut($oldShortcut).TargetPath
}
$restarting=$false
$newTray=$null
$priorShortcutBytes=$null
$priorRunCommand=$loginCommand
$shortcutPath=Join-Path $env:APPDATA 'Microsoft\Windows\Start Menu\Programs\Secretary Node.lnk'
if (Test-Path -LiteralPath $shortcutPath -PathType Leaf) {
 $priorShortcutBytes=[IO.File]::ReadAllBytes($shortcutPath)
}
try {
if ($oldTarget -and -not [string]::Equals($oldTarget,$trayInstalled,[StringComparison]::OrdinalIgnoreCase)) {
 if (-not ('SecretaryTrayWindow' -as [type])) {
 Add-Type -TypeDefinition @'
using System;
using System.Runtime.InteropServices;
public static class SecretaryTrayWindow {
 [DllImport("user32.dll", CharSet=CharSet.Unicode)] public static extern IntPtr FindWindow(string cls, string title);
 [DllImport("user32.dll")] public static extern bool PostMessage(IntPtr hwnd, uint msg, IntPtr wp, IntPtr lp);
 [DllImport("user32.dll")] public static extern uint GetWindowThreadProcessId(IntPtr hwnd, out uint pid);
 [DllImport("user32.dll")] public static extern IntPtr GetWindow(IntPtr hwnd, uint command);
}
'@
 }
 $window=[SecretaryTrayWindow]::FindWindow('SecretaryNodeTrayWindow','Secretary Node')
 if ($window -ne [IntPtr]::Zero) {
  $owner=[SecretaryTrayWindow]::GetWindow($window,4)
  if ($owner -ne [IntPtr]::Zero) { $window=$owner }
  [uint32]$trayPid=0
  [void][SecretaryTrayWindow]::GetWindowThreadProcessId($window,[ref]$trayPid)
  if ($trayPid -eq 0) { throw 'Unable to verify running tray PID; startup registration was not changed' }
  $processInfo=Get-CimInstance Win32_Process -Filter "ProcessId=$trayPid" -ErrorAction Stop
  if (-not $processInfo -or [string]::IsNullOrWhiteSpace($processInfo.ExecutablePath)) {
   throw 'Unable to verify running tray executable; startup registration was not changed'
  }
  $actualPath=[IO.Path]::GetFullPath($processInfo.ExecutablePath)
  $expectedPath=[IO.Path]::GetFullPath($oldTarget)
  if (-not [string]::Equals($actualPath,$expectedPath,[StringComparison]::OrdinalIgnoreCase)) {
   throw "Found tray window belongs to '$actualPath', not installed '$expectedPath'; refusing to stop unrelated process"
  }
  $restarting=$true
  if (-not [SecretaryTrayWindow]::PostMessage($window,0x0010,[IntPtr]::Zero,[IntPtr]::Zero)) { throw 'Cannot stop old tray; restart it manually and retry' }
  $closed=$false
  for ($attempt=0;$attempt -lt 300;$attempt++) {
   if ([SecretaryTrayWindow]::FindWindow('SecretaryNodeTrayWindow','Secretary Node') -eq [IntPtr]::Zero) { $closed=$true;break }
   Start-Sleep -Milliseconds 100
  }
  if (-not $closed) { throw 'Old tray did not exit after cooperative shutdown; upgrade files were staged without changing startup registration' }
  if ($trayPid -ne 0) {
   $oldProcess=Get-Process -Id $trayPid -ErrorAction SilentlyContinue
   if ($oldProcess) {
    $oldProcess.WaitForExit(10000) | Out-Null
    $oldProcess.Refresh()
    if (-not $oldProcess.HasExited) { throw 'Old tray process still holds the single-instance lock' }
   }
  }
 }
}
 $shortcutStage=Join-Path (Split-Path -Path $shortcutPath -Parent) ('Secretary Node.'+[guid]::NewGuid().ToString('N')+'.lnk')
 $shortcut=(New-Object -ComObject WScript.Shell).CreateShortcut($shortcutStage)
 $shortcut.TargetPath=$trayInstalled
 $shortcut.Arguments='--data-dir "'+$NodeDataDir+'"'
 $shortcut.WorkingDirectory=$installBin
 $shortcut.Save()
 if (Test-Path -LiteralPath $shortcutPath) {
  $shortcutBackup=Join-Path (Split-Path -Path $shortcutPath -Parent) ('Secretary Node.'+[guid]::NewGuid().ToString('N')+'.bak')
  [IO.File]::Replace($shortcutStage,$shortcutPath,$shortcutBackup)
  Remove-Item -LiteralPath $shortcutBackup -Force
 } else {
  [IO.File]::Move($shortcutStage,$shortcutPath)
 }
 if ($loginCommand) {
  $quoted='"'+$trayInstalled+'" --data-dir "'+$NodeDataDir+'"'
  Set-ItemProperty -Path $runKey -Name 'Secretary Node' -Value $quoted
 }
 if ($restarting) {
  $newTray=Start-Process -FilePath $trayInstalled -ArgumentList ('--data-dir "'+$NodeDataDir+'"') -PassThru
  $ready=$false
  for ($attempt=0;$attempt -lt 50;$attempt++) {
   Start-Sleep -Milliseconds 100
   $newTray.Refresh()
   if ($newTray.HasExited) { throw 'New tray exited during upgrade activation' }
   $instance=Get-CimInstance Win32_Process -Filter "ProcessId=$($newTray.Id)" -ErrorAction Stop
   $window=[SecretaryTrayWindow]::FindWindow('SecretaryNodeTrayWindow','Secretary Node')
   [uint32]$windowPid=0
   if ($window -ne [IntPtr]::Zero) {
    $owner=[SecretaryTrayWindow]::GetWindow($window,4)
    if ($owner -ne [IntPtr]::Zero) { $window=$owner }
    [void][SecretaryTrayWindow]::GetWindowThreadProcessId($window,[ref]$windowPid)
   }
   if ($instance -and $windowPid -eq $newTray.Id -and
       [string]::Equals([IO.Path]::GetFullPath($instance.ExecutablePath),[IO.Path]::GetFullPath($trayInstalled),[StringComparison]::OrdinalIgnoreCase) -and
       $instance.CommandLine.Contains($NodeDataDir)) { $ready=$true; break }
  }
  if (-not $ready) { throw 'New tray failed process/window/version/data-directory readiness verification' }
 }
} catch {
 $failure=$_
 $shutdownFailure=$null
 try {
 if ($null -ne $newTray) {
  $newTray.Refresh()
  if (-not $newTray.HasExited) {
   $candidate=Get-CimInstance Win32_Process -Filter "ProcessId=$($newTray.Id)" -ErrorAction Stop
   if (-not $candidate -or -not [string]::Equals($candidate.ExecutablePath,$trayInstalled,[StringComparison]::OrdinalIgnoreCase)) {
    throw 'Cannot verify new tray ownership during rollback; previous tray was not restarted'
   }
   $window=[SecretaryTrayWindow]::FindWindow('SecretaryNodeTrayWindow','Secretary Node')
   if ($window -eq [IntPtr]::Zero) { throw 'New tray window unavailable for cooperative Quit; previous tray was not restarted' }
   $owner=[SecretaryTrayWindow]::GetWindow($window,4)
   if ($owner -ne [IntPtr]::Zero) { $window=$owner }
   [uint32]$windowPid=0
   [void][SecretaryTrayWindow]::GetWindowThreadProcessId($window,[ref]$windowPid)
   if ($windowPid -ne $newTray.Id -or -not [SecretaryTrayWindow]::PostMessage($window,0x0010,[IntPtr]::Zero,[IntPtr]::Zero)) {
    throw 'Cannot cooperatively stop owned new tray; previous tray was not restarted'
   }
   $newTray.WaitForExit(20000) | Out-Null
   $newTray.Refresh()
   if (-not $newTray.HasExited) { throw 'New tray Quit did not finish; previous tray was not restarted' }
  }
  $ownedNodePath=Join-Path $installBin 'secretary-node.exe'
  $released=$false
  for ($attempt=0;$attempt -lt 100;$attempt++) {
   $ownedNodes=@(Get-CimInstance Win32_Process -Filter "Name='secretary-node.exe'" -ErrorAction Stop | Where-Object {
    [string]::Equals($_.ExecutablePath,$ownedNodePath,[StringComparison]::OrdinalIgnoreCase) -and
    $_.CommandLine -and $_.CommandLine.Contains($NodeDataDir)
   })
   if ($ownedNodes.Count -eq 0) { $released=$true; break }
   Start-Sleep -Milliseconds 100
  }
  if (-not $released) { throw 'New tray-owned Node still running; previous tray was not restarted' }
 }
 } catch {
  $shutdownFailure=$_
 }
 if ($null -ne $priorShortcutBytes) {
  $restore=Join-Path (Split-Path -Path $shortcutPath -Parent) ('Secretary Node.'+[guid]::NewGuid().ToString('N')+'.restore')
  [IO.File]::WriteAllBytes($restore,$priorShortcutBytes)
  try {
   if (Test-Path -LiteralPath $shortcutPath) {
    $backup=Join-Path (Split-Path -Path $shortcutPath -Parent) ('Secretary Node.'+[guid]::NewGuid().ToString('N')+'.restore-backup')
    [IO.File]::Replace($restore,$shortcutPath,$backup)
    Remove-Item -LiteralPath $backup -Force -ErrorAction SilentlyContinue
   } else {
    [IO.File]::Move($restore,$shortcutPath)
   }
  } finally { Remove-Item -LiteralPath $restore -Force -ErrorAction SilentlyContinue }
 } elseif (Test-Path -LiteralPath $shortcutPath) {
  Remove-Item -LiteralPath $shortcutPath -Force
 }
 if ($priorRunCommand) {
  Set-ItemProperty -Path $runKey -Name 'Secretary Node' -Value $priorRunCommand
 } else {
  Remove-ItemProperty -Path $runKey -Name 'Secretary Node' -ErrorAction SilentlyContinue
 }
 if ($null -eq $shutdownFailure -and $restarting -and $oldTarget -and (Test-Path -LiteralPath $oldTarget -PathType Leaf)) {
  $oldRunning=@(Get-CimInstance Win32_Process -Filter "Name='secretary-tray.exe'" | Where-Object { $_.ExecutablePath -eq $oldTarget })
  if ($oldRunning.Count -eq 0) { Start-Process -FilePath $oldTarget -ArgumentList ('--data-dir "'+$NodeDataDir+'"') }
 }
 if ($null -ne $shutdownFailure) { throw "Upgrade failed: $($failure.Exception.Message). New tray cleanup failed: $($shutdownFailure.Exception.Message). Previous registration restored; old tray was not restarted." }
 throw $failure
}
Write-Host "Secretary Windows Node installed/upgraded. Node state preserved in $NodeDataDir."
