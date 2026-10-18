# T09 Windows Node install/upgrade entrypoint. Run as the existing Node owner;
# this script requests UAC for the isolated SID/ACL stage when necessary.
# Existing identity.json, node-state.json and provider checkpoints are never reset.
param(
 [string]$NodeDataDir = (Join-Path (Join-Path $env:USERPROFILE '.secretary') 'node'),
 [string]$WorkerHome = (Join-Path $env:ProgramData 'SecretaryWorker'),
 [string]$Workspace = '',
 [string]$WorkerUsername = 'secretary_worker',
 [string]$NodeBinary = ''
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
if ([string]::IsNullOrWhiteSpace($NodeBinary)) {
 # A repeat install is an upgrade: compile this checkout even when an older
 # secretary-node.exe already exists on PATH or under LocalAppData.
 $go=Get-Command go -ErrorAction SilentlyContinue
 if (-not $go) { throw 'Windows Node source install/upgrade requires Go; otherwise supply an already-updated -NodeBinary.' }
 $NodeBinary=Join-Path $env:LOCALAPPDATA 'Secretary\bin\secretary-node.exe'
 New-Item -ItemType Directory -Path (Split-Path $NodeBinary -Parent) -Force | Out-Null
 Push-Location $repositoryRoot
 try { & go build -o $NodeBinary ./cmd/secretary-node; if ($LASTEXITCODE -ne 0) { throw 'Windows Node build failed' } }
 finally { Pop-Location }
}
$NodeBinary=(Resolve-Path -LiteralPath $NodeBinary).Path
$arguments=@{
 NodeDataDir=$NodeDataDir; Workspace=$Workspace
 WorkerHome=$WorkerHome; WorkerUsername=$WorkerUsername
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
& $NodeBinary --data-dir $NodeDataDir --check-worker-sandbox
if ($LASTEXITCODE -ne 0) { throw 'Windows Worker sandbox/Dispatch preflight failed' }
Write-Host "Secretary Windows Node installed/upgraded. Node state preserved in $NodeDataDir."
