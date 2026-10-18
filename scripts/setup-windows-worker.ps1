# Run once elevated; Worker account may not read Node/Server credentials.
param(
 [Parameter(Mandatory=$true)][string]$NodeDataDir,
 [Parameter(Mandatory=$true)][string]$Workspace,
 [string]$WorkerHome = "$env:ProgramData\SecretaryWorker",
 [string]$WorkerUsername = "secretary_worker"
)
$ErrorActionPreference = 'Stop'
$principal=[Security.Principal.WindowsPrincipal][Security.Principal.WindowsIdentity]::GetCurrent()
if (-not $principal.IsInRole([Security.Principal.WindowsBuiltInRole]::Administrator)) {
 throw 'Run one-time Worker setup in elevated PowerShell.'
}
if (-not (Test-Path -LiteralPath $NodeDataDir -PathType Container)) { throw "Missing Node folder: $NodeDataDir" }
$NodeDataDir=(Resolve-Path -LiteralPath $NodeDataDir).Path
$WorkerHome=[IO.Path]::GetFullPath($WorkerHome)
New-Item -ItemType Directory -Path $WorkerHome -Force | Out-Null
$WorkerHome=(Resolve-Path -LiteralPath $WorkerHome).Path
$Workspace=[IO.Path]::GetFullPath($Workspace)
if (-not (Test-Path -LiteralPath $Workspace -PathType Container)) {
 if ($Workspace.StartsWith($WorkerHome+'\',[StringComparison]::OrdinalIgnoreCase)) {
  New-Item -ItemType Directory -Path $Workspace -Force | Out-Null
 } else { throw "Missing named project Workspace: $Workspace" }
}
$Workspace=(Resolve-Path -LiteralPath $Workspace).Path
if ($NodeDataDir -eq $WorkerHome -or $NodeDataDir.StartsWith($WorkerHome+'\')) { throw 'Worker home overlaps Node data.' }
if ($NodeDataDir -eq $Workspace -or $NodeDataDir.StartsWith($Workspace+'\')) { throw 'Workspace overlaps Node data.' }
$configPath=Join-Path $NodeDataDir 'worker-account.json'
$account="$env:COMPUTERNAME\$WorkerUsername"
if (Test-Path -LiteralPath $configPath -PathType Leaf) {
 $saved=Get-Content -LiteralPath $configPath -Raw | ConvertFrom-Json
 if ($saved.username -ne $WorkerUsername -or $saved.home -ne $WorkerHome -or $saved.domain -ne '.' -or [string]::IsNullOrWhiteSpace($saved.password)) {
  throw 'Existing Worker account configuration differs; refusing to rotate credentials or replace provider state.'
 }
 $password=[string]$saved.password
 & net user $WorkerUsername *> $null
 if ($LASTEXITCODE -ne 0) { throw 'Saved Worker account no longer exists; restore it or explicitly repair the installation.' }
} else {
 $bytes=New-Object byte[] 30
 [Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
 $password=[Convert]::ToBase64String($bytes).Replace('+','A').Replace('/','B')+'a1!'
 # Create through the LocalAccounts API rather than passing a random
 # password via cmd.exe/net.exe command-line parsing or process arguments.
 # Fail closed if the host denies non-admin account provisioning.
 $securePassword=ConvertTo-SecureString $password -AsPlainText -Force
 try {
  New-LocalUser -Name $WorkerUsername -Password $securePassword -PasswordNeverExpires -ErrorAction Stop | Out-Null
 } catch {
  throw ('Could not create local Worker user; refusing to overwrite existing credentials: ' + $_.Exception.GetType().Name)
 }
}
& icacls $NodeDataDir /deny "$($account):(OI)(CI)R" | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to seal Node data ACL' }
foreach($dir in @($WorkerHome,$Workspace)) {
 & icacls $dir /grant "$($account):(OI)(CI)M" | Out-Null
 if($LASTEXITCODE -ne 0){throw "Failed to grant Worker workspace access to $dir"}
}
# Ordinary npm/user-profile CLI installs must remain executable by the
# alternate SID. Grant *read/execute only* on recognized native provider
# packages and the npm shim directory; never share the owner's full profile.
$nodeProfileNpm=Join-Path $env:APPDATA 'npm'
if (Test-Path -LiteralPath $nodeProfileNpm -PathType Container) {
  & icacls $nodeProfileNpm /grant "$($account):(RX)" | Out-Null
  if ($LASTEXITCODE -ne 0) { throw 'Could not grant Worker traversal of npm shim directory' }
  foreach($shim in @('codex.cmd','claude.cmd','opencode.cmd')) {
    $shimPath=Join-Path $nodeProfileNpm $shim
    if (Test-Path -LiteralPath $shimPath -PathType Leaf) {
      & icacls $shimPath /grant "$($account):R" | Out-Null
      if ($LASTEXITCODE -ne 0) { throw "Could not allow native CLI shim: $shimPath" }
    }
  }
  foreach($package in @('@openai\\codex','@anthropic-ai\\claude-code','opencode-ai','@opencode-ai\\cli')) {
    $packagePath=Join-Path (Join-Path $nodeProfileNpm 'node_modules') $package
    if (Test-Path -LiteralPath $packagePath -PathType Container) {
      & icacls $packagePath /grant "$($account):(OI)(CI)RX" /T | Out-Null
      if ($LASTEXITCODE -ne 0) { throw "Could not allow native CLI package: $packagePath" }
    }
  }
}
if (-not (Test-Path -LiteralPath $configPath -PathType Leaf)) {
 $config=@{username=$WorkerUsername;password=$password;domain='.';home=$WorkerHome}|ConvertTo-Json -Compress
 [IO.File]::WriteAllText($configPath,$config,[Text.UTF8Encoding]::new($false))
}
& icacls $configPath /inheritance:r /grant:r "$($env:USERNAME):F" 'SYSTEM:F' /deny "$($account):R" | Out-Null
if($LASTEXITCODE -ne 0){throw 'Failed to restrict Worker account settings'}
Write-Host 'Worker account provisioned. Projectless workspaces are created under WorkerHome/workspaces; named project workspaces must be granted explicitly.'
Write-Host 'Existing native provider history is migrated on first Dispatch.'
Write-Host "Verify: secretary-node.exe --data-dir '$NodeDataDir' --check-worker-sandbox"
