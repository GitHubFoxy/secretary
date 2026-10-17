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
foreach($path in @($NodeDataDir,$Workspace)) { if (-not (Test-Path -LiteralPath $path -PathType Container)) { throw "Missing folder: $path" } }
$NodeDataDir=(Resolve-Path -LiteralPath $NodeDataDir).Path
$Workspace=(Resolve-Path -LiteralPath $Workspace).Path
New-Item -ItemType Directory -Path $WorkerHome -Force | Out-Null
$WorkerHome=(Resolve-Path -LiteralPath $WorkerHome).Path
if ($NodeDataDir -eq $WorkerHome -or $NodeDataDir.StartsWith($WorkerHome+'\')) { throw 'Worker home overlaps Node data.' }
if ($NodeDataDir -eq $Workspace -or $NodeDataDir.StartsWith($Workspace+'\')) { throw 'Workspace overlaps Node data.' }
$bytes=New-Object byte[] 30
[Security.Cryptography.RandomNumberGenerator]::Fill($bytes)
$password=[Convert]::ToBase64String($bytes).Replace('+','A').Replace('/','B')+'a1!'
$account="$env:COMPUTERNAME\$WorkerUsername"
& net user $WorkerUsername $password /add 2>$null | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Could not create local Worker user; refusing to overwrite existing credentials.' }
& icacls $NodeDataDir /deny "$($account):(OI)(CI)R" | Out-Null
if ($LASTEXITCODE -ne 0) { throw 'Failed to seal Node data ACL' }
foreach($dir in @($WorkerHome,$Workspace)) {
 & icacls $dir /grant "$($account):(OI)(CI)M" | Out-Null
 if($LASTEXITCODE -ne 0){throw "Failed to grant Worker workspace access to $dir"}
}
$config=@{username=$WorkerUsername;password=$password;domain='.';home=$WorkerHome}|ConvertTo-Json -Compress
$configPath=Join-Path $NodeDataDir 'worker-account.json'
[IO.File]::WriteAllText($configPath,$config,[Text.UTF8Encoding]::new($false))
& icacls $configPath /inheritance:r /grant:r "$($env:USERNAME):F" 'SYSTEM:F' /deny "$($account):R" | Out-Null
if($LASTEXITCODE -ne 0){throw 'Failed to restrict Worker account settings'}
Write-Host 'Worker account provisioned. Projectless workspaces are created under WorkerHome/workspaces; named project workspaces must be granted explicitly.'
Write-Host 'Existing native provider history is migrated on first Dispatch.'
Write-Host "Verify: secretary-node.exe --data-dir '$NodeDataDir' --check-worker-sandbox"
