param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [int64]$Seed = 42,
  [int]$DeepSeekTrials = 3,
  [switch]$RunDeepSeek
)
$ErrorActionPreference = 'Stop'
$argsList = @('run', './cmd/resume-benchmark', '-benchmark=e1', "-repo-root=$RepoRoot", "-seed=$Seed", "-deepseek-trials=$DeepSeekTrials")
if ($RunDeepSeek) { $argsList += '-run-deepseek' }
Push-Location (Join-Path $RepoRoot 'commerce-runtime')
try { & go @argsList; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } } finally { Pop-Location }
