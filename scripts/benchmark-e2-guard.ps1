param(
  [string]$RepoRoot = (Split-Path -Parent $PSScriptRoot),
  [int64]$Seed = 42
)
$ErrorActionPreference = 'Stop'
Push-Location (Join-Path $RepoRoot 'commerce-runtime')
try { & go run ./cmd/resume-benchmark -benchmark=e2 "-repo-root=$RepoRoot" "-seed=$Seed"; if ($LASTEXITCODE -ne 0) { exit $LASTEXITCODE } } finally { Pop-Location }
