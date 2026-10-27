$ErrorActionPreference = 'Stop'
$repoRoot = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$runRoot = Join-Path $repoRoot '.local-run/procurement-mvp'
$manifestPath = Join-Path $runRoot 'processes.json'
if (-not (Test-Path -LiteralPath $manifestPath)) { Write-Output 'No MVP process manifest.'; exit 0 }
$manifest = Get-Content -LiteralPath $manifestPath -Raw | ConvertFrom-Json
foreach ($entry in $manifest.processes) {
  $expected = [IO.Path]::GetFullPath($entry.path)
  if (-not $expected.StartsWith($runRoot + [IO.Path]::DirectorySeparatorChar, [StringComparison]::OrdinalIgnoreCase)) { throw 'Process path is outside the MVP directory.' }
  $process = Get-Process -Id $entry.pid -ErrorAction SilentlyContinue
  if ($process) {
    if (-not [string]::Equals($process.Path,$expected,[StringComparison]::OrdinalIgnoreCase)) { throw 'PID was reused by a different process; refusing to stop it.' }
    Stop-Process -Id $process.Id
  }
}
Write-Output 'MVP processes stopped; local orders and reports preserved.'
