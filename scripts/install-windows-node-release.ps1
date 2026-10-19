param(
 [Parameter(Mandatory=$true)][string]$ReleaseUri,
 [Parameter(Mandatory=$true)][ValidatePattern('^[a-fA-F0-9]{64}$')][string]$ReleaseSHA256,
 [string]$NodeDataDir = (Join-Path (Join-Path $env:USERPROFILE '.secretary') 'node'),
 [string]$WorkerHome = (Join-Path $env:ProgramData 'SecretaryWorker'),
 [string]$Workspace = '',
 [string]$WorkerUsername = 'secretary_worker',
 [ValidateSet('codex','claude')][string]$Harness = 'codex',
 [string]$CodexCLI = '',
 [string]$CodexACP = ''
)
$ErrorActionPreference = 'Stop'
Set-StrictMode -Version Latest
$stage = Join-Path $env:TEMP ('secretary-release-' + [guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $stage -Force | Out-Null
try {
 $archive = Join-Path $stage 'release.zip'
 $uri = [Uri]$ReleaseUri
 if ($uri.Scheme -eq 'file') {
  Copy-Item -LiteralPath $uri.LocalPath -Destination $archive
 } elseif ($uri.Scheme -eq 'https') {
  $request = [Net.HttpWebRequest]::Create($uri)
  $request.Timeout = 120000
  $request.AllowAutoRedirect = $false
  $response = $request.GetResponse()
  try {
   if ([int]$response.StatusCode -ne 200) { throw "Release download returned HTTP $([int]$response.StatusCode)" }
   $inputStream = $response.GetResponseStream()
   $outputStream = [IO.File]::Create($archive)
   try {
    $buffer = New-Object byte[] 65536
    $total = [long]0
    while (($n = $inputStream.Read($buffer,0,$buffer.Length)) -gt 0) {
     $total += $n
     if ($total -gt 268435456) { throw 'Release archive exceeds 256 MiB' }
     $outputStream.Write($buffer,0,$n)
    }
   } finally { $outputStream.Dispose(); $inputStream.Dispose() }
  } finally { $response.Dispose() }
 } else { throw 'Release URI must be HTTPS or an explicitly supplied local file URI' }
 $observed = (Get-FileHash -LiteralPath $archive -Algorithm SHA256).Hash
 if (-not [string]::Equals($observed,$ReleaseSHA256,[StringComparison]::OrdinalIgnoreCase)) {
  throw 'Release SHA-256 does not match the independently pinned digest; no binaries were activated'
 }
 Add-Type -AssemblyName System.IO.Compression
 $zip = [IO.Compression.ZipFile]::OpenRead($archive)
 try {
  $entries = @($zip.Entries)
  $expected = @('secretary-node.exe','secretary-tray.exe','release.json')
  if ($entries.Count -ne 3) { throw 'Release archive has unexpected entries' }
  foreach ($entry in $entries) {
   if ($expected -cnotcontains $entry.FullName -or $entry.Length -gt 134217728) { throw 'Release archive contains unexpected or oversized entry' }
   $target = Join-Path $stage $entry.FullName
   $inStream = $entry.Open()
   $outStream = [IO.File]::Create($target)
   try { $inStream.CopyTo($outStream) } finally { $inStream.Dispose(); $outStream.Dispose() }
  }
 } finally { $zip.Dispose() }
 $manifest = Get-Content -LiteralPath (Join-Path $stage 'release.json') -Raw | ConvertFrom-Json
 if ($manifest.schema -ne 1 -or $manifest.platform -ne 'windows' -or [string]::IsNullOrWhiteSpace($manifest.version)) {
  throw 'Unsupported Windows Node release manifest'
 }
 $arch = switch ($env:PROCESSOR_ARCHITECTURE) {
  'AMD64' { 'amd64' }
  'ARM64' { 'arm64' }
  default { throw 'Unsupported Windows architecture' }
 }
 if ($manifest.arch -ne $arch) { throw 'Release architecture does not match Windows' }
 foreach ($entry in @(@('secretary-node.exe','node_sha256'),@('secretary-tray.exe','tray_sha256'))) {
  $path = Join-Path $stage $entry[0]
  $expectedHash = [string]$manifest.($entry[1])
  if ($expectedHash -notmatch '^[a-fA-F0-9]{64}$' -or
      -not [string]::Equals((Get-FileHash -LiteralPath $path -Algorithm SHA256).Hash,$expectedHash,[StringComparison]::OrdinalIgnoreCase)) {
   throw "Release executable hash mismatch: $($entry[0])"
  }
 }
 if (($CodexCLI -eq '') -ne ($CodexACP -eq '')) { throw 'Select both external Codex/ACP executables or neither' }
 if ($Harness -eq 'codex') {
  $pairArgs = @('--codex-pair-action=install','--codex-pair-scope=node')
  if ($CodexCLI -ne '') {
   $pairArgs += ('--codex-pair-cli=' + $CodexCLI)
   $pairArgs += ('--codex-pair-acp=' + $CodexACP)
  }
  & (Join-Path $stage 'secretary-node.exe') @pairArgs
  if ($LASTEXITCODE -ne 0) { throw 'Codex/ACP provisioning failed; existing Node identity and credentials were not changed' }
 }
 & (Join-Path $PSScriptRoot 'install-windows-node.ps1') -NodeDataDir $NodeDataDir -WorkerHome $WorkerHome -Workspace $Workspace -WorkerUsername $WorkerUsername -NodeBinary (Join-Path $stage 'secretary-node.exe') -TrayBinary (Join-Path $stage 'secretary-tray.exe')
 if (-not $?) { throw 'Windows Node installation failed; verify Worker account/ACL diagnostics' }
 if ($Harness -eq 'codex') {
  & (Join-Path $stage 'secretary-node.exe') --codex-pair-action=readiness --codex-pair-scope=node
  if ($LASTEXITCODE -ne 0) {
   Write-Warning 'Codex/ACP installed but not yet native-ready. Authorize with the exact Codex executable reported by --codex-pair-action=show, then run --codex-pair-action=readiness. No alternate harness was selected.'
  }
 }
} finally {
 Remove-Item -LiteralPath $stage -Recurse -Force -ErrorAction SilentlyContinue
}
